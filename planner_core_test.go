package gtfs

import (
	"testing"
	"time"
)

// st builds a stop time where arrival = departure = sec (scheduled the same).
func st(trip, route, stop string, sec int) tripStopTime {
	return tripStopTime{TripID: trip, RouteID: route, StopID: stop, ArrivalSec: sec, DepartureSec: sec,
		ScheduledArrivalSec: sec, ScheduledDepartureSec: sec, TripUsable: true, Boardable: true, Alightable: true}
}

func lineStops() map[string]Stop {
	return map[string]Stop{
		"o": {StopId: "o", StopName: "Origin", StopLat: -36.850, StopLon: 174.760},
		"x": {StopId: "x", StopName: "Change", StopLat: -36.860, StopLon: 174.770},
		"d": {StopId: "d", StopName: "Dest", StopLat: -36.870, StopLon: 174.780},
	}
}

func TestDepartScanReadsBackWithinTransferBudget(t *testing.T) {
	// o -> x -> d by two buses (one change), or o -> d direct but later. With
	// a budget of 0 changes only the direct bus may be read back, even though
	// the best label at d (after round 1) is the two-bus one.
	stops := lineStops()
	day := newPlannerDay(map[string][]tripStopTime{
		"a":      {st("a", "A", "o", 8*3600), st("a", "A", "x", 8*3600+300)},
		"b":      {st("b", "B", "x", 8*3600+600), st("b", "B", "d", 8*3600+900)},
		"direct": {st("direct", "D", "o", 8*3600+600), st("direct", "D", "d", 8*3600+1500)},
	})
	rules := &scanRules{transferGap: func(string) int { return 60 }}
	near := []StopWithDistance{{Stop: stops["o"]}}
	scan := newDepartScan(day, stops, map[string][]stopTransfer{}, rules, []StopWithDistance{{Stop: stops["d"]}}, 4)
	direct := scan.run(scan.seed(near, 8*3600-60), 8*3600-60, 1, targetSlackSec, true)

	if got := scan.arrival["d"]; got != 8*3600+900 {
		t.Fatalf("best arrival at d = %d, want the two-bus 08:15", got)
	}
	if c := scan.bestChain("d"); c == nil || c["d"].TripID != "b" || c["x"].TripID != "a" {
		t.Fatalf("best chain = %+v, want a then b", c)
	}
	if direct["d"] != 8*3600+1500 {
		t.Fatalf("direct arrival at d = %d, want 08:25", direct["d"])
	}
	if c := scan.chain("d", 1); c == nil || c["d"].TripID != "direct" {
		t.Fatalf("direct chain = %+v, want the direct bus", c)
	}
}

func TestDepartScanUsesStopInterchangeGap(t *testing.T) {
	// Arrive at x at 08:05; the connection leaves 08:07. A 1-min stop allows
	// it; a 3-min station gap (ferry wharf) pushes the rider to 08:20.
	stops := lineStops()
	day := newPlannerDay(map[string][]tripStopTime{
		"a":     {st("a", "A", "o", 8*3600), st("a", "A", "x", 8*3600+300)},
		"tight": {st("tight", "B", "x", 8*3600+420), st("tight", "B", "d", 8*3600+900)},
		"later": {st("later", "B", "x", 8*3600+1200), st("later", "B", "d", 8*3600+1700)},
	})
	near := []StopWithDistance{{Stop: stops["o"]}}
	for _, tc := range []struct {
		gap  int
		want string
	}{{60, "tight"}, {180, "later"}} {
		rules := &scanRules{transferGap: func(string) int { return tc.gap }}
		scan := newDepartScan(day, stops, map[string][]stopTransfer{}, rules, nil, 4)
		scan.run(scan.seed(near, 8*3600-60), 8*3600-60, 2, targetSlackSec, false)
		if p, _ := scan.best("d"); p.TripID != tc.want {
			t.Fatalf("gap %d: rode %q, want %q", tc.gap, p.TripID, tc.want)
		}
	}
}

func TestBuildInterchangeGaps(t *testing.T) {
	stops := map[string]Stop{
		"bus":   {StopId: "bus", StopLat: -36.8, StopLon: 174.7},
		"wharf": {StopId: "wharf", StopLat: -36.81, StopLon: 174.71},
		"small": {StopId: "small", ParentStation: "S", StopLat: -36.82, StopLon: 174.72},
		"big1":  {StopId: "big1", ParentStation: "B"}, "big2": {StopId: "big2", ParentStation: "B"},
		"big3": {StopId: "big3", ParentStation: "B"}, "big4": {StopId: "big4", ParentStation: "B"},
		"feed": {StopId: "feed", StopLat: -36.83, StopLon: 174.73},
	}
	types := map[string]map[int]bool{
		"bus": {3: true}, "wharf": {4: true}, "small": {2: true},
		"big1": {2: true}, "big2": {2: true}, "big3": {2: true}, "big4": {2: true}, "feed": {3: true},
	}
	gaps := buildInterchangeGaps(stops, types, map[string]int{"feed": 240})
	want := map[string]int{"bus": 0, "wharf": ferryBoardGapSec, "small": railBoardGapSec, "big1": bigStationBoardGap, "feed": 240}
	for id, w := range want {
		if gaps[id] != w {
			t.Errorf("gap[%s] = %d, want %d", id, gaps[id], w)
		}
	}
}

