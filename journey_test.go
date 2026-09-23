package gtfs

import (
	"fmt"
	"math"
	"strings"
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
			{TripID: "trip-1", StopID: "far", DepartureSec: 15 * 3600, ScheduledDepartureSec: 15 * 3600, TripUsable: true, Boardable: true, Alightable: true, RealtimeStatus: "scheduled"},
			{TripID: "trip-1", StopID: "close", DepartureSec: 15*3600 + 3*60, ScheduledDepartureSec: 15*3600 + 3*60, TripUsable: true, Boardable: true, Alightable: true, RealtimeStatus: "scheduled"},
			{TripID: "trip-1", StopID: "downtown", DepartureSec: 15*3600 + 20*60, ArrivalSec: 15*3600 + 20*60, ScheduledArrivalSec: 15*3600 + 20*60, TripUsable: true, Boardable: true, Alightable: true, RealtimeStatus: "scheduled"},
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
	if canBoardTransitAtStop(10*60, 10*60, false, minDirectTransferSeconds) != true {
		t.Fatalf("expected non-transfer boarding at same second to be allowed")
	}

	if canBoardTransitAtStop(10*60, 10*60+59, true, minDirectTransferSeconds) != false {
		t.Fatalf("expected transfer boarding with less than one minute to be rejected")
	}

	if canBoardTransitAtStop(10*60, 10*60+60, true, minDirectTransferSeconds) != true {
		t.Fatalf("expected transfer boarding with one minute gap to be allowed")
	}
}

