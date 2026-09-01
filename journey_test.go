package gtfs

import (
	"testing"
	"time"
)

func TestPreferCloserOriginStopOnSameTrip(t *testing.T) {
	departAt := time.Date(2026, 3, 23, 8, 0, 0, 0, time.UTC)
	dayStart := time.Date(2026, 3, 23, 0, 0, 0, 0, time.UTC)

	legs := []JourneyLeg{
		{
			Mode:          "walk",
			DepartureTime: departAt,
			ArrivalTime:   departAt.Add(10 * time.Minute),
			Duration:      10 * time.Minute,
			DistanceKm:    0.8,
			ToStop:        &Stop{StopId: "far", StopName: "Far Stop"},
			TripUsable:    true,
		},
		{
			Mode:                   "transit",
			TripID:                 "trip-1",
			RouteID:                "route-1",
			FromStop:               &Stop{StopId: "far", StopName: "Far Stop"},
			ToStop:                 &Stop{StopId: "downtown", StopName: "Downtown"},
			DepartureTime:          dayStart.Add(15 * time.Hour),
			ArrivalTime:            dayStart.Add(15*time.Hour + 20*time.Minute),
			Duration:               20 * time.Minute,
			ScheduledDepartureTime: dayStart.Add(15 * time.Hour),
			ScheduledArrivalTime:   dayStart.Add(15*time.Hour + 20*time.Minute),
			RealtimeStatus:         "scheduled",
			TripUsable:             true,
		},
	}

	nearbyStartStops := []StopWithDistance{
		{Stop: Stop{StopId: "far", StopName: "Far Stop"}, Distance: 0.8},
		{Stop: Stop{StopId: "close", StopName: "Close Stop"}, Distance: 0.2},
	}

	trips := map[string][]tripStopTime{
		"trip-1": {
			{TripID: "trip-1", StopID: "far", DepartureSec: 15 * 3600, ScheduledDepartureSec: 15 * 3600, TripUsable: true, RealtimeStatus: "scheduled"},
			{TripID: "trip-1", StopID: "close", DepartureSec: 15*3600 + 3*60, ScheduledDepartureSec: 15*3600 + 3*60, TripUsable: true, RealtimeStatus: "scheduled"},
			{TripID: "trip-1", StopID: "downtown", DepartureSec: 15*3600 + 20*60, ArrivalSec: 15*3600 + 20*60, ScheduledArrivalSec: 15*3600 + 20*60, TripUsable: true, RealtimeStatus: "scheduled"},
		},
	}

	stopMap := map[string]Stop{
		"far":      {StopId: "far", StopName: "Far Stop"},
		"close":    {StopId: "close", StopName: "Close Stop"},
		"downtown": {StopId: "downtown", StopName: "Downtown"},
	}

	updated := preferCloserOriginStopOnSameTrip(legs, nearbyStartStops, trips, stopMap, departAt, dayStart, 4.8)

	if got := updated[0].ToStop.StopId; got != "close" {
		t.Fatalf("expected walk leg to end at close stop, got %q", got)
	}
	if got := updated[0].DistanceKm; got != 0.2 {
		t.Fatalf("expected shorter walk distance, got %v", got)
	}
	if got := updated[1].FromStop.StopId; got != "close" {
		t.Fatalf("expected transit leg to board at close stop, got %q", got)
	}
	if got := updated[1].DepartureTime; !got.Equal(dayStart.Add(15*time.Hour + 3*time.Minute)) {
		t.Fatalf("expected later boarding time, got %v", got)
	}
}

