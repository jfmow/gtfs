package realtime

import (
	"errors"
	"sync"
	"time"
)

var (
	tripUpdateApiRequestMutex sync.Mutex
)

var (
	cachedTripUpdatesData       map[string]TripUpdatesMap = make(map[string]TripUpdatesMap)
	lastUpdatedTripUpdatesCache map[string]time.Time      = make(map[string]time.Time)
)

type TripUpdatesMap map[string]TripUpdate

func (v Realtime) GetTripUpdates() (TripUpdatesMap, error) {
	tripUpdateApiRequestMutex.Lock()
	defer tripUpdateApiRequestMutex.Unlock()
	if cachedTripUpdatesData[v.uuid] != nil && len(cachedTripUpdatesData[v.uuid]) >= 1 && lastUpdatedTripUpdatesCache[v.uuid].Add(v.refreshPeriod).After(time.Now()) {
		return cachedTripUpdatesData[v.uuid], nil
	}

	result, err := fetchData[[]struct {
		ID         string     `json:"id"`
		TripUpdate TripUpdate `json:"trip_update"`
		IsDeleted  bool       `json:"is_deleted"`
	}](v.tripUpdatesUrl, v.apiHeader, v.apiKey)
	if err != nil {
		return nil, err
	}

	var updates = make(TripUpdatesMap)

	for _, i := range result {
		i.TripUpdate.ID = i.ID
		updates[i.TripUpdate.Trip.TripID] = i.TripUpdate
	}

	cachedTripUpdatesData[v.uuid] = updates
	lastUpdatedTripUpdatesCache[v.uuid] = time.Now()

	return updates, nil
}

func (trips TripUpdatesMap) ByTripID(tripID string) (TripUpdate, error) {
	trip, found := trips[tripID]
	if !found {
		return TripUpdate{}, errors.New("no trip update found for trip id")
	}
	return trip, nil
}

type TripUpdate struct {
	Trip           Trip           `json:"trip"`
	StopTimeUpdate StopTimeUpdate `json:"stop_time_update"`
	Vehicle        struct {
		ID           string `json:"id"`
		Label        string `json:"label"`
		LicensePlate string `json:"license_plate"`
	} `json:"vehicle"`
	Timestamp int64  `json:"timestamp"`
	Delay     int64  `json:"delay"`
	ID        string `json:"id"`
}

type StopTimeUpdate struct {
	StopSequence         int64   `json:"stop_sequence"`
	Arrival              Arrival `json:"arrival"`
	Departure            Arrival `json:"departure"`
	StopID               string  `json:"stop_id"`
	ScheduleRelationship int64   `json:"schedule_relationship"`
}

type Arrival struct {
	Delay       int64 `json:"delay"`
	Time        int64 `json:"time"`
	Uncertainty int64 `json:"uncertainty"`
}

type Trip struct {
	TripID               string  `json:"trip_id"`
	StartTime            string  `json:"start_time"`
	StartDate            string  `json:"start_date"`
	ScheduleRelationship int64   `json:"schedule_relationship"`
	RouteID              RouteID `json:"route_id"`
	DirectionID          int64   `json:"direction_id"`
}