func TestCanAlightForTransitConnectionRequiresOneMinuteForTransfers(t *testing.T) {
	if canAlightForTransitConnection(10*60, 10*60, false, minDirectTransferSeconds) != true {
		t.Fatalf("expected non-transfer alight at same second to be allowed")
	}

	if canAlightForTransitConnection(10*60, 10*60+59, true, minDirectTransferSeconds) != false {
		t.Fatalf("expected transfer alight with less than one minute to be rejected")
	}

	if canAlightForTransitConnection(10*60, 10*60+60, true, minDirectTransferSeconds) != true {
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

func TestEnforceMonotonicStopTimesRelabelsDraggedStop(t *testing.T) {
	// A downstream stop kept "on_time" from an early realtime pass, then a big
	// upstream delay drags it 6 min past schedule via the monotonic clamp.
	stopTimes := []tripStopTime{
		{StopID: "a", ArrivalSec: 8*3600 + 6*60, DepartureSec: 8*3600 + 6*60, ScheduledArrivalSec: 8 * 3600, ScheduledDepartureSec: 8 * 3600, RealtimeStatus: "delayed"},
		{StopID: "b", ArrivalSec: 8*3600 + 3*60, DepartureSec: 8*3600 + 3*60, ScheduledArrivalSec: 8*3600 + 3*60, ScheduledDepartureSec: 8*3600 + 3*60, RealtimeStatus: "on_time"},
	}
	enforceMonotonicStopTimes(stopTimes)

	if stopTimes[1].ArrivalSec != 8*3600+6*60 {
		t.Fatalf("expected stop b clamped forward to 08:06, got %d", stopTimes[1].ArrivalSec)
	}
	if stopTimes[1].RealtimeStatus != "delayed" {
		t.Fatalf("expected dragged-forward stop b relabelled 'delayed', got %q", stopTimes[1].RealtimeStatus)
	}
}

func TestDeferOriginWalkRemovesLeadingWait(t *testing.T) {
	day := time.Date(2026, 3, 23, 0, 0, 0, 0, time.UTC)
	walkDur := 9 * time.Minute
	trainDep := day.Add(20*time.Hour + 20*time.Minute) // 20:20

	legs := []JourneyLeg{
		{Mode: "walk", DepartureTime: day.Add(19*time.Hour + 42*time.Minute), ArrivalTime: day.Add(19*time.Hour + 51*time.Minute), Duration: walkDur},
		{Mode: "transit", DepartureTime: trainDep, ArrivalTime: trainDep.Add(18 * time.Minute), Duration: 18 * time.Minute},
	}
	out := deferOriginWalk(legs)

	wantArrive := trainDep.Add(-originWalkBufferSeconds * time.Second) // 20:18
	if !out[0].ArrivalTime.Equal(wantArrive) {
		t.Fatalf("expected walk to arrive at %v (train - 2min), got %v", wantArrive, out[0].ArrivalTime)
	}
	if !out[0].DepartureTime.Equal(wantArrive.Add(-walkDur)) {
		t.Fatalf("expected walk to start at %v, got %v", wantArrive.Add(-walkDur), out[0].DepartureTime)
	}
	if out[0].ArrivalTime.Sub(out[0].DepartureTime) != walkDur {
		t.Fatalf("walk duration changed: %v", out[0].ArrivalTime.Sub(out[0].DepartureTime))
	}

	// A tight connection (well under the buffer) is left untouched.
	tight := []JourneyLeg{
		{Mode: "walk", DepartureTime: day, ArrivalTime: day.Add(walkDur), Duration: walkDur},
		{Mode: "transit", DepartureTime: day.Add(walkDur + 30*time.Second), ArrivalTime: day.Add(walkDur + 20*time.Minute)},
	}
	tightOut := deferOriginWalk(tight)
	if !tightOut[0].DepartureTime.Equal(day) {
		t.Fatalf("expected a tight connection's walk left at its original start, got %v", tightOut[0].DepartureTime)
	}
}

func TestBuildStopTransferGraphLinksNearbyStops(t *testing.T) {
	// A ~300 m gap (different stop_ids, no shared parent) - the case RAPTOR
	// couldn't bridge before: e.g. route 70's Greenlane stop and route 65's.
	stopMap := map[string]Stop{
		"a": {StopId: "a", StopName: "Great South Road/Market Road", StopLat: -36.8890, StopLon: 174.8010},
		"b": {StopId: "b", StopName: "Green Lane West/Great South Road", StopLat: -36.8912, StopLon: 174.8022},
		// Far away - must NOT be linked.
		"far": {StopId: "far", StopName: "Far Stop", StopLat: -36.8500, StopLon: 174.7600},
		// Same pole (~20 m) - below the min, must NOT be linked.
		"a2": {StopId: "a2", StopName: "Great South Road/Market Road (other side)", StopLat: -36.88902, StopLon: 174.80098},
	}

	g := buildStopTransferGraph(stopMap)

	hasEdge := func(from, to string) bool {
		for _, tr := range g[from] {
			if tr.ToStopID == to {
				return true
			}
		}
		return false
	}

	if !hasEdge("a", "b") || !hasEdge("b", "a") {
		t.Fatalf("expected a<->b foot transfer, got %+v", g)
	}
	if hasEdge("a", "far") {
		t.Fatalf("did not expect a->far foot transfer")
	}
	if hasEdge("a", "a2") {
		t.Fatalf("did not expect a sub-%.0fm 'transfer' a->a2", footTransferMinKm*1000)
	}
	for _, tr := range g["a"] {
		if tr.ToStopID == "b" && tr.WalkSec <= footTransferBufferSec {
			t.Fatalf("walk time should include travel + buffer, got %ds", tr.WalkSec)
		}
	}
}

func TestRelaxFootTransfersImprovesArrivalAndChains(t *testing.T) {
	graph := map[string][]stopTransfer{
		"onroute": {{ToStopID: "otherroute", WalkSec: 240}},
	}
	arrival := map[string]int{
		"onroute":    8 * 3600,
		"otherroute": math.MaxInt32,
	}
	pred := map[string]stopPredecessor{}
	nextUpdated := map[string]bool{"onroute": true}

	relaxFootTransfers([]string{"onroute"}, graph, arrival, pred, nextUpdated)

	if got, want := arrival["otherroute"], 8*3600+240; got != want {
		t.Fatalf("expected otherroute arrival %d, got %d", want, got)
	}
	if !nextUpdated["otherroute"] {
		t.Fatalf("otherroute should be marked so the next round boards there")
	}
	if p := pred["otherroute"]; p.Mode != "walk-transfer" || p.FromStopID != "onroute" {
		t.Fatalf("unexpected predecessor: %+v", p)
	}
}

func TestRaptorDepartScanRespectsPickupDropoff(t *testing.T) {
	// Two-stop ferry-style trip: board only at A (pickup ok), alight only at B
	// (B has pickup_type=1 -> Boardable false). The rider must still be able to
	// ride A->B.
	trips := map[string][]tripStopTime{
		"ferry": {
			{TripID: "ferry", StopID: "A", DepartureSec: 8 * 3600, ScheduledDepartureSec: 8 * 3600, TripUsable: true, Boardable: true, Alightable: true},
			{TripID: "ferry", StopID: "B", ArrivalSec: 8*3600 + 12*60, ScheduledArrivalSec: 8*3600 + 12*60, TripUsable: true, Boardable: false, Alightable: true},
		},
	}
	stopMap := map[string]Stop{
		"A": {StopId: "A", StopLat: -36.84, StopLon: 174.77},
		"B": {StopId: "B", StopLat: -36.83, StopLon: 174.80},
	}
	near := []StopWithDistance{{Stop: stopMap["A"], Distance: 0.05}}

	arrival, pred := raptorDepartScan(trips, stopMap, map[string][]stopTransfer{}, near, 7*3600+55*60, 2, 4.8, minDirectTransferSeconds, nil, nil)

	if arrival["B"] != 8*3600+12*60 {
		t.Fatalf("expected to ride ferry A->B, arrival[B]=%d", arrival["B"])
	}
	if pred["B"].Mode != "transit" || pred["B"].FromStopID != "A" {
		t.Fatalf("unexpected predecessor for B: %+v", pred["B"])
	}

	// And a rider starting at B must NOT be able to board the ferry there.
	nearB := []StopWithDistance{{Stop: stopMap["B"], Distance: 0.05}}
	arrival2, _ := raptorDepartScan(trips, stopMap, map[string][]stopTransfer{}, nearB, 7*3600+55*60, 2, 4.8, minDirectTransferSeconds, nil, nil)
	if arrival2["A"] != math.MaxInt32 {
		t.Fatalf("should not be able to board at set-down-only stop B; arrival[A]=%d", arrival2["A"])
	}
}

func TestBuildStopTransferGraphKeepsFerryStops(t *testing.T) {
	// A CBD bus stop with many nearby bus poles and one ferry wharf just past
	// the trim cutoff - the wharf must survive.
	stopMap := map[string]Stop{
		"bus0": {StopId: "bus0", StopName: "Queen Street", StopType: "bus", StopLat: -36.8460, StopLon: 174.7660},
	}
	for i := 0; i < 12; i++ {
		id := "b" + string(rune('A'+i))
		stopMap[id] = Stop{StopId: id, StopName: id, StopType: "bus", StopLat: -36.8460 + float64(i+1)*0.0002, StopLon: 174.7660}
	}
	stopMap["wharf"] = Stop{StopId: "wharf", StopName: "Downtown Ferry Terminal Pier 4", StopType: "ferry", StopLat: -36.8458, StopLon: 174.7690} // ~270 m away

	g := buildStopTransferGraph(stopMap)
	found := false
	for _, tr := range g["bus0"] {
		if tr.ToStopID == "wharf" {
			found = true
		}
	}
	if !found {
		t.Fatalf("ferry wharf trimmed from bus0 transfers: %+v", g["bus0"])
	}
}

func TestBuildStopTransferGraphLinksSameStationDifferentPlatform(t *testing.T) {
	// Karanga-a-Hape's two platforms: same parent_station, ~40 m apart - under
	// footTransferMinKm, so the ordinary distance-based rule would drop them,
	// and the old same-stationKey skip dropped them outright. A rider must
	// still be able to cross from one line's platform to the other's.
	stopMap := map[string]Stop{
		"p1": {StopId: "p1", StopName: "Karanga-a-Hape Train Station 1", ParentStation: "station", StopType: "train", StopLat: -36.85816, StopLon: 174.75879},
		"p2": {StopId: "p2", StopName: "Karanga-a-Hape Train Station 2", ParentStation: "station", StopType: "train", StopLat: -36.85809, StopLon: 174.75923},
	}

	g := buildStopTransferGraph(stopMap)

	var tr *stopTransfer
	for i, t := range g["p1"] {
		if t.ToStopID == "p2" {
			tr = &g["p1"][i]
		}
	}
	if tr == nil {
		t.Fatalf("expected p1->p2 same-station transfer, got %+v", g["p1"])
	}
	if tr.WalkSec != sameStationTransferSec {
		t.Fatalf("expected fixed same-station change time %ds, got %ds", sameStationTransferSec, tr.WalkSec)
	}
	if !tr.rare {
		t.Fatalf("same-station transfer must never be trimmed")
	}
}

func TestMergeAdjacentWalkLegs(t *testing.T) {
	base := time.Date(2026, 3, 23, 8, 0, 0, 0, time.UTC)
	legs := []JourneyLeg{
		{Mode: "walk", DepartureTime: base, ArrivalTime: base.Add(2 * time.Minute), Duration: 2 * time.Minute, DistanceKm: 0.15},
		{Mode: "walk", DepartureTime: base.Add(2 * time.Minute), ArrivalTime: base.Add(6 * time.Minute), Duration: 4 * time.Minute, DistanceKm: 0.30},
		{Mode: "transit", DepartureTime: base.Add(8 * time.Minute), ArrivalTime: base.Add(20 * time.Minute)},
	}
	out := mergeAdjacentWalkLegs(legs)
	if len(out) != 2 {
		t.Fatalf("expected 2 legs after merge, got %d", len(out))
	}
	if out[0].Duration != 6*time.Minute || math.Abs(out[0].DistanceKm-0.45) > 1e-6 {
		t.Fatalf("merged walk leg wrong: dur=%v dist=%v", out[0].Duration, out[0].DistanceKm)
	}
}

func TestCountTransfers(t *testing.T) {
	base := time.Now()
	tl := func(mode, routeID string) JourneyLeg {
		return JourneyLeg{Mode: mode, RouteID: routeID, DepartureTime: base, ArrivalTime: base}
	}
	cases := []struct {
		name string
		legs []JourneyLeg
		want int
	}{
		{"walk only", []JourneyLeg{tl("walk", "")}, 0},
		{"single transit leg", []JourneyLeg{tl("walk", ""), tl("transit", "A"), tl("walk", "")}, 0},
		{"two different routes via a walk", []JourneyLeg{tl("walk", ""), tl("transit", "A"), tl("walk", ""), tl("transit", "B"), tl("walk", "")}, 1},
		{"three transit legs, two route changes", []JourneyLeg{tl("walk", ""), tl("transit", "A"), tl("transit", "B"), tl("walk", ""), tl("transit", "C")}, 2},
		{"same route split across two trips isn't a transfer", []JourneyLeg{tl("walk", ""), tl("transit", "A"), tl("transit", "A"), tl("walk", "")}, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := countTransfers(c.legs); got != c.want {
				t.Fatalf("got %d want %d", got, c.want)
			}
		})
	}
}

func TestRouteAllowed(t *testing.T) {
	cases := []struct {
		name    string
		routeID string
		only    map[string]bool
		banned  map[string]bool
		want    bool
	}{
		{"no filters", "70", nil, nil, true},
		{"banned", "70", nil, map[string]bool{"70": true}, false},
		{"not banned", "70", nil, map[string]bool{"71": true}, true},
		{"only allows listed", "70", map[string]bool{"70": true}, nil, true},
		{"only excludes unlisted", "70", map[string]bool{"71": true}, nil, false},
		{"banned wins over only", "70", map[string]bool{"70": true}, map[string]bool{"70": true}, false},
	}
	for _, c := range cases {
		if got := routeAllowed(c.routeID, c.only, c.banned); got != c.want {
			t.Fatalf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}

func TestCleanRouteIDs(t *testing.T) {
	if got := cleanRouteIDs(nil); got != nil {
		t.Fatalf("expected nil for nil input, got %v", got)
	}
	if got := cleanRouteIDs([]string{"", "  "}); got != nil {
		t.Fatalf("expected nil when all entries are blank, got %v", got)
	}
	got := cleanRouteIDs([]string{" 70 ", "", "NEX"})
	if len(got) != 2 || got[0] != "70" || got[1] != "NEX" {
		t.Fatalf("unexpected cleaned route IDs: %v", got)
	}
}

func TestRaptorDepartScanOnlyRoutesRestrictsBoarding(t *testing.T) {
	// Two parallel trips connect A to B: one on route "70", one on route "71".
	// With onlyRoutes={"71"}, the "70" trip must be skipped even though it's
	// otherwise a valid, faster boarding.
	trips := map[string][]tripStopTime{
		"trip70": {
			{TripID: "trip70", RouteID: "70", StopID: "A", DepartureSec: 8 * 3600, TripUsable: true, Boardable: true, Alightable: true},
			{TripID: "trip70", RouteID: "70", StopID: "B", ArrivalSec: 8*3600 + 5*60, TripUsable: true, Boardable: true, Alightable: true},
		},
		"trip71": {
			{TripID: "trip71", RouteID: "71", StopID: "A", DepartureSec: 8 * 3600, TripUsable: true, Boardable: true, Alightable: true},
			{TripID: "trip71", RouteID: "71", StopID: "B", ArrivalSec: 8*3600 + 15*60, TripUsable: true, Boardable: true, Alightable: true},
		},
	}
	stopMap := map[string]Stop{
		"A": {StopId: "A", StopLat: -36.84, StopLon: 174.77},
		"B": {StopId: "B", StopLat: -36.83, StopLon: 174.80},
	}
	near := []StopWithDistance{{Stop: stopMap["A"], Distance: 0.05}}

	arrival, pred := raptorDepartScan(trips, stopMap, map[string][]stopTransfer{}, near, 7*3600+55*60, 2, 4.8, minDirectTransferSeconds, nil, map[string]bool{"71": true})
	if arrival["B"] != 8*3600+15*60 {
		t.Fatalf("expected onlyRoutes to force the slower route 71 trip, arrival[B]=%d", arrival["B"])
	}
	if pred["B"].RouteID != "71" {
		t.Fatalf("expected predecessor route 71, got %q", pred["B"].RouteID)
	}
}

// TestFilterNearbyStopsKeepsTrainStopBeyondCap reproduces a real journey that
// used to fail with OnlyRouteIDs set to a train line: a dense CBD destination
// has 50+ bus poles within the walk radius, and the actual train platform
// (further away than all of them, but still in range) used to be trimmed by
// the maxStops cap - even a same-named bus interchange bay a few metres
// closer than the platform counted against it, though no train calls there.
// RAPTOR then never saw the train as reachable, regardless of how good the
// connection was.
func TestFilterNearbyStopsKeepsTrainStopBeyondCap(t *testing.T) {
	stopMap := map[string]Stop{}
	// 55 bus stops, all closer than the train platform.
	for i := 0; i < 55; i++ {
		id := fmt.Sprintf("bus%d", i)
		stopMap[id] = Stop{StopId: id, StopName: id, StopType: "bus", StopLat: -36.8520 + float64(i)*0.0002, StopLon: 174.7690}
	}
	// A same-named bus interchange bay, closer than the platform, but not a
	// train stop - it must not be mistaken for the real thing.
	stopMap["decoy"] = Stop{StopId: "decoy", StopName: "Stop D Example Station", StopType: "bus", StopLat: -36.8525, StopLon: 174.7691}
	stopMap["platform"] = Stop{StopId: "platform", StopName: "Example Train Station", StopType: "train", StopLat: -36.8600, StopLon: 174.7700} // ~0.9km away

	near := filterNearbyStops(stopMap, -36.8526, 174.7690, 1.0, 50)

	foundPlatform := false
	for _, sd := range near {
		if sd.Stop.StopId == "platform" {
			foundPlatform = true
		}
	}
	if !foundPlatform {
		t.Fatalf("train platform trimmed from nearby stops despite being in range: %d candidates returned", len(near))
	}
}

func TestNoOnlyRouteJourneyError(t *testing.T) {
	generic := noOnlyRouteJourneyError(JourneyRequest{})
	if !strings.Contains(generic.Error(), "no journey found between") {
		t.Fatalf("expected generic message, got %q", generic.Error())
	}
	withOnly := noOnlyRouteJourneyError(JourneyRequest{OnlyRouteIDs: []string{"70"}})
	if !strings.Contains(withOnly.Error(), "only the selected routes") {
		t.Fatalf("expected only-routes-specific message, got %q", withOnly.Error())
	}
}

func TestAllowedRouteSetFiltersByMode(t *testing.T) {
	routeMap := map[string]Route{
		"bus70":   {RouteId: "bus70", RouteType: 3},
		"bus71":   {RouteId: "bus71", RouteType: 3},
		"eastern": {RouteId: "eastern", RouteType: 2},
		"ferry":   {RouteId: "ferry", RouteType: 4},
	}

	if got := allowedRouteSet(routeMap, nil, nil); got != nil {
		t.Fatalf("no filters should mean no restriction, got %v", got)
	}

	trainOrFerry := allowedRouteSet(routeMap, nil, []int{2, 4})
	if !trainOrFerry["eastern"] || !trainOrFerry["ferry"] || trainOrFerry["bus70"] || len(trainOrFerry) != 2 {
		t.Fatalf("expected only the train and ferry, got %v", trainOrFerry)
	}

	// Both filters: a route must pass each.
	both := allowedRouteSet(routeMap, []string{"bus70", "eastern"}, []int{3})
	if !both["bus70"] || len(both) != 1 {
		t.Fatalf("expected just bus70, got %v", both)
	}

	// A mode with no routes must block everything, not lift the restriction.
	none := allowedRouteSet(routeMap, nil, []int{5})
	if len(none) == 0 || routeAllowed("bus70", none, nil) {
		t.Fatalf("a mode with no routes must allow nothing, got %v", none)
	}
}

func TestRaptorDepartScanModeFilterSkipsOtherModes(t *testing.T) {
	// A fast train and a slow bus both run A -> B. Bus only must take the bus.
	trips := map[string][]tripStopTime{
		"train": {
			{TripID: "train", RouteID: "eastern", StopID: "A", DepartureSec: 8 * 3600, TripUsable: true, Boardable: true, Alightable: true},
			{TripID: "train", RouteID: "eastern", StopID: "B", ArrivalSec: 8*3600 + 5*60, TripUsable: true, Boardable: true, Alightable: true},
		},
		"bus": {
			{TripID: "bus", RouteID: "bus70", StopID: "A", DepartureSec: 8 * 3600, TripUsable: true, Boardable: true, Alightable: true},
			{TripID: "bus", RouteID: "bus70", StopID: "B", ArrivalSec: 8*3600 + 20*60, TripUsable: true, Boardable: true, Alightable: true},
		},
	}
	stopMap := map[string]Stop{
		"A": {StopId: "A", StopLat: -36.84, StopLon: 174.77},
		"B": {StopId: "B", StopLat: -36.83, StopLon: 174.80},
	}
	routeMap := map[string]Route{
		"eastern": {RouteId: "eastern", RouteType: 2},
		"bus70":   {RouteId: "bus70", RouteType: 3},
	}
	near := []StopWithDistance{{Stop: stopMap["A"], Distance: 0.05}}

	only := allowedRouteSet(routeMap, nil, []int{3})
	_, pred := raptorDepartScan(trips, stopMap, map[string][]stopTransfer{}, near, 7*3600+55*60, 2, 4.8, minDirectTransferSeconds, nil, only)
	if pred["B"].RouteID != "bus70" {
		t.Fatalf("expected the bus, got route %q", pred["B"].RouteID)
	}
}

func TestRaptorDepartScanMinTransferSecRejectsRushedChange(t *testing.T) {
	// Route 1 reaches X at 8:10; route 2 leaves X at 8:12. A two-minute change
	// is fine normally, but not when the rider asked for three extra minutes.
	trips := map[string][]tripStopTime{
		"t1": {
			{TripID: "t1", RouteID: "1", StopID: "A", DepartureSec: 8 * 3600, TripUsable: true, Boardable: true, Alightable: true},
			{TripID: "t1", RouteID: "1", StopID: "X", ArrivalSec: 8*3600 + 10*60, TripUsable: true, Boardable: true, Alightable: true},
		},
		"t2": {
			{TripID: "t2", RouteID: "2", StopID: "X", DepartureSec: 8*3600 + 12*60, TripUsable: true, Boardable: true, Alightable: true},
			{TripID: "t2", RouteID: "2", StopID: "B", ArrivalSec: 8*3600 + 20*60, TripUsable: true, Boardable: true, Alightable: true},
		},
	}
	stopMap := map[string]Stop{
		"A": {StopId: "A"}, "X": {StopId: "X"}, "B": {StopId: "B"},
	}
	near := []StopWithDistance{{Stop: stopMap["A"], Distance: 0.05}}

	arrival, _ := raptorDepartScan(trips, stopMap, map[string][]stopTransfer{}, near, 7*3600+55*60, 2, 4.8, minDirectTransferSeconds, nil, nil)
	if arrival["B"] != 8*3600+20*60 {
		t.Fatalf("expected the normal two-minute change to work, arrival[B]=%d", arrival["B"])
	}
	arrival, _ = raptorDepartScan(trips, stopMap, map[string][]stopTransfer{}, near, 7*3600+55*60, 2, 4.8, minDirectTransferSeconds+180, nil, nil)
	if arrival["B"] != math.MaxInt32 {
		t.Fatalf("expected the rushed change to be rejected, arrival[B]=%d", arrival["B"])
	}
}

func TestTransferGraphAtSpeedRetimesForSlowWalkers(t *testing.T) {
	graph := map[string][]stopTransfer{
		"a": {
			{ToStopID: "b", WalkSec: walkDurationSeconds(0.3, footTransferWalkSpeedKm) + footTransferBufferSec, DistKm: 0.3},
			{ToStopID: "platform2", WalkSec: sameStationTransferSec},
		},
	}
	if got := transferGraphAtSpeed(graph, 4.8); got["a"][0].WalkSec != graph["a"][0].WalkSec {
		t.Fatalf("normal pace should reuse the shared graph")
	}
	slow := transferGraphAtSpeed(graph, 3.0)
	if want := walkDurationSeconds(0.3, 3.0) + footTransferBufferSec; slow["a"][0].WalkSec != want {
		t.Fatalf("expected %ds at 3 km/h, got %d", want, slow["a"][0].WalkSec)
	}
	if slow["a"][1].WalkSec != sameStationTransferSec {
		t.Fatalf("same-station interchange should keep its fixed time, got %d", slow["a"][1].WalkSec)
	}
	if graph["a"][0].WalkSec == slow["a"][0].WalkSec {
		t.Fatalf("the shared graph must not be modified")
	}
}

func TestNoJourneyErrorMentionsTheModeFilter(t *testing.T) {
	err := noOnlyRouteJourneyError(JourneyRequest{AllowedRouteTypes: []int{2, 4, 1000, 1200}})
	if !strings.Contains(err.Error(), "only the chosen transport") {
		t.Fatalf("expected the modes in the message, got %q", err.Error())
	}
}
