package realtime

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/jfmow/gtfs/realtime/proto"
	googleProto "google.golang.org/protobuf/proto"
)

// unit is one vehicle position; tripID "" is an untripped (trailing) unit.
func unit(id, label, tripID string, lat, lon, speed, bearing float32, ts uint64) *proto.FeedEntity {
	vp := &proto.VehiclePosition{
		Vehicle:   &proto.VehicleDescriptor{Id: &id, Label: &label},
		Position:  &proto.Position{Latitude: &lat, Longitude: &lon, Speed: &speed, Bearing: &bearing},
		Timestamp: &ts,
	}
	if tripID != "" {
		vp.Trip = &proto.TripDescriptor{TripId: &tripID}
	}
	return &proto.FeedEntity{Id: &id, Vehicle: vp}
}

func consistIDs(c Consists, tripID string) []string {
	var ids []string
	for _, u := range c[tripID] {
		ids = append(ids, u.GetId())
	}
	return ids
}

func poll(rt Realtime, at int64, entities ...*proto.FeedEntity) Consists {
	rt.vehiclesCache.mu.Lock()
	defer rt.vehiclesCache.mu.Unlock()
	rt.updateConsists(entities, time.Unix(at, 0))
	return rt.vehiclesCache.consists
}

func newConsistRealtime() Realtime { return Realtime{vehiclesCache: &vehiclesCache{}} }

// ~0.0005 deg latitude = ~55 m.
const step = 0.0005

func TestConsistPairsUnitsRunningTogether(t *testing.T) {
	rt := newConsistRealtime()
	c := poll(rt, 100,
		unit("1", "AMP        1020", "trip-a", -36.87, 174.61, 19.5, 288, 100),
		unit("2", "AMP        1170", "", -36.87+step, 174.61, 19.4, 290, 102),
	)
	if got := consistIDs(c, "trip-a"); len(got) != 1 {
		t.Fatalf("after one poll consist = %v, want just the lead (unconfirmed)", got)
	}
	c = poll(rt, 118,
		unit("1", "AMP        1020", "trip-a", -36.86, 174.61, 19.5, 288, 118),
		unit("2", "AMP        1170", "", -36.86+step, 174.61, 19.6, 289, 117),
	)
	if got := consistIDs(c, "trip-a"); len(got) != 2 || got[0] != "1" || got[1] != "2" {
		t.Fatalf("consist = %v, want [1 2]", got)
	}
}

func TestConsistKeepsPairThroughALongLayover(t *testing.T) {
	rt := newConsistRealtime()
	poll(rt, 100,
		unit("1", "AMP        1033", "trip-a", -36.85, 174.77, 13, 280, 100),
		unit("2", "AMP        620", "", -36.85+step, 174.77, 13, 280, 100))
	poll(rt, 118,
		unit("1", "AMP        1033", "trip-a", -36.844, 174.7676, 5, 280, 118),
		unit("2", "AMP        620", "", -36.844+step, 174.7676, 5, 280, 118))

	// Twenty minutes at Britomart: the trailing unit's reading goes stale
	// beside the train, which then takes its next trip - with a stabled unit
	// reporting fresh right alongside.
	var c Consists
	for at := int64(136); at <= 1336; at += 18 {
		c = poll(rt, at,
			unit("1", "AMP        1033", "trip-b", -36.844, 174.7676, 0, 0, uint64(at)),
			unit("2", "AMP        620", "", -36.844+step, 174.7676, 0, 0, 130),
			unit("3", "AMP        864", "", -36.844, 174.7676+step/2, 0, 0, uint64(at)),
		)
	}
	if got := consistIDs(c, "trip-b"); len(got) != 2 || got[1] != "2" {
		t.Fatalf("consist = %v, want [1 2] kept through the layover", got)
	}
}

func TestConsistIgnoresUnitsStoppedSideBySide(t *testing.T) {
	rt := newConsistRealtime()
	for i := int64(0); i < 5; i++ {
		ts := uint64(100 + i*18)
		c := poll(rt, int64(ts),
			unit("1", "AMP        1101", "trip-a", -36.84, 174.76, 0, 0, ts),
			unit("2", "AMP        511", "", -36.84+step, 174.76, 0, 0, ts),
			unit("3", "AMP        294", "", -36.84-2*step, 174.76, 0, 0, ts),
		)
		if got := consistIDs(c, "trip-a"); len(got) != 1 {
			t.Fatalf("poll %d consist = %v, a train stopped among stabled units must not pair", i, got)
		}
	}
	// Departing together confirms the pair already seen side by side.
	c := poll(rt, 200,
		unit("1", "AMP        1101", "trip-a", -36.83, 174.76, 8, 10, 200),
		unit("2", "AMP        511", "", -36.83+step, 174.76, 8.5, 12, 199),
	)
	if got := consistIDs(c, "trip-a"); len(got) != 2 {
		t.Fatalf("consist = %v, want the pair once it moves off together", got)
	}
}

func TestConsistRejectsStaleAndMismatchedUnits(t *testing.T) {
	rt := newConsistRealtime()
	for _, at := range []int64{100, 118, 136} {
		ts := uint64(at)
		c := poll(rt, at,
			unit("1", "AMP        469", "trip-a", -36.84, 174.76, 7, 107, ts),
			unit("2", "AMP        593", "", -36.84+step, 174.76, 7, 107, ts-179), // stale
			unit("3", "AMP        212", "", -36.84, 174.76+step, 0, 0, ts),       // not moving with it
			unit("4", "RT1402", "", -36.84-step, 174.76, 7, 107, ts),             // a bus
			unit("5", "AMP        300", "", -36.84, 174.76-step, 7, 290, ts),     // other direction
		)
		if got := consistIDs(c, "trip-a"); len(got) != 1 {
			t.Fatalf("consist = %v, want just the lead", got)
		}
	}
}

