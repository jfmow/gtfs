package realtime

import (
	"errors"
	"math"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/jfmow/gtfs/realtime/proto"
	googleProto "google.golang.org/protobuf/proto"
)

// Consists maps a trip id to the units coupled to run it, lead unit first.
//
// Feeds like Auckland Transport's don't say how long a train is: a 6-car
// service is two 3-car units coupled together, and only the lead unit carries
// the trip - the trailing unit reports its own position with no trip at all.
// The consist is inferred by pairing each tripped unit with an untripped unit
// of the same class travelling alongside it. A trip with no coupled unit is a
// single unit; a trip with no vehicle at all has no entry.
type Consists map[string][]*proto.VehicleDescriptor

// ByTripID returns the units running a trip, lead unit first.
func (c Consists) ByTripID(tripID string) ([]*proto.VehicleDescriptor, error) {
	units, found := c[tripID]
	if !found {
		return nil, errors.New("no consist found for trip id")
	}
	return units, nil
}

// GetConsists returns the inferred consist of every tripped vehicle.
func (v Realtime) GetConsists() (Consists, error) {
	if _, err := v.GetVehicles(); err != nil {
		return nil, err
	}

	v.vehiclesCache.mu.Lock()
	defer v.vehiclesCache.mu.Unlock()

	result := make(Consists, len(v.vehiclesCache.consists))
	for tripID, units := range v.vehiclesCache.consists {
		result[tripID] = units
	}
	return result, nil
}

const (
	// Two units of one train report from their own GPS, tens of metres apart
	// (an AMP unit is ~72 m long).
	consistMaxDistanceMeters = 150
	// Readings further apart in time than this aren't compared at all - a
	// stale reading from a stabled unit can sit right beside a live one.
	consistMaxTimestampGap = 20 * time.Second
	consistMaxSpeedDiff    = 3.0 // m/s, between readings taken together
	// Readings seconds apart catch a braking/accelerating train at different
	// speeds; allow this much more per second between them.
	consistAccel          = 1.0  // m/s²
	consistMaxBearingDiff = 60.0 // degrees, when both report one
	consistMovingSpeed    = 2.0  // m/s
	// A pair is confirmed once seen together on this many polls, at least one
	// of them with both units moving. Units merely stopped side by side never
	// are: at Britomart a waiting train's own trailing unit is often the stale
	// one, while the fresh unit beside it is stabled.
	consistConfirmPolls = 2
	// Coupled units whose fresh readings are further apart than this have
	// been split.
	consistSplitDistanceMeters = 400
	// How long a pair outlives its last poll seen together, when nothing says
	// it has split. A trailing unit's GPS often lags or freezes for minutes
	// while still coupled; a candidate pair only bridges a missed poll or two.
	consistRetain          = 5 * time.Minute
	consistCandidateRetain = time.Minute
)

// consistPair is two units seen coupled. It's keyed by unit, not trip, so it
// carries across a terminus turnaround - including when the driver changes
// ends and the other unit becomes the one carrying the trip.
type consistPair struct {
	units      [2]*proto.VehicleDescriptor
	polls      int
	seenMoving bool
	lastSeen   time.Time
}

func (p *consistPair) confirmed() bool {
	return p.polls >= consistConfirmPolls && p.seenMoving
}

func (p *consistPair) other(unitID string) *proto.VehicleDescriptor {
	if p.units[0].GetId() == unitID {
		return p.units[1]
	}
	return p.units[0]
}

func (p *consistPair) is(a, b string) bool {
	x, y := p.units[0].GetId(), p.units[1].GetId()
	return (x == a && y == b) || (x == b && y == a)
}

