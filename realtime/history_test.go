package realtime

import (
	"testing"
	"time"

	"github.com/jfmow/gtfs/realtime/proto"
)

func TestTripHistoryStoresVehicleLocationsAndTripUpdatesByTripID(t *testing.T) {
	cache := &tripHistoryCache{}
	realtime := Realtime{tripHistoryCache: cache}
	now := time.Unix(100, 0)
	tripID := "trip-1"
	vehicleTimestamp := uint64(100)
	stopSequence := uint32(2)
	updateTimestamp := uint64(101)
	endTime := time.Now().Add(time.Hour).Unix()

	realtime.addVehicleHistory(VehiclesMap{
		tripID: {
			Trip:                &proto.TripDescriptor{TripId: &tripID},
			Timestamp:           &vehicleTimestamp,
			CurrentStopSequence: &stopSequence,
		},
	}, now)
	realtime.addTripUpdateHistory(TripUpdatesMap{
		tripID: {
			Trip:      &proto.TripDescriptor{TripId: &tripID},
			Timestamp: &updateTimestamp,
			StopTimeUpdate: []*proto.TripUpdate_StopTimeUpdate{
				{Departure: &proto.TripUpdate_StopTimeEvent{Time: &endTime}},
			},
		},
	}, now)

	history, err := realtime.GetTripHistoryByTripID(tripID)
	if err != nil {
		t.Fatalf("expected trip history: %v", err)
	}
	if got := len(history.Locations); got != 1 {
		t.Fatalf("locations length = %d, want 1", got)
	}
	if got := len(history.TripUpdates); got != 1 {
		t.Fatalf("trip updates length = %d, want 1", got)
	}
	if !history.ExpiresAt.Equal(time.Unix(endTime, 0)) {
		t.Fatalf("expires at = %v, want %v", history.ExpiresAt, time.Unix(endTime, 0))
	}
}

func TestTripHistoryExpiresWhenTripEnds(t *testing.T) {
	cache := &tripHistoryCache{}
	realtime := Realtime{tripHistoryCache: cache}
	tripID := "trip-1"
	updateTimestamp := uint64(101)
	endTime := int64(200)

	realtime.addTripUpdateHistory(TripUpdatesMap{
		tripID: {
			Trip:      &proto.TripDescriptor{TripId: &tripID},
			Timestamp: &updateTimestamp,
			StopTimeUpdate: []*proto.TripUpdate_StopTimeUpdate{
				{Arrival: &proto.TripUpdate_StopTimeEvent{Time: &endTime}},
			},
		},
	}, time.Unix(100, 0))

	cache.mu.Lock()
	realtime.expireTripHistoryLocked(time.Unix(endTime, 0))
	cache.mu.Unlock()

	if _, err := realtime.GetTripHistoryByTripID(tripID); err == nil {
		t.Fatal("expected expired trip history to be removed")
	}
}