func TestConsistKeepsPartnerThroughPassersByAndGaps(t *testing.T) {
	rt := newConsistRealtime()
	lead := func(at int64, lat float32) *proto.FeedEntity {
		return unit("1", "AMP        157", "trip-a", lat, 174.83, 16, 163, uint64(at))
	}
	poll(rt, 100, lead(100, -36.92), unit("2", "AMP        198", "", -36.92+step, 174.83, 16, 163, 100))
	poll(rt, 118, lead(118, -36.93), unit("2", "AMP        198", "", -36.93+step, 174.83, 16, 163, 118))

	// Partner missing from a poll, then a closer untripped unit alongside.
	c := poll(rt, 136, lead(136, -36.94))
	if got := consistIDs(c, "trip-a"); len(got) != 2 {
		t.Fatalf("consist = %v, partner missing for a poll should keep the pair", got)
	}
	c = poll(rt, 154, lead(154, -36.95),
		unit("2", "AMP        198", "", -36.95+step, 174.83, 16, 163, 150),
		unit("3", "AMP        999", "", -36.95+step/5, 174.83, 16, 163, 154),
	)
	if got := consistIDs(c, "trip-a"); len(got) != 2 || got[1] != "2" {
		t.Fatalf("consist = %v, want the confirmed partner kept", got)
	}

	// Uncoupled: the partner starts its own trip.
	c = poll(rt, 172, lead(172, -36.96), unit("2", "AMP        198", "trip-b", -36.80, 174.83, 0, 0, 172))
	if got := consistIDs(c, "trip-a"); len(got) != 1 {
		t.Fatalf("consist = %v, want the pair split once the partner runs its own trip", got)
	}
}

// Replays captured GTFS-RT protobuf polls in order:
// AT_FEED='/path/to/polls/*.pb' go test -run TestConsistOnCapturedFeed -v ./realtime
func TestConsistOnCapturedFeed(t *testing.T) {
	paths, _ := filepath.Glob(os.Getenv("AT_FEED"))
	if len(paths) == 0 {
		t.Skip("set AT_FEED to a glob of captured GTFS-RT protobuf polls")
	}
	sort.Strings(paths)
	rt := newConsistRealtime()
	var c Consists
	for _, path := range paths {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var feed proto.FeedMessage
		if err := googleProto.Unmarshal(body, &feed); err != nil {
			t.Fatal(err)
		}
		c = poll(rt, int64(feed.GetHeader().GetTimestamp()), feed.Entity...)
	}
	trains, coupled := 0, 0
	for tripID, units := range c {
		if unitClass(units[0].GetLabel()) != "AMP" {
			continue
		}
		trains++
		if len(units) > 1 {
			coupled++
			t.Logf("%s: %s + %s", tripID, units[0].GetLabel(), units[1].GetLabel())
		} else {
			t.Logf("%s: %s alone", tripID, units[0].GetLabel())
		}
	}
	t.Logf("after %d polls: %d of %d trains coupled", len(paths), coupled, trains)
}

func TestConsistCarriesAcrossTurnaround(t *testing.T) {
	rt := newConsistRealtime()
	poll(rt, 100,
		unit("1", "AMP        578", "trip-a", -37.02, 174.90, 17, 315, 100),
		unit("2", "AMP        605", "", -37.02+step, 174.90, 17, 315, 100))
	poll(rt, 118,
		unit("1", "AMP        578", "trip-a", -37.01, 174.90, 17, 315, 118),
		unit("2", "AMP        605", "", -37.01+step, 174.90, 17, 315, 118))

	// At the terminus the driver changes ends: the other unit now carries the
	// return trip, stopped at the platform.
	c := poll(rt, 600,
		unit("1", "AMP        578", "", -37.00, 174.90, 0, 0, 600),
		unit("2", "AMP        605", "trip-b", -37.00+step, 174.90, 0, 0, 600))
	if got := consistIDs(c, "trip-b"); len(got) != 2 || got[0] != "2" || got[1] != "1" {
		t.Fatalf("consist = %v, want [2 1] carried over from the previous trip", got)
	}
}

func TestConsistSurvivesTrailingUnitGPSFreeze(t *testing.T) {
	rt := newConsistRealtime()
	poll(rt, 100,
		unit("1", "AMP        810", "trip-a", -36.90, 174.70, 7, 290, 100),
		unit("2", "AMP        904", "", -36.90+step, 174.70, 7, 290, 100))
	poll(rt, 118,
		unit("1", "AMP        810", "trip-a", -36.89, 174.70, 11, 290, 118),
		unit("2", "AMP        904", "", -36.89+step, 174.70, 11, 290, 118))

	// The trailing unit stops reporting: its last reading stays put while
	// the train runs on.
	for i, at := range []int64{136, 154, 172, 190} {
		c := poll(rt, at,
			unit("1", "AMP        810", "trip-a", -36.89+float32(i+1)*0.004, 174.70, 11, 290, uint64(at)),
			unit("2", "AMP        904", "", -36.89+step, 174.70, 11, 290, 118))
		if got := consistIDs(c, "trip-a"); len(got) != 2 {
			t.Fatalf("poll %d consist = %v, a frozen reading isn't a split", i, got)
		}
	}

	// ...but not forever.
	c := poll(rt, 118+int64(consistRetain/time.Second)+1,
		unit("1", "AMP        810", "trip-a", -36.80, 174.70, 11, 290, 500))
	if got := consistIDs(c, "trip-a"); len(got) != 1 {
		t.Fatalf("consist = %v, want the pair dropped after %v unseen together", got, consistRetain)
	}
}
