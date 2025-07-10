package realtime

import (
	"errors"
	"sync"
	"time"

	"github.com/jfmow/gtfs/realtime/proto"
)

type tripUpdateCache struct {
	mu          sync.Mutex
	data        TripUpdatesMap
	lastUpdated time.Time
}

type TripUpdatesMap map[string]*proto.TripUpdate

func (v Realtime) GetTripUpdates() (TripUpdatesMap, error) {
	v.tripUpdatesCache.mu.Lock()
	defer v.tripUpdatesCache.mu.Unlock()

	if len(v.tripUpdatesCache.data) >= 1 && v.tripUpdatesCache.lastUpdated.Add(v.refreshPeriod).After(time.Now()) {
		return v.tripUpdatesCache.data, nil
	}

	result, err := fetchProto(v.tripUpdatesUrl, v.apiHeader, v.apiKey)
	if err != nil {
		return nil, err
	}

	var updates = make(TripUpdatesMap)
	now := time.Now().In(v.localTimeZone)

	for _, i := range result {
		tripUpdate := i.GetTripUpdate()
		trip := tripUpdate.GetTrip()
		tripId := trip.GetTripId()
		startDateStr := trip.GetStartDate()
		startTimeStr := trip.GetStartTime()

		// Parse start date + time
		startDateTime, err := time.Parse("20060102 15:04:05", startDateStr+" "+startTimeStr)
		if err != nil {
			continue // skip malformed
		}

		hasStarted := !startDateTime.After(now)
		timestamp := tripUpdate.GetTimestamp()

		existing, exists := updates[tripId]
		if !exists {
			// If no previous entry, keep this one (even future trip)
			updates[tripId] = tripUpdate
			continue
		}

		existingTrip := existing.GetTrip()
		existingDateTime, err := time.Parse("20060102 15:04:05",
			existingTrip.GetStartDate()+" "+existingTrip.GetStartTime())
		if err != nil {
			continue
		}
		existingHasStarted := !existingDateTime.After(now)
		existingTimestamp := existing.GetTimestamp()

		switch {
		case !existingHasStarted && hasStarted:
			// Prefer started over unstarted
			updates[tripId] = tripUpdate

		case hasStarted && existingHasStarted:
			// Both started: prefer latest timestamp
			if timestamp > existingTimestamp {
				updates[tripId] = tripUpdate
			}

		case !hasStarted && !existingHasStarted:
			// Keep first unstarted trip only (do not overwrite)
			// No action needed
		}
	}

	v.tripUpdatesCache.data = updates
	v.tripUpdatesCache.lastUpdated = time.Now()

	return updates, nil
}

func (trips TripUpdatesMap) ByTripID(tripID string) (*proto.TripUpdate, error) {
	trip, found := trips[tripID]
	if !found {
		return nil, errors.New("no trip update found for trip id")
	}
	return trip, nil
}
