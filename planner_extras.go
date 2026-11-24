package gtfs

import (
	"math"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// leaveLaterMinGain is the least extra time at home worth offering.
const leaveLaterMinGain = 3 * time.Minute

// leaveLaterSameStopKm: stops this close with the same name (the bays of one
// bus station, either side of a road) count as the same place to board.
const leaveLaterSameStopKm = 0.15

// leaveLaterOption looks for a later first ride that still makes the plan's
// first change: boarding at the same stop (or another bay of the same
// station), alighting at the same stop, any route, in time to walk to and
// board the next ride. The rider then arrives at the same time and waits less
// at the change. Returns nil when there's no such ride at least
// leaveLaterMinGain later, or the plan has no change to wait at.
func leaveLaterOption(plan JourneyPlan, day *plannerDay, rules *scanRules, dayStart time.Time, routeMap map[string]Route, stopMap map[string]Stop, walkSpeedKmph float64) *JourneyPlan {
	first, next := -1, -1
	for i, leg := range plan.Legs {
		if leg.Mode != "transit" {
			continue
		}
		if first < 0 {
			first = i
		} else {
			next = i
			break
		}
	}
	if first < 0 || next < 0 {
		return nil
	}
	ride, nextRide := plan.Legs[first], plan.Legs[next]
	if ride.FromStop == nil || ride.ToStop == nil || nextRide.FromStop == nil || ride.RouteID == nextRide.RouteID {
		return nil
	}
	// The walk to the first stop, if any, is re-timed for the new stop.
	originWalk := first == 1 && plan.Legs[0].Mode == "walk"
	if first > 1 || (first == 1 && !originWalk) {
		return nil
	}
	walkSec := 0
	for _, leg := range plan.Legs[first+1 : next] {
		walkSec += int(leg.Duration.Seconds())
	}
	deadline := int(nextRide.DepartureTime.Sub(dayStart).Seconds()) - walkSec - rules.gapAt(nextRide.FromStop.StopId)
	curDep := int(ride.DepartureTime.Sub(dayStart).Seconds())
	leaveSec := int(plan.DepartureTime.Sub(dayStart).Seconds())

	// leaveFor is when the rider leaves home to board at `stop` at depSec.
	leaveFor := func(stop Stop, depSec int) (int, float64) {
		if !originWalk {
			return depSec, 0
		}
		dist := calculateDistance(plan.StartLat, plan.StartLon, stop.StopLat, stop.StopLon)
		return depSec - rules.originGapSec - walkDurationSeconds(dist, walkSpeedKmph), dist
	}

	type found struct {
		board, alight tripStopTime
		leave         int
		dist          float64
	}
	var best *found
	for _, stop := range sameBoardingPlace(*ride.FromStop, stopMap) {
		refs := day.byDep[stop.StopId]
		hi := deadline + day.maxEarlySec
		for i := firstRef(refs, curDep+1-day.maxLateSec); i < len(refs) && int(refs[i].sched) <= hi; i++ {
			times := day.tripList[refs[i].trip]
			board := times[refs[i].pos]
			if board.TripID == ride.TripID || board.DepartureSec <= curDep || board.DepartureSec > deadline {
				continue
			}
			leave, dist := leaveFor(stop, board.DepartureSec)
			if best != nil && leave <= best.leave {
				continue
			}
			if !rules.canBoard(board) || !rules.tripAllowed(times) {
				continue
			}
			for _, st := range times[refs[i].pos+1:] {
				if st.ArrivalSec > deadline {
					break
				}
				if st.StopID == ride.ToStop.StopId && rules.canAlight(st) {
					best = &found{board: board, alight: st, leave: leave, dist: dist}
					break
				}
			}
		}
	}
	if best == nil || time.Duration(best.leave-leaveSec)*time.Second < leaveLaterMinGain {
		return nil
	}

	legs := make([]JourneyLeg, len(plan.Legs))
	copy(legs, plan.Legs)
	boardStop := stopMap[best.board.StopID]
	if originWalk {
		w := legs[0]
		w.ToStop = &boardStop
		w.DistanceKm = best.dist
		w.DepartureTime = dayStart.Add(time.Duration(best.leave) * time.Second)
		w.ArrivalTime = w.DepartureTime.Add(time.Duration(walkDurationSeconds(best.dist, walkSpeedKmph)) * time.Second)
		w.Duration = w.ArrivalTime.Sub(w.DepartureTime)
		legs[0] = w
	}
	timing := buildTransitLegTiming(dayStart, best.board.DepartureSec, best.alight.ArrivalSec, best.board.ScheduledDepartureSec, best.alight.ScheduledArrivalSec, best.alight.RealtimeStatus)
	leg := legs[first]
	leg.FromStop = &boardStop
	leg.TripID = best.board.TripID
	leg.RouteID = best.board.RouteID
	leg.Route = nil
	if r, ok := routeMap[best.board.RouteID]; ok {
		leg.Route = &r
	}
	leg.DepartureTime = timing.DepartureTime
	leg.ArrivalTime = timing.ArrivalTime
	leg.Duration = timing.Duration
	leg.ScheduledDepartureTime = timing.ScheduledDepartureTime
	leg.ScheduledArrivalTime = timing.ScheduledArrivalTime
	leg.RealtimeStatus = timing.RealtimeStatus
	leg.DelaySeconds = timing.DelaySeconds
	leg.TripUsable = best.board.TripUsable
	leg.VehicleTracked = false
	legs[first] = leg
	// A walk to the next ride now starts when this one gets in.
	at := leg.ArrivalTime
	for i := first + 1; i < next; i++ {
		legs[i].DepartureTime = at
		legs[i].ArrivalTime = at.Add(legs[i].Duration)
		at = legs[i].ArrivalTime
	}

	later := plan
	later.ID = uuid.NewString()
	later.Legs = legs
	later.DepartureTime = dayStart.Add(time.Duration(best.leave) * time.Second)
	if !originWalk {
		later.DepartureTime = leg.DepartureTime
	}
	later.TotalDuration = later.ArrivalTime.Sub(later.DepartureTime)
	later.LeaveLater = nil
	later.RouteGeoJSON = nil
	return &later
}

// sameBoardingPlace returns the stop and the others a rider would call the
// same place to catch a ride: platforms of the same station, and same-named
// stops within leaveLaterSameStopKm.
func sameBoardingPlace(stop Stop, stopMap map[string]Stop) []Stop {
	out := []Stop{stop}
	key, name := stationKey(stop), normalizeStopName(stop.StopName)
	for id, s := range stopMap {
		if id == stop.StopId {
			continue
		}
		if (stop.ParentStation != "" && stationKey(s) == key) ||
			(normalizeStopName(s.StopName) == name && calculateDistance(stop.StopLat, stop.StopLon, s.StopLat, s.StopLon) <= leaveLaterSameStopKm) {
			out = append(out, s)
		}
	}
	return out
}

// AddWalkRoutes replaces a WalkRoutesPending plan's straight-line walks with
// walking routes from OSRM (and its LeaveLater's too). A no-op for a plan
// that isn't pending. It only assigns plan's own fields (LeaveLater gets a
// new copy), so a copy of a cached plan can be filled while the cache is read.
func (v Database) AddWalkRoutes(plan *JourneyPlan, osrmURL string) {
	if plan == nil || !plan.WalkRoutesPending {
		return
	}
	req := JourneyRequest{StartLat: plan.StartLat, StartLon: plan.StartLon, EndLat: plan.EndLat, EndLon: plan.EndLon, OsrmURL: osrmURL}
	plan.RouteGeoJSON = buildJourneyGeoJSON(v, req, plan.Legs)
	plan.WalkRoutesPending = false
	if plan.LeaveLater != nil {
		// A copy: callers keep plans (and share this pointer) elsewhere.
		later := *plan.LeaveLater
		later.WalkRoutesPending = true
		v.AddWalkRoutes(&later, osrmURL)
		plan.LeaveLater = &later
	}
}

func hasWalkLeg(legs []JourneyLeg) bool {
	for _, l := range legs {
		if l.Mode == "walk" {
			return true
		}
	}
	return false
}

// --- Shape cache ---------------------------------------------------------------
//
// Drawing a plan's rides reads each trip's whole shape from SQLite, which was
// most of a warm plan's time. The same few hundred shapes cover nearly every
// plan, so the planner keeps them in memory (dropped on a feed refresh).

const plannerShapeCacheSize = 768

type shapeCacheEntry struct {
	shape    Shape
	lastUsed int64
}

var plannerShapes = struct {
	sync.Mutex
	tick    int64
	tripTo  map[string]string // db|trip -> shape id ("" = none)
	byShape map[string]*shapeCacheEntry
}{tripTo: map[string]string{}, byShape: map[string]*shapeCacheEntry{}}

// plannerTripShape is GetShapeByTripID through the cache. The returned
// Coordinates are shared: callers must not modify them in place.
func (v Database) plannerTripShape(tripID string) (Shape, error) {
	c := &plannerShapes
	tripKey := v.name + "|" + tripID
	c.Lock()
	shapeID, known := c.tripTo[tripKey]
	if known {
		if e, ok := c.byShape[v.name+"|"+shapeID]; ok {
			c.tick++
			e.lastUsed = c.tick
			s := e.shape
			c.Unlock()
			return s, nil
		}
	}
	c.Unlock()

	shape, err := v.GetShapeByTripID(tripID)
	if err != nil {
		return shape, err
	}
	c.Lock()
	defer c.Unlock()
	if len(c.tripTo) > 50_000 {
		c.tripTo = map[string]string{}
	}
	c.tripTo[tripKey] = shape.ShapeID
	if len(c.byShape) >= plannerShapeCacheSize {
		oldest, oldestTick := "", int64(math.MaxInt64)
		for k, e := range c.byShape {
			if e.lastUsed < oldestTick {
				oldest, oldestTick = k, e.lastUsed
			}
		}
		delete(c.byShape, oldest)
	}
	c.tick++
	c.byShape[v.name+"|"+shape.ShapeID] = &shapeCacheEntry{shape: shape, lastUsed: c.tick}
	return shape, nil
}

// dropPlannerCaches forgets a database's cached planner data after its feed
// is replaced.
func dropPlannerCaches(dbName string) {
	tripStopTimesMemos.Delete(dbName)
	interchangeMemos.Delete(dbName)
	stopTransferGraphMemos.Delete(dbName)
	c := &plannerShapes
	c.Lock()
	prefix := dbName + "|"
	for k := range c.tripTo {
		if strings.HasPrefix(k, prefix) {
			delete(c.tripTo, k)
		}
	}
	for k := range c.byShape {
		if strings.HasPrefix(k, prefix) {
			delete(c.byShape, k)
		}
	}
	c.Unlock()
}

// pruneDominatedPlans drops a plan when another leaves no earlier, arrives no
// later, changes no more often and walks no further, and is strictly better
// on one of those (the same rule as the apps' pruneDominatedPlans), so the
// result slots go to real alternatives.
func pruneDominatedPlans(plans []JourneyPlan) []JourneyPlan {
	if len(plans) < 2 {
		return plans
	}
	walk := make([]float64, len(plans))
	for i, p := range plans {
		walk[i] = totalWalkDistance(p)
	}
	out := plans[:0:0]
	for i, b := range plans {
		dominated := false
		for j, a := range plans {
			if i == j {
				continue
			}
			noWorse := !a.DepartureTime.Before(b.DepartureTime) && !a.ArrivalTime.After(b.ArrivalTime) &&
				a.Transfers <= b.Transfers && walk[j] <= walk[i]+1e-6
			better := a.ArrivalTime.Before(b.ArrivalTime) || a.Transfers < b.Transfers || a.DepartureTime.After(b.DepartureTime)
			if noWorse && (better || j < i) {
				dominated = true
				break
			}
		}
		if !dominated {
			out = append(out, b)
		}
	}
	return out
}

// farWorseMargin: a plan whose ranking cost is more than this behind the best
// one isn't shown (e.g. a direct train arriving two hours before an arrive-by
// time, when buses get there with minutes to spare).
const farWorseMargin = 60 * time.Minute

func dropFarWorsePlans(plans []JourneyPlan, rank planRanking) []JourneyPlan {
	if len(plans) < 2 {
		return plans
	}
	best := rank.cost(plans[0])
	for _, p := range plans[1:] {
		best = min(best, rank.cost(p))
	}
	out := plans[:0:0]
	for _, p := range plans {
		if rank.cost(p) <= best+farWorseMargin {
			out = append(out, p)
		}
	}
	return out
}

// reachedByRide keeps the end-of-journey stop candidates whose label wasn't
// set by a walk from another stop: the rider walks to the destination (or
// from the origin) straight from the stop the ride uses, never via a second
// stop, which read back as one long walk past the walking limit.
func reachedByRide(stops []StopWithDistance, ok func(stopID string) bool) []StopWithDistance {
	out := make([]StopWithDistance, 0, len(stops))
	for _, s := range stops {
		if ok(s.Stop.StopId) {
			out = append(out, s)
		}
	}
	return out
}

// --- The previous day's late-night trips ------------------------------------
//
// GTFS times count from the service day's start and run past 24:00 for trips
// that finish after midnight. Planning at 00:30 uses the new day's timetable,
// which doesn't have them: a Friday-night bus at "24:40" belongs to Friday's
// service day. Each day's planner timetable therefore also holds the previous
// day's late trips, from midnight on, shifted onto the new day's clock.

// overnightSuffix marks those trips' keys (a trip ID often runs every day,
// so the same ID can be on both days). Their stop times keep the real TripID.
const overnightSuffix = "@prev"

// pastMidnightFilter is the WHERE clause queryTripStopTimes adds for trips that
// run past 24:00.
func pastMidnightFilter(on bool) string {
	if !on {
		return ""
	}
	return `WHERE st.trip_id IN (
		SELECT trip_id FROM stop_times
		WHERE CAST(substr(departure_time, 1, instr(departure_time, ':') - 1) AS INTEGER) >= 24
		   OR CAST(substr(arrival_time, 1, instr(arrival_time, ':') - 1) AS INTEGER) >= 24
	)`
}

// overnightTrips shifts the previous service day's trips onto this day's
// clock (offsetSec = previous day start - this day start, about -24 h) and
// keeps only the stops from this day's start on; a trip left with fewer than
// two stops can't be ridden and is dropped.
func overnightTrips(prev map[string][]tripStopTime, offsetSec int) map[string][]tripStopTime {
	out := make(map[string][]tripStopTime)
	for id, times := range prev {
		var kept []tripStopTime
		for _, st := range times {
			st.ArrivalSec += offsetSec
			st.DepartureSec += offsetSec
			st.ScheduledArrivalSec += offsetSec
			st.ScheduledDepartureSec += offsetSec
			if st.DepartureSec < 0 {
				continue
			}
			if st.ArrivalSec < 0 {
				st.ArrivalSec, st.ScheduledArrivalSec = st.DepartureSec, st.ScheduledDepartureSec
			}
			kept = append(kept, st)
		}
		if len(kept) >= 2 {
			out[id+overnightSuffix] = kept
		}
	}
	return out
}

// previousServiceDay is the YYYYMMDD before `day` ("" if it doesn't parse).
func previousServiceDay(day string) string {
	t, err := time.Parse("20060102", day)
	if err != nil {
		return ""
	}
	return t.AddDate(0, 0, -1).Format("20060102")
}

// withOvernightTrips adds the previous service day's late trips to `trips`
// (this day's, starting at dayStart). A failed load just leaves them out.
func (v Database) withOvernightTrips(trips map[string][]tripStopTime, dayStart time.Time) map[string][]tripStopTime {
	prevStart := serviceDayStart(serviceDayNoon(dayStart).AddDate(0, 0, -1), v.timeZone)
	prev, err := v.queryTripStopTimes(prevStart, true)
	if err != nil || len(prev) == 0 {
		return trips
	}
	for id, times := range overnightTrips(prev, int(prevStart.Sub(dayStart).Seconds())) {
		trips[id] = times
	}
	return trips
}

// tripTimesForLeg finds the stop times behind a transit leg: the trip by ID,
// or the previous day's run of it, whichever boards at the leg's stop at the
// leg's time.
func tripTimesForLeg(trips map[string][]tripStopTime, leg JourneyLeg, dayStart time.Time) ([]tripStopTime, bool) {
	if leg.FromStop == nil {
		return nil, false
	}
	dep := int(leg.ScheduledDepartureTime.Sub(dayStart).Seconds())
	if leg.ScheduledDepartureTime.IsZero() {
		dep = int(leg.DepartureTime.Sub(dayStart).Seconds())
	}
	for _, key := range []string{leg.TripID, leg.TripID + overnightSuffix} {
		times, ok := trips[key]
		if !ok {
			continue
		}
		for _, st := range times {
			if st.StopID == leg.FromStop.StopId && (st.ScheduledDepartureSec == dep || st.DepartureSec == int(leg.DepartureTime.Sub(dayStart).Seconds())) {
				return times, true
			}
		}
	}
	return nil, false
}

// longWait is the longest wait at a change a plan can have while another
// option has no such wait (late at night, a last train followed by hours at a
// stop for the first morning bus otherwise "wins" by leaving earlier).
const longWait = 45 * time.Minute

// longestChangeWait is the longest time between one ride's arrival and the
// next ride's departure, less any walk between them.
func longestChangeWait(p JourneyPlan) time.Duration {
	var longest time.Duration
	var lastArrival time.Time
	var walk time.Duration
	for _, leg := range p.Legs {
		if leg.Mode != "transit" {
			walk += leg.Duration
			continue
		}
		if !lastArrival.IsZero() {
			if w := leg.DepartureTime.Sub(lastArrival) - walk; w > longest {
				longest = w
			}
		}
		lastArrival, walk = leg.ArrivalTime, 0
	}
	return longest
}

// dropLongWaitPlans removes plans with a change wait over longWait, unless
// every plan has one.
func dropLongWaitPlans(plans []JourneyPlan) []JourneyPlan {
	out := plans[:0:0]
	for _, p := range plans {
		if longestChangeWait(p) <= longWait {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return plans
	}
	return out
}