// updateConsists pairs tripped and untripped units from one feed poll and
// rebuilds v.vehiclesCache.consists. Caller holds v.vehiclesCache.mu.
func (v Realtime) updateConsists(entities []*proto.FeedEntity, fetchedAt time.Time) {
	cache := v.vehiclesCache
	if cache.consistPairs == nil {
		cache.consistPairs = make(map[string]*consistPair)
	}
	pairs := cache.consistPairs // by unit id; both units point at one pair

	var leads, free []*proto.VehiclePosition
	byVehicleID := make(map[string]*proto.VehiclePosition)
	for _, e := range entities {
		vp := e.GetVehicle()
		if vp == nil || vp.GetVehicle().GetId() == "" {
			continue
		}
		pos := vp.GetPosition()
		if pos == nil || (pos.GetLatitude() == 0 && pos.GetLongitude() == 0) {
			continue
		}
		byVehicleID[vp.GetVehicle().GetId()] = vp
		if vp.GetTrip().GetTripId() != "" {
			leads = append(leads, vp)
		} else {
			free = append(free, vp)
		}
	}

	// A confirmed pair whose units are both reporting, or standing together,
	// stays put until it visibly splits; anyone else alongside is a
	// passer-by. One whose partner has gone quiet elsewhere gives way to a
	// unit actually seen with the lead.
	holds := func(p *consistPair) bool {
		if p == nil || !p.confirmed() {
			return false
		}
		a, b := byVehicleID[p.units[0].GetId()], byVehicleID[p.units[1].GetId()]
		return (fresh(a, b) || standingTogether(a, b)) && !pairSplit(a, b)
	}

	// Each untripped unit trails at most one train - the nearest candidate.
	type candidate struct {
		lead, unit *proto.VehiclePosition
		dist       float64
	}
	var candidates []candidate
	for _, lead := range leads {
		for _, unit := range free {
			if d, ok := travellingTogether(lead, unit); ok {
				candidates = append(candidates, candidate{lead, unit, d})
			}
		}
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].dist < candidates[j].dist })

	matched := make(map[string]bool) // unit ids
	for _, c := range candidates {
		leadID, unitID := c.lead.GetVehicle().GetId(), c.unit.GetVehicle().GetId()
		if matched[leadID] || matched[unitID] {
			continue
		}
		pair := pairs[leadID]
		if pair == nil || !pair.is(leadID, unitID) {
			if holds(pair) || holds(pairs[unitID]) {
				continue
			}
			for _, id := range []string{leadID, unitID} {
				if old := pairs[id]; old != nil {
					delete(pairs, old.units[0].GetId())
					delete(pairs, old.units[1].GetId())
				}
			}
			pair = &consistPair{}
			pairs[leadID], pairs[unitID] = pair, pair
		}
		matched[leadID], matched[unitID] = true, true

		// The lead may have swapped ends since; keep the latest descriptors.
		pair.units = [2]*proto.VehicleDescriptor{cloneDescriptor(c.lead.GetVehicle()), cloneDescriptor(c.unit.GetVehicle())}
		pair.polls++
		pair.seenMoving = pair.seenMoving || (c.lead.GetPosition().GetSpeed() >= consistMovingSpeed && c.unit.GetPosition().GetSpeed() >= consistMovingSpeed)
		pair.lastSeen = fetchedAt
	}

	for id, pair := range pairs {
		if pair.lastSeen.Equal(fetchedAt) {
			continue
		}
		a, b := byVehicleID[pair.units[0].GetId()], byVehicleID[pair.units[1].GetId()]
		retain := consistRetain
		if !pair.confirmed() {
			retain = consistCandidateRetain
		}
		switch {
		case pairSplit(a, b):
			delete(pairs, id)
		case pair.confirmed() && standingTogether(a, b):
			// A layover: the train is stopped and its partner's last reading,
			// however old, is still right beside it.
			pair.lastSeen = fetchedAt
		case !pair.confirmed() && fresh(a, b):
			// Both reported just now and weren't together - the pairing was a
			// coincidence of two trains passing.
			delete(pairs, id)
		case fetchedAt.Sub(pair.lastSeen) > retain:
			delete(pairs, id)
		}
	}

	consists := make(Consists, len(leads))
	for _, lead := range leads {
		leadID := lead.GetVehicle().GetId()
		units := []*proto.VehicleDescriptor{cloneDescriptor(lead.GetVehicle())}
		if pair := pairs[leadID]; pair != nil && pair.confirmed() {
			units = append(units, pair.other(leadID))
		}
		consists[lead.GetTrip().GetTripId()] = units
	}
	cache.consists = consists
}

