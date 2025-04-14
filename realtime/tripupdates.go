package realtime

import (
	"errors"
	"sync"
	"time"

	"github.com/jfmow/gtfs/realtime/proto"
)

var (
	tripUpdateApiRequestMutex sync.Mutex
)

var (
	cachedTripUpdatesData       map[string]TripUpdatesMap = make(map[string]TripUpdatesMap)
	lastUpdatedTripUpdatesCache map[string]time.Time      = make(map[string]time.Time)
)

type TripUpdatesMap map[string]*proto.TripUpdate

func (v Realtime) GetTripUpdates() (TripUpdatesMap, error) {
	tripUpdateApiRequestMutex.Lock()
	defer tripUpdateApiRequestMutex.Unlock()
	if cachedTripUpdatesData[v.uuid] != nil && len(cachedTripUpdatesData[v.uuid]) >= 1 && lastUpdatedTripUpdatesCache[v.uuid].Add(v.refreshPeriod).After(time.Now()) {
		return cachedTripUpdatesData[v.uuid], nil
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

	cachedTripUpdatesData[v.uuid] = updates
	lastUpdatedTripUpdatesCache[v.uuid] = time.Now()

	return updates, nil
}

func (trips TripUpdatesMap) ByTripID(tripID string) (*proto.TripUpdate, error) {
	trip, found := trips[tripID]
	if !found {
		return nil, errors.New("no trip update found for trip id")
	}
	return trip, nil
}