func TestAccessibleRulesSkipInaccessibleTripsAndStops(t *testing.T) {
	stops := lineStops()
	stops["x"] = Stop{StopId: "x", WheelChairBoarding: int(wheelchairNo)}
	rules := &scanRules{accessible: true, stopOK: func(id string) bool { return accessibleStop(stops, id) }}
	ok := st("t", "R", "o", 0)
	if !rules.canBoard(ok) {
		t.Fatal("an unknown-access trip at an unmarked stop should be boardable")
	}
	noTrip := ok
	noTrip.Access = wheelchairNo
	if rules.canBoard(noTrip) {
		t.Fatal("a trip marked not accessible must not be boardable")
	}
	if rules.canAlight(st("t", "R", "x", 0)) {
		t.Fatal("a stop marked not accessible must not be used")
	}
	g := accessibleGraph(map[string][]stopTransfer{"o": {{ToStopID: "x", DistKm: 0}, {ToStopID: "d", DistKm: 0.3}}})
	if len(g["o"]) != 1 || g["o"][0].ToStopID != "x" {
		t.Fatalf("accessible graph kept %+v, want only the same-station change", g["o"])
	}
}

func TestRangeScanFindsEachLaterDeparture(t *testing.T) {
	// A direct bus every 15 min: the range scan should hit each one.
	stops := lineStops()
	trips := map[string][]tripStopTime{}
	for i, dep := range []int{8 * 3600, 8*3600 + 900, 8*3600 + 1800} {
		id := string(rune('a' + i))
		trips[id] = []tripStopTime{st(id, "R", "o", dep), st(id, "R", "d", dep+600)}
	}
	day := newPlannerDay(trips)
	rules := &scanRules{transferGap: func(string) int { return 60 }}
	scan := newDepartScan(day, stops, map[string][]stopTransfer{}, rules, []StopWithDistance{{Stop: stops["d"]}}, 4)
	var hits []string
	scan.rangeScan([]StopWithDistance{{Stop: stops["o"]}}, 7*3600, 9*3600, 2, 40, func(int) {
		hits = append(hits, scan.bestChain("d")["d"].TripID)
	})
	if len(hits) != 3 || hits[0] != "c" || hits[2] != "a" {
		t.Fatalf("range scan hits = %v, want c, b, a (latest first)", hits)
	}
}

func TestLeaveLaterOptionTakesLaterFirstRide(t *testing.T) {
	// Plan: bus a 08:00 o->x, wait, bus b 08:30 x->d. Bus a2 at 08:15 still
	// reaches x at 08:20, in time for b - so leave 15 min later.
	stops := lineStops()
	day := newPlannerDay(map[string][]tripStopTime{
		"a":  {st("a", "A", "o", 8*3600), st("a", "A", "x", 8*3600+300)},
		"a2": {st("a2", "A", "o", 8*3600+900), st("a2", "A", "x", 8*3600+1200)},
		"a3": {st("a3", "A", "o", 8*3600+1500), st("a3", "A", "x", 8*3600+1780)}, // too late
		"b":  {st("b", "B", "x", 8*3600+1800), st("b", "B", "d", 8*3600+2400)},
	})
	dayStart := time.Date(2026, 11, 26, 0, 0, 0, 0, time.UTC)
	at := func(sec int) time.Time { return dayStart.Add(time.Duration(sec) * time.Second) }
	o, x, d := stops["o"], stops["x"], stops["d"]
	plan := JourneyPlan{
		DepartureTime: at(8 * 3600), ArrivalTime: at(8*3600 + 2400),
		Legs: []JourneyLeg{
			{Mode: "transit", TripID: "a", RouteID: "A", FromStop: &o, ToStop: &x, DepartureTime: at(8 * 3600), ArrivalTime: at(8*3600 + 300)},
			{Mode: "transit", TripID: "b", RouteID: "B", FromStop: &x, ToStop: &d, DepartureTime: at(8*3600 + 1800), ArrivalTime: at(8*3600 + 2400)},
		},
	}
	rules := &scanRules{transferGap: func(string) int { return 60 }}
	later := leaveLaterOption(plan, day, rules, dayStart, nil, stops, 4)
	if later == nil {
		t.Fatal("expected a leave-later option")
	}
	if later.Legs[0].TripID != "a2" || !later.DepartureTime.Equal(at(8*3600+900)) || !later.ArrivalTime.Equal(plan.ArrivalTime) {
		t.Fatalf("leave later = %s at %s, arriving %s", later.Legs[0].TripID, later.DepartureTime, later.ArrivalTime)
	}
}