// travellingTogether reports whether an untripped unit is running coupled to
// a tripped one on this poll, and how far apart they are.
func travellingTogether(lead, unit *proto.VehiclePosition) (float64, bool) {
	if unitClass(lead.GetVehicle().GetLabel()) != unitClass(unit.GetVehicle().GetLabel()) {
		return 0, false
	}
	if !fresh(lead, unit) {
		return 0, false
	}
	gap := timestampGap(lead, unit)

	a, b := lead.GetPosition(), unit.GetPosition()
	// Readings a few seconds apart put a moving train's units further apart.
	maxDist := consistMaxDistanceMeters + float64(a.GetSpeed())*gap.Seconds()
	dist := haversineMeters(float64(a.GetLatitude()), float64(a.GetLongitude()), float64(b.GetLatitude()), float64(b.GetLongitude()))
	if dist > maxDist {
		return 0, false
	}
	if math.Abs(float64(a.GetSpeed()-b.GetSpeed())) > consistMaxSpeedDiff+consistAccel*gap.Seconds() {
		return 0, false
	}
	// Bearing 0 is also "not reported"; only compare when both are moving and
	// report one.
	if a.GetSpeed() >= consistMovingSpeed && b.GetSpeed() >= consistMovingSpeed && a.GetBearing() != 0 && b.GetBearing() != 0 {
		diff := math.Mod(math.Abs(float64(a.GetBearing()-b.GetBearing())), 360)
		if diff > 180 {
			diff = 360 - diff
		}
		if diff > consistMaxBearingDiff {
			return 0, false
		}
	}
	return dist, true
}

// standingTogether reports whether a stopped unit's partner last reported from
// right beside it - however stale that reading is, it hasn't gone anywhere.
func standingTogether(x, y *proto.VehiclePosition) bool {
	if x == nil || y == nil {
		return false
	}
	a, b := x.GetPosition(), y.GetPosition()
	for _, live := range []*proto.VehiclePosition{x, y} {
		if live.GetPosition().GetSpeed() >= consistMovingSpeed {
			return false
		}
	}
	return haversineMeters(float64(a.GetLatitude()), float64(a.GetLongitude()), float64(b.GetLatitude()), float64(b.GetLongitude())) <= consistMaxDistanceMeters
}

func timestampGap(x, y *proto.VehiclePosition) time.Duration {
	return time.Duration(math.Abs(float64(int64(x.GetTimestamp())-int64(y.GetTimestamp())))) * time.Second
}

// fresh reports whether both units are in this poll with readings taken close
// enough together to compare.
func fresh(x, y *proto.VehiclePosition) bool {
	return x != nil && y != nil && timestampGap(x, y) <= consistMaxTimestampGap
}

// pairSplit reports whether two coupled units have visibly separated: each is
// running a trip of its own, or fresh readings put them somewhere else
// entirely. A missing or stale reading is no evidence either way.
func pairSplit(x, y *proto.VehiclePosition) bool {
	if x == nil || y == nil {
		return false
	}
	if x.GetTrip().GetTripId() != "" && y.GetTrip().GetTripId() != "" {
		return true // each running a trip of its own
	}
	if !fresh(x, y) {
		return false
	}
	a, b := x.GetPosition(), y.GetPosition()
	return haversineMeters(float64(a.GetLatitude()), float64(a.GetLongitude()), float64(b.GetLatitude()), float64(b.GetLongitude())) > consistSplitDistanceMeters
}

// unitClass is a label's leading letters ("AMP        1020" -> "AMP"), so a
// train is only ever paired with another unit of its own fleet.
func unitClass(label string) string {
	label = strings.TrimSpace(label)
	end := strings.IndexFunc(label, func(r rune) bool { return !unicode.IsLetter(r) })
	if end == -1 {
		return label
	}
	return label[:end]
}

func cloneDescriptor(d *proto.VehicleDescriptor) *proto.VehicleDescriptor {
	if d == nil {
		return nil
	}
	return googleProto.Clone(d).(*proto.VehicleDescriptor)
}

func haversineMeters(lat1, lon1, lat2, lon2 float64) float64 {
	const r = 6371000
	p1, p2 := lat1*math.Pi/180, lat2*math.Pi/180
	dp, dl := (lat2-lat1)*math.Pi/180, (lon2-lon1)*math.Pi/180
	h := math.Sin(dp/2)*math.Sin(dp/2) + math.Cos(p1)*math.Cos(p2)*math.Sin(dl/2)*math.Sin(dl/2)
	return 2 * r * math.Asin(math.Sqrt(h))
}
