package realtime

import (
	"testing"
	"time"

	"github.com/jfmow/gtfs/realtime/proto"
)

func tripUpdateFor(tripID, vehicleID string) *proto.FeedEntity {
	tu := &proto.TripUpdate{Trip: &proto.TripDescriptor{
		TripId: &tripID, StartDate: strPtr("20260101"), StartTime: strPtr("08:00:00"),
	}}
	if vehicleID != "" {
		tu.Vehicle = &proto.VehicleDescriptor{Id: &vehicleID}
	}
	id := "tu-" + tripID
	return &proto.FeedEntity{Id: &id, TripUpdate: tu}
}

func vehiclesFrom(t *testing.T, entities ...*proto.FeedEntity) VehiclesMap {
	t.Helper()
	rt, err := NewCombinedClient("", "", 20*time.Second, "https://example.com/feed", *time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	rt.vehiclesFeed.fetch = (&fakeUpstream{entities: entities}).fetch
	vehicles, err := rt.GetVehicles()
	if err != nil {
		t.Fatal(err)
	}
	return vehicles
}

// A parked bus left signed on to a trip, listed after the bus actually
// running it, must not replace it: the trip update names the running bus.
func TestGetVehiclesPrefersTripUpdateVehicleOnDuplicateTrip(t *testing.T) {
	vehicles := vehiclesFrom(t,
		unit("running", "NB5932", "t1", -36.8685, 174.7521, 13, 71, 1000),
		unit("parked", "NB5938", "t1", -36.9222, 174.7015, 0, 173, 1001), // later in feed, newer timestamp
		tripUpdateFor("t1", "running"),
	)
	if got := vehicles["t1"].GetVehicle().GetId(); got != "running" {
		t.Fatalf("t1 vehicle = %q, want the one the trip update names", got)
	}
}

// The trip update's vehicle wins even when it's stopped and the other unit
// is moving - seen in AT's feed, where speed alone picks the wrong bus.
func TestGetVehiclesTripUpdateBeatsMovingImpostor(t *testing.T) {
	vehicles := vehiclesFrom(t,
		unit("impostor", "RT7412", "t1", -36.958, 174.824, 5, 307, 1000),
		unit("assigned", "RT7425", "t1", -36.993, 174.880, 0, 248, 999),
		tripUpdateFor("t1", "assigned"),
	)
	if got := vehicles["t1"].GetVehicle().GetId(); got != "assigned" {
		t.Fatalf("t1 vehicle = %q, want assigned", got)
	}
}

// Without a trip update naming a vehicle, the freshest report wins, and a
// tie keeps the first in the feed rather than the last.
func TestGetVehiclesDuplicateTripFallback(t *testing.T) {
	vehicles := vehiclesFrom(t,
		unit("stale", "A", "t1", -36.8, 174.7, 0, 0, 900),
		unit("fresh", "B", "t1", -36.8, 174.7, 0, 0, 1000),
		unit("first", "C", "t2", -36.8, 174.7, 0, 0, 500),
		unit("second", "D", "t2", -36.8, 174.7, 0, 0, 500),
		tripUpdateFor("t2", ""),
	)
	if got := vehicles["t1"].GetVehicle().GetId(); got != "fresh" {
		t.Fatalf("t1 vehicle = %q, want fresh", got)
	}
	if got := vehicles["t2"].GetVehicle().GetId(); got != "first" {
		t.Fatalf("t2 vehicle = %q, want first", got)
	}
}

// A trip update naming a unit that isn't reporting a position falls back too.
func TestGetVehiclesTripUpdateNamesAbsentUnit(t *testing.T) {
	vehicles := vehiclesFrom(t,
		unit("a", "A", "t1", -36.8, 174.7, 0, 0, 1000),
		unit("b", "B", "t1", -36.8, 174.7, 0, 0, 1001),
		tripUpdateFor("t1", "elsewhere"),
	)
	if got := vehicles["t1"].GetVehicle().GetId(); got != "b" {
		t.Fatalf("t1 vehicle = %q, want b (freshest)", got)
	}
}