func TestPruneDominatedPlans(t *testing.T) {
	base := time.Date(2026, 11, 26, 8, 0, 0, 0, time.UTC)
	p := func(id string, depMin, arrMin, transfers int) JourneyPlan {
		return JourneyPlan{ID: id, DepartureTime: base.Add(time.Duration(depMin) * time.Minute), ArrivalTime: base.Add(time.Duration(arrMin) * time.Minute), Transfers: transfers}
	}
	out := pruneDominatedPlans([]JourneyPlan{p("good", 10, 50, 1), p("worse", 5, 55, 1), p("direct", 0, 60, 0)})
	if len(out) != 2 || out[0].ID != "good" || out[1].ID != "direct" {
		t.Fatalf("kept %v, want good and direct", []string{out[0].ID, out[1].ID})
	}
}

func TestOvernightTripsShiftOntoTheNextDay(t *testing.T) {
	// Friday's 23:50 bus: stops at 23:50, 24:10 and 24:30 (Friday's clock).
	prev := map[string][]tripStopTime{
		"late":  {st("late", "R", "a", 23*3600+50*60), st("late", "R", "b", 24*3600+10*60), st("late", "R", "c", 24*3600+30*60)},
		"early": {st("early", "R", "a", 20*3600), st("early", "R", "b", 21*3600)},
	}
	out := overnightTrips(prev, -24*3600)
	got, ok := out["late"+overnightSuffix]
	if !ok || len(out) != 1 {
		t.Fatalf("overnight trips = %v, want only the late bus", out)
	}
	if len(got) != 2 || got[0].StopID != "b" || got[0].DepartureSec != 10*60 || got[1].ScheduledArrivalSec != 30*60 {
		t.Fatalf("late bus after midnight = %+v, want b at 00:10 and c at 00:30", got)
	}
	if got[0].TripID != "late" {
		t.Fatalf("TripID = %q, want the real trip id", got[0].TripID)
	}
}

func TestApplyRealtimeUsesThePreviousServiceDayForOvernightTrips(t *testing.T) {
	static := map[string][]tripStopTime{
		"t" + overnightSuffix: {st("t", "R", "b", 600), st("t", "R", "c", 1800)},
	}
	adj := map[string]realtimeTripAdjustment{"t": {hasRealtime: true, tripDelay: 120, startDate: "20261127", hasVehicle: true}}
	out := applyRealtimeToTripStopTimes(static, adj, "20261128", "20261128", 900)
	if got := out["t"+overnightSuffix][0].DepartureSec; got != 720 {
		t.Fatalf("overnight trip departure = %d, want the 2-min delay applied (720)", got)
	}
	// An update for the same trip on the new day doesn't touch it.
	adj["t"] = realtimeTripAdjustment{hasRealtime: true, tripDelay: 300, startDate: "20261128", hasVehicle: true}
	out = applyRealtimeToTripStopTimes(static, adj, "20261128", "20261128", 900)
	if got := out["t"+overnightSuffix][0].DepartureSec; got != 600 {
		t.Fatalf("overnight trip departure = %d, want it unchanged (600)", got)
	}
}

func TestDropLongWaitPlansKeepsTheOnlyOption(t *testing.T) {
	base := time.Date(2026, 11, 28, 0, 0, 0, 0, time.UTC)
	m := func(min int) time.Time { return base.Add(time.Duration(min) * time.Minute) }
	wait := JourneyPlan{ID: "wait", Legs: []JourneyLeg{
		{Mode: "transit", DepartureTime: m(30), ArrivalTime: m(60)},
		{Mode: "transit", DepartureTime: m(330), ArrivalTime: m(360)},
	}}
	direct := JourneyPlan{ID: "direct", Legs: []JourneyLeg{{Mode: "transit", DepartureTime: m(320), ArrivalTime: m(400)}}}
	if out := dropLongWaitPlans([]JourneyPlan{wait, direct}); len(out) != 1 || out[0].ID != "direct" {
		t.Fatalf("kept %v, want only direct", out)
	}
	if out := dropLongWaitPlans([]JourneyPlan{wait}); len(out) != 1 {
		t.Fatal("the only option must be kept")
	}
}
