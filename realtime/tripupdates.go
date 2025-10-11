package realtime

import (
	"errors"
	"fmt"
	"sort"
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

	// Merge fetched updates with cache
	if v.tripUpdatesCache.data == nil {
		v.tripUpdatesCache.data = make(TripUpdatesMap)
	}

	// For every trip in this fetch, merge with cache (preserve previous stop updates
	// that are not included in the new feed). After merging, any cached trip that
	// did NOT appear in this fetch will be removed from the cache (as per
	// requirement: only delete trips that do not appear in the fetch).
	for tripId, fetched := range updates {
		if existing, ok := v.tripUpdatesCache.data[tripId]; ok {
			v.tripUpdatesCache.data[tripId] = mergeTripUpdates(existing, fetched)
		} else {
			// store fetched as-is
			v.tripUpdatesCache.data[tripId] = fetched
		}
	}

	// Remove any cached trips that didn't appear in this fetch
	for tid := range v.tripUpdatesCache.data {
		if _, ok := updates[tid]; !ok {
			delete(v.tripUpdatesCache.data, tid)
		}
	}

	v.tripUpdatesCache.lastUpdated = time.Now()

	return v.tripUpdatesCache.data, nil
}

// mergeTripUpdates merges two TripUpdate objects for the same trip id.
// The resulting TripUpdate will contain the union of StopTimeUpdate entries,
// where StopSequence (when present) is used as the primary key and StopId as a
// fallback. For overlapping stops, the fetched entry wins (it represents the
// freshest data). Top-level fields (Timestamp, Vehicle, Delay, TripProperties)
// are taken from whichever TripUpdate has the newer Timestamp.
func mergeTripUpdates(existing, fetched *proto.TripUpdate) *proto.TripUpdate {
	if existing == nil {
		return fetched
	}
	if fetched == nil {
		return existing
	}

	// decide which top-level fields to prefer based on timestamp
	var preferFetched bool
	if fetched.GetTimestamp() >= existing.GetTimestamp() {
		preferFetched = true
	}

	merged := &proto.TripUpdate{}

	// Top-level Trip descriptor, Vehicle, Delay, TripProperties
	if preferFetched {
		merged.Trip = fetched.GetTrip()
		merged.Vehicle = fetched.GetVehicle()
		if fetched.Delay != nil {
			merged.Delay = fetched.Delay
		}
		merged.TripProperties = fetched.GetTripProperties()
	} else {
		merged.Trip = existing.GetTrip()
		merged.Vehicle = existing.GetVehicle()
		if existing.Delay != nil {
			merged.Delay = existing.Delay
		}
		merged.TripProperties = existing.GetTripProperties()
	}

	// Timestamp: keep the max (freshest)
	if fetched.GetTimestamp() >= existing.GetTimestamp() {
		if fetched.Timestamp != nil {
			t := fetched.GetTimestamp()
			merged.Timestamp = &t
		}
	} else if existing.Timestamp != nil {
		t := existing.GetTimestamp()
		merged.Timestamp = &t
	}

	// Build stop map starting from existing (so we preserve historical stops),
	// then overlay fetched entries (fresh values win for the same stop key).
	stopMap := make(map[string]*proto.TripUpdate_StopTimeUpdate)

	for _, stu := range existing.GetStopTimeUpdate() {
		if key := stopKey(stu); key != "" {
			stopMap[key] = stu
		}
	}

	for _, stu := range fetched.GetStopTimeUpdate() {
		if key := stopKey(stu); key != "" {
			// overlay/replace with fetched
			stopMap[key] = stu
		}
	}

	// Convert map back to slice and sort by stop_sequence when available
	var mergedList []*proto.TripUpdate_StopTimeUpdate
	for _, s := range stopMap {
		mergedList = append(mergedList, s)
	}

	sort.SliceStable(mergedList, func(i, j int) bool {
		return mergedList[i].GetStopSequence() < mergedList[j].GetStopSequence()
	})

	merged.StopTimeUpdate = mergedList

	return merged
}

// stopKey returns a stable key for a StopTimeUpdate: prefer stop_sequence when
// set (>0), otherwise fall back to stop_id. Returns empty string if neither is
// available.
func stopKey(s *proto.TripUpdate_StopTimeUpdate) string {
	if s == nil {
		return ""
	}
	if seq := s.GetStopSequence(); seq != 0 {
		return fmt.Sprintf("seq:%d", seq)
	}
	if id := s.GetStopId(); id != "" {
		return "id:" + id
	}
	return ""
}

func (trips TripUpdatesMap) ByTripID(tripID string) (*proto.TripUpdate, error) {
	trip, found := trips[tripID]
	if !found {
		return nil, errors.New("no trip update found for trip id")
	}
	return trip, nil
}