func TestPreferCloserOriginStopOnSameTripKeepsOriginalWhenLaterStopUnreachable(t *testing.T) {
	departAt := time.Date(2026, 3, 23, 8, 0, 0, 0, time.UTC)
	dayStart := time.Date(2026, 3, 23, 0, 0, 0, 0, time.UTC)

	legs := []JourneyLeg{
		{
			Mode:          "walk",
			DepartureTime: departAt,
			ArrivalTime:   departAt.Add(10 * time.Minute),
			Duration:      10 * time.Minute,
			DistanceKm:    0.8,
			ToStop:        &Stop{StopId: "far", StopName: "Far Stop"},
			TripUsable:    true,
		},
		{
			Mode:                   "transit",
			TripID:                 "trip-1",
			RouteID:                "route-1",
			FromStop:               &Stop{StopId: "far", StopName: "Far Stop"},
			ToStop:                 &Stop{StopId: "downtown", StopName: "Downtown"},
			DepartureTime:          dayStart.Add(15 * time.Hour),
			ArrivalTime:            dayStart.Add(15*time.Hour + 20*time.Minute),
			Duration:               20 * time.Minute,
			ScheduledDepartureTime: dayStart.Add(15 * time.Hour),
			ScheduledArrivalTime:   dayStart.Add(15*time.Hour + 20*time.Minute),
			RealtimeStatus:         "scheduled",
			TripUsable:             true,
		},
	}

	nearbyStartStops := []StopWithDistance{
		{Stop: Stop{StopId: "far", StopName: "Far Stop"}, Distance: 0.8},
		{Stop: Stop{StopId: "close", StopName: "Close Stop"}, Distance: 0.2},
	}

	trips := map[string][]tripStopTime{
		"trip-1": {
			{TripID: "trip-1", StopID: "far", DepartureSec: 15 * 3600, ScheduledDepartureSec: 15 * 3600, TripUsable: true, RealtimeStatus: "scheduled"},
			{TripID: "trip-1", StopID: "close", DepartureSec: 8*3600 + 2*60, ScheduledDepartureSec: 8*3600 + 2*60, TripUsable: true, RealtimeStatus: "scheduled"},
			{TripID: "trip-1", StopID: "downtown", DepartureSec: 15*3600 + 20*60, ArrivalSec: 15*3600 + 20*60, ScheduledArrivalSec: 15*3600 + 20*60, TripUsable: true, RealtimeStatus: "scheduled"},
		},
	}

	stopMap := map[string]Stop{
		"far":      {StopId: "far", StopName: "Far Stop"},
		"close":    {StopId: "close", StopName: "Close Stop"},
		"downtown": {StopId: "downtown", StopName: "Downtown"},
	}

	updated := preferCloserOriginStopOnSameTrip(legs, nearbyStartStops, trips, stopMap, departAt, dayStart, 4.8)

	if got := updated[0].ToStop.StopId; got != "far" {
		t.Fatalf("expected original stop to remain when closer stop is unreachable, got %q", got)
	}
	if got := updated[1].FromStop.StopId; got != "far" {
		t.Fatalf("expected original boarding stop to remain, got %q", got)
	}
}

func TestNormalizeJourneyRequestKeepsExplicitZeroTransfers(t *testing.T) {
	req := normalizeJourneyRequest(JourneyRequest{
		MaxTransfers: 0,
		MaxResults:   1,
	})

	if req.MaxTransfers != 0 {
		t.Fatalf("expected explicit zero transfers to be preserved, got %d", req.MaxTransfers)
	}
}

func TestNormalizeJourneyRequestUsesDefaultTransfersForNegativeValue(t *testing.T) {
	req := normalizeJourneyRequest(JourneyRequest{
		MaxTransfers: -1,
		MaxResults:   1,
	})

	if req.MaxTransfers != 2 {
		t.Fatalf("expected negative max transfers to default to 2, got %d", req.MaxTransfers)
	}
}

func TestCanBoardTransitAtStopRequiresOneMinuteForTransfers(t *testing.T) {
	if canBoardTransitAtStop(10*60, 10*60, false) != true {
		t.Fatalf("expected non-transfer boarding at same second to be allowed")
	}

	if canBoardTransitAtStop(10*60, 10*60+59, true) != false {
		t.Fatalf("expected transfer boarding with less than one minute to be rejected")
	}

	if canBoardTransitAtStop(10*60, 10*60+60, true) != true {
		t.Fatalf("expected transfer boarding with one minute gap to be allowed")
	}
}

