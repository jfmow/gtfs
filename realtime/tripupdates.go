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

	for _, i := range result {
		tripId := i.GetTripUpdate().GetTrip().GetTripId()
		updates[tripId] = i.GetTripUpdate()
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
