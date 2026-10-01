package gtfs

import (
	"testing"
	"time"

	gtfsrealtime "github.com/jfmow/gtfs/realtime"
	"github.com/jfmow/gtfs/realtime/proto"
)

func vehicleOnTrip(tripID, startDate string) *proto.VehiclePosition {
	trip := &proto.TripDescriptor{TripId: &tripID}
	if startDate != "" {
		trip.StartDate = &startDate
	}
	return &proto.VehiclePosition{Trip: trip}
}

func TestMarkVehicleTracked(t *testing.T) {
	loc := time.UTC
	now := time.Date(2026, 10, 1, 13, 0, 0, 0, loc)
	at := func(day, hour int) time.Time { return time.Date(2026, 10, day, hour, 0, 0, 0, loc) }
	ride := func(tripID string, depart time.Time) JourneyLeg {
		return JourneyLeg{Mode: "transit", TripID: tripID, ScheduledDepartureTime: depart, RealtimeStatus: "on_time"}
	}

	plans := []JourneyPlan{{Legs: []JourneyLeg{
		{Mode: "walk"},
		ride("running", at(1, 13)),        // vehicle on today's trip
		ride("not-yet", at(1, 15)),        // trip update only, no vehicle
		ride("tomorrow", at(2, 9)),        // same trip ID, vehicle is today's run
		ride("dateless", at(1, 14)),       // vehicle without start_date, today
		ride("dateless-later", at(2, 14)), // vehicle without start_date, tomorrow
		ride("past-midnight", at(2, 0)),   // trip started on the 1st's service day
	}}}
	vehicles := gtfsrealtime.VehiclesMap{
		"running":        vehicleOnTrip("running", "20261001"),
		"tomorrow":       vehicleOnTrip("tomorrow", "20261001"),
		"dateless":       vehicleOnTrip("dateless", ""),
		"dateless-later": vehicleOnTrip("dateless-later", ""),
		"past-midnight":  vehicleOnTrip("past-midnight", "20261001"),
	}

	markVehicleTracked(plans, vehicles, now, loc)

	want := []bool{false, true, false, false, true, false, true}
	for i, leg := range plans[0].Legs {
		if leg.VehicleTracked != want[i] {
			t.Errorf("leg %d (%q): VehicleTracked = %v, want %v", i, leg.TripID, leg.VehicleTracked, want[i])
		}
	}
}