func TestCanAlightForTransitConnectionRequiresOneMinuteForTransfers(t *testing.T) {
	if canAlightForTransitConnection(10*60, 10*60, false) != true {
		t.Fatalf("expected non-transfer alight at same second to be allowed")
	}

	if canAlightForTransitConnection(10*60, 10*60+59, true) != false {
		t.Fatalf("expected transfer alight with less than one minute to be rejected")
	}

	if canAlightForTransitConnection(10*60, 10*60+60, true) != true {
		t.Fatalf("expected transfer alight with one minute gap to be allowed")
	}
}

func TestClampDelaySeconds(t *testing.T) {
	if got := clampDelaySeconds(-2460); got != minTrustedDelaySeconds {
		t.Fatalf("expected bogus 41-min-early delay clamped to %d, got %d", minTrustedDelaySeconds, got)
	}
	if got := clampDelaySeconds(12 * 60 * 60); got != maxTrustedDelaySeconds {
		t.Fatalf("expected 12h delay clamped to %d, got %d", maxTrustedDelaySeconds, got)
	}
	if got := clampDelaySeconds(180); got != 180 {
		t.Fatalf("expected a plausible 3-min delay to pass through, got %d", got)
	}
}

func TestEnforceMonotonicStopTimes(t *testing.T) {
	// Alight stop dragged 41 min early by a bogus realtime adjustment while the
	// board stop kept its scheduled time.
	stopTimes := []tripStopTime{
		{StopID: "board", ArrivalSec: 17 * 3600, DepartureSec: 17 * 3600, ScheduledArrivalSec: 17 * 3600, ScheduledDepartureSec: 17 * 3600},
		{StopID: "alight", ArrivalSec: 17*3600 + 4*60 - 2460, DepartureSec: 17*3600 + 4*60 - 2460, ScheduledArrivalSec: 17*3600 + 4*60, ScheduledDepartureSec: 17*3600 + 4*60},
	}
	enforceMonotonicStopTimes(stopTimes)

	if stopTimes[1].ArrivalSec < stopTimes[0].DepartureSec {
		t.Fatalf("alight arrival %d is before board departure %d", stopTimes[1].ArrivalSec, stopTimes[0].DepartureSec)
	}
	if stopTimes[1].DepartureSec < stopTimes[1].ArrivalSec {
		t.Fatalf("alight departure %d is before its own arrival %d", stopTimes[1].DepartureSec, stopTimes[1].ArrivalSec)
	}
}

func TestBuildTransitLegTimingFallsBackToScheduleOnInversion(t *testing.T) {
	dayStart := time.Date(2026, 3, 23, 0, 0, 0, 0, time.UTC)
	// Adjusted arrival (departSec 17:25, arriveSec 16:44) is before departure.
	timing := buildTransitLegTiming(dayStart, 17*3600+25*60, 16*3600+44*60, 17*3600+21*60, 17*3600+25*60, "early")

	if timing.ArrivalTime.Before(timing.DepartureTime) {
		t.Fatalf("leg arrives (%v) before it departs (%v)", timing.ArrivalTime, timing.DepartureTime)
	}
	if timing.Duration < 0 {
		t.Fatalf("expected non-negative duration, got %v", timing.Duration)
	}
	if timing.RealtimeStatus != "scheduled" {
		t.Fatalf("expected fallback to scheduled status, got %q", timing.RealtimeStatus)
	}
	if !timing.DepartureTime.Equal(dayStart.Add(17*time.Hour + 21*time.Minute)) {
		t.Fatalf("expected scheduled departure 17:21, got %v", timing.DepartureTime)
	}
}
