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

	result, fetchedAt, err := v.tripUpdatesFeed.get(time.Now())
	if err != nil {
		return nil, err
	}
	if v.tripUpdatesCache.data != nil && fetchedAt.Equal(v.tripUpdatesCache.lastUpdated) {
		return v.tripUpdatesCache.data, nil // nothing newer from the feed
	}

	var updates = make(TripUpdatesMap)
	now := time.Now().In(v.localTimeZone)

	for _, i := range result {
		tripUpdate := i.GetTripUpdate()
		if tripUpdate == nil {
			continue // a combined feed's vehicles/alerts
		}
		trip := tripUpdate.GetTrip()
		tripId := trip.GetTripId()
		startDateStr := trip.GetStartDate()
		startTimeStr := trip.GetStartTime()

		// Parse start date + time
		startDateTime, err := parseTripStart(startDateStr, startTimeStr, v.localTimeZone)
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
		existingDateTime, err := parseTripStart(existingTrip.GetStartDate(), existingTrip.GetStartTime(), v.localTimeZone)
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

	v.tripUpdatesCache.lastUpdated = fetchedAt
	v.addTripUpdateHistory(v.tripUpdatesCache.data, fetchedAt)

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
	// When preserving an entry from `existing` that is not present in `fetched`,
	// mark its StopTimeProperties.Historic = true. For fetched entries, ensure
	// Historic = false (or unset) so consumers can find the latest non-historic
	// update.
	stopMap := make(map[string]*proto.TripUpdate_StopTimeUpdate)

	for _, stu := range existing.GetStopTimeUpdate() {
		if key := stopKey(stu); key != "" {
			// clone or use as-is; ensure StopTimeProperties exists and historic=true
			if stu.StopTimeProperties == nil {
				stu.StopTimeProperties = &proto.TripUpdate_StopTimeUpdate_StopTimeProperties{}
			}
			trueVal := true
			stu.StopTimeProperties.Historic = &trueVal
			stopMap[key] = stu
		}
	}

	for _, stu := range fetched.GetStopTimeUpdate() {
		if key := stopKey(stu); key != "" {
			// overlay/replace with fetched; fetched entries are considered current
			// so mark historic = false (unset) to indicate freshness.
			if stu.StopTimeProperties != nil {
				falseVal := false
				stu.StopTimeProperties.Historic = &falseVal
			}
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

// parseTripStart turns a trip descriptor's start_date + start_time into an
// instant in loc. start_time is a GTFS clock: it can pass 24:00:00 and counts
// from "noon minus 12h" of start_date (an hour off midnight on a daylight-saving
// change day), so it can't go through time.Parse.
func parseTripStart(startDate, startTime string, loc *time.Location) (time.Time, error) {
	day, err := time.ParseInLocation("20060102", startDate, loc)
	if err != nil {
		return time.Time{}, err
	}
	var h, m, s int
	if _, err := fmt.Sscanf(startTime, "%d:%d:%d", &h, &m, &s); err != nil {
		return time.Time{}, err
	}
	dayStart := time.Date(day.Year(), day.Month(), day.Day(), 12, 0, 0, 0, loc).Add(-12 * time.Hour)
	return dayStart.Add(time.Duration(h)*time.Hour + time.Duration(m)*time.Minute + time.Duration(s)*time.Second), nil
}
