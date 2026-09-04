package realtime

import (
	"errors"
	"sort"
	"sync"
	"time"

	gtfsproto "github.com/jfmow/gtfs/realtime/proto"
	googleProto "google.golang.org/protobuf/proto"
)

type tripHistoryCache struct {
	mu sync.Mutex
	// enabled is off by default - the sampling loop deep-clones every trip
	// update / vehicle position on every feed poll and only evicts once a trip's
	// end time can be inferred, so it can grow to hundreds of MB over a service
	// day. Callers that actually read GetTripHistory opt in via EnableTripHistory.
	enabled bool
	data    TripHistoryMap
}

// TripHistoryMap stores realtime samples by trip id for the active trip period.
type TripHistoryMap map[string]*TripHistory

// TripHistory contains the vehicle positions and trip updates seen for a trip.
// Entries are retained until the trip end time can be inferred from trip updates
// and has passed.
type TripHistory struct {
	TripID      string                       `json:"trip_id"`
	Locations   []*gtfsproto.VehiclePosition `json:"locations"`
	TripUpdates []*gtfsproto.TripUpdate      `json:"trip_updates"`
	ExpiresAt   time.Time                    `json:"expires_at,omitempty"`
}

// EnableTripHistory turns on the per-trip realtime sampling that GetTripHistory
// returns. It is off by default (see tripHistoryCache.enabled). tripHistoryCache
// is a shared pointer across Realtime value copies, so enabling it on any copy
// enables it for all of them.
func (v Realtime) EnableTripHistory() {
	v.tripHistoryCache.mu.Lock()
	v.tripHistoryCache.enabled = true
	v.tripHistoryCache.mu.Unlock()
}

func (v Realtime) addVehicleHistory(vehicles VehiclesMap, now time.Time) {
	v.tripHistoryCache.mu.Lock()
	defer v.tripHistoryCache.mu.Unlock()

	if !v.tripHistoryCache.enabled {
		return
	}
	v.expireTripHistoryLocked(now)
	for tripID, vehicle := range vehicles {
		if tripID == "" || vehicle == nil {
			continue
		}
		history := v.tripHistoryLocked(tripID)
		if !hasVehicleSample(history.Locations, vehicle) {
			history.Locations = append(history.Locations, cloneVehiclePosition(vehicle))
		}
	}
}

func (v Realtime) addTripUpdateHistory(updates TripUpdatesMap, now time.Time) {
	v.tripHistoryCache.mu.Lock()
	defer v.tripHistoryCache.mu.Unlock()

	if !v.tripHistoryCache.enabled {
		return
	}
	v.expireTripHistoryLocked(now)
	for tripID, update := range updates {
		if tripID == "" || update == nil {
			continue
		}
		history := v.tripHistoryLocked(tripID)
		if expiresAt, ok := tripUpdateEndTime(update); ok && (history.ExpiresAt.IsZero() || expiresAt.After(history.ExpiresAt)) {
			history.ExpiresAt = expiresAt
		}
		if !hasTripUpdateSample(history.TripUpdates, update) {
			history.TripUpdates = append(history.TripUpdates, cloneTripUpdate(update))
		}
	}
}

func (v Realtime) tripHistoryLocked(tripID string) *TripHistory {
	if v.tripHistoryCache.data == nil {
		v.tripHistoryCache.data = make(TripHistoryMap)
	}
	if _, ok := v.tripHistoryCache.data[tripID]; !ok {
		v.tripHistoryCache.data[tripID] = &TripHistory{TripID: tripID}
	}
	return v.tripHistoryCache.data[tripID]
}

func (v Realtime) expireTripHistoryLocked(now time.Time) {
	for tripID, history := range v.tripHistoryCache.data {
		if !history.ExpiresAt.IsZero() && !history.ExpiresAt.After(now) {
			delete(v.tripHistoryCache.data, tripID)
		}
	}
}

// GetTripHistory returns retained vehicle location and trip update samples by trip id.
func (v Realtime) GetTripHistory() TripHistoryMap {
	v.tripHistoryCache.mu.Lock()
	defer v.tripHistoryCache.mu.Unlock()

	v.expireTripHistoryLocked(time.Now())
	result := make(TripHistoryMap, len(v.tripHistoryCache.data))
	for tripID, history := range v.tripHistoryCache.data {
		result[tripID] = cloneTripHistory(history)
	}
	return result
}

// GetTripHistoryByTripID returns retained vehicle location and trip update samples for a trip id.
func (v Realtime) GetTripHistoryByTripID(tripID string) (*TripHistory, error) {
	history, found := v.GetTripHistory()[tripID]
	if !found {
		return nil, errors.New("no trip history found for trip id")
	}
	return history, nil
}

func (history TripHistoryMap) ByTripID(tripID string) (*TripHistory, error) {
	tripHistory, found := history[tripID]
	if !found {
		return nil, errors.New("no trip history found for trip id")
	}
	return tripHistory, nil
}

func tripUpdateEndTime(update *gtfsproto.TripUpdate) (time.Time, bool) {
	var end int64
	for _, stopUpdate := range update.GetStopTimeUpdate() {
		if arrival := stopUpdate.GetArrival(); arrival != nil && arrival.GetTime() > end {
			end = arrival.GetTime()
		}
		if departure := stopUpdate.GetDeparture(); departure != nil && departure.GetTime() > end {
			end = departure.GetTime()
		}
	}
	if end == 0 {
		return time.Time{}, false
	}
	return time.Unix(end, 0), true
}

func hasVehicleSample(samples []*gtfsproto.VehiclePosition, vehicle *gtfsproto.VehiclePosition) bool {
	for _, sample := range samples {
		if sample.GetTimestamp() == vehicle.GetTimestamp() && sample.GetStopId() == vehicle.GetStopId() && sample.GetCurrentStopSequence() == vehicle.GetCurrentStopSequence() {
			return true
		}
	}
	return false
}

func hasTripUpdateSample(samples []*gtfsproto.TripUpdate, update *gtfsproto.TripUpdate) bool {
	for _, sample := range samples {
		if sample.GetTimestamp() == update.GetTimestamp() {
			return true
		}
	}
	return false
}

func cloneTripHistory(history *TripHistory) *TripHistory {
	clone := &TripHistory{TripID: history.TripID, ExpiresAt: history.ExpiresAt}
	for _, location := range history.Locations {
		clone.Locations = append(clone.Locations, cloneVehiclePosition(location))
	}
	for _, update := range history.TripUpdates {
		clone.TripUpdates = append(clone.TripUpdates, cloneTripUpdate(update))
	}
	sort.SliceStable(clone.Locations, func(i, j int) bool { return clone.Locations[i].GetTimestamp() < clone.Locations[j].GetTimestamp() })
	sort.SliceStable(clone.TripUpdates, func(i, j int) bool { return clone.TripUpdates[i].GetTimestamp() < clone.TripUpdates[j].GetTimestamp() })
	return clone
}

func cloneVehiclePosition(vehicle *gtfsproto.VehiclePosition) *gtfsproto.VehiclePosition {
	if vehicle == nil {
		return nil
	}
	return googleProto.Clone(vehicle).(*gtfsproto.VehiclePosition)
}

func cloneTripUpdate(update *gtfsproto.TripUpdate) *gtfsproto.TripUpdate {
	if update == nil {
		return nil
	}
	return googleProto.Clone(update).(*gtfsproto.TripUpdate)
}
