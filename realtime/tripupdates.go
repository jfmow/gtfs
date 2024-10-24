package realtime

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

var (
	tripUpdateClient          = &http.Client{}
	tripUpdateApiRequestMutex sync.Mutex
)

var (
	cachedTripUpdatesData       TripUpdatesMap
	lastUpdatedTripUpdatesCache time.Time
)

type TripUpdatesMap map[string]TripUpdate

func (v tripUpdates) GetTripUpdates() (TripUpdatesMap, error) {
	tripUpdateApiRequestMutex.Lock()
	defer tripUpdateApiRequestMutex.Unlock()
	if len(cachedTripUpdatesData) >= 1 && lastUpdatedTripUpdatesCache.Add(15*time.Second).After(time.Now()) {
		return cachedTripUpdatesData, nil
	}

	url := v.url
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("error creating request: %w", err)
	}
	req.Header.Set("Cache-Control", "no-cache")
	if v.apiHeader != "" {
		req.Header.Set(v.apiHeader, v.apiKey)
	}

	resp, err := tripUpdateClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error making request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading response body: %w", err)
	}

	var result TripUpdatesResponse
	err = json.Unmarshal(body, &result)
	if err != nil {
		return nil, fmt.Errorf("error parsing JSON: %w", err)
	}

	var updates = make(TripUpdatesMap)

	for _, i := range result.Response.Entity {
		updates[i.TripUpdate.Trip.TripID] = i.TripUpdate
	}

	cachedTripUpdatesData = updates
	lastUpdatedTripUpdatesCache = time.Now()

	return updates, nil
}

func (trips TripUpdatesMap) ByTripID(tripID string) (TripUpdate, error) {
	trip, found := trips[tripID]
	if !found {
		return TripUpdate{}, errors.New("no vehicle found for trip id")
	}
	return trip, nil
}

type TripUpdatesResponse struct {
	Status   string `json:"status"`
	Response struct {
		Header struct {
			Timestamp           float64 `json:"timestamp"`
			GtfsRealtimeVersion string  `json:"gtfs_realtime_version"`
			Incrementality      int64   `json:"incrementality"`
		} `json:"header"`
		Entity []struct {
			ID         string     `json:"id"`
			TripUpdate TripUpdate `json:"trip_update"`
			IsDeleted  bool       `json:"is_deleted"`
		} `json:"entity"`
	} `json:"response"`
}

type TripUpdate struct {
	Trip           Trip           `json:"trip"`
	StopTimeUpdate StopTimeUpdate `json:"stop_time_update"`
	Vehicle        Vehicle        `json:"vehicle"`
	Timestamp      int64          `json:"timestamp"`
	Delay          int64          `json:"delay"`
}

type StopTimeUpdate struct {
	StopSequence         int64   `json:"stop_sequence"`
	Arrival              Arrival `json:"arrival"`
	Departure            Arrival `json:"departure"`
	StopID               string  `json:"stop_id"`
	ScheduleRelationship int64   `json:"schedule_relationship"`
}

type Arrival struct {
	Delay int64 `json:"delay"`
	Time  int64 `json:"time"`
}

type Trip struct {
	TripID               string  `json:"trip_id"`
	StartTime            string  `json:"start_time"`
	StartDate            string  `json:"start_date"`
	ScheduleRelationship int64   `json:"schedule_relationship"`
	RouteID              RouteID `json:"route_id"`
	DirectionID          int64   `json:"direction_id"`
}
