package gtfs

import (
	"maps"
	"math"
	"sort"
)

// --- Indexed timetable ---------------------------------------------------------
//
// The RAPTOR scans used to walk every trip of the day on every round (~15k
// trips x ~30 stops for Auckland) just to find the handful that call at a stop
// reached in the previous round. plannerDay adds a per-stop index of the trips
// calling there, sorted by scheduled time, so a round only looks at trips that
// serve a marked stop inside the time window that can still matter.

// plannerDay is one service day's stop times, indexed for the planner. The
// index (byDep/byArr) is built once per static day and shared by every
// realtime overlay of it: realtime changes times but never the stops a trip
// calls at or their order. Read-only once built.
type plannerDay struct {
	// trips is keyed by trip ID, as the leg-building helpers expect.
	trips map[string][]tripStopTime
	// tripIDs and tripList give each trip an ordinal for the index.
	tripIDs  []string
	tripList [][]tripStopTime
	// byDep holds each stop's calls sorted by scheduled departure, byArr by
	// scheduled arrival.
	byDep map[string][]tripRef
	byArr map[string][]tripRef
	// Realtime spread across the day's trips: how far any actual time sits
	// after (maxLateSec) or before (maxEarlySec) its schedule. Index windows,
	// which are in scheduled time, widen by these.
	maxLateSec  int
	maxEarlySec int
}

type tripRef struct {
	trip  int32 // ordinal in tripList
	pos   int32 // index in that trip's stop times
	sched int32 // scheduled departure (byDep) or arrival (byArr), service-day seconds
}

// newPlannerDay indexes a day's (static) stop times.
func newPlannerDay(trips map[string][]tripStopTime) *plannerDay {
	ids := make([]string, 0, len(trips))
	for id := range trips {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	d := &plannerDay{
		trips:    trips,
		tripIDs:  ids,
		tripList: make([][]tripStopTime, len(ids)),
		byDep:    make(map[string][]tripRef),
		byArr:    make(map[string][]tripRef),
	}
	for i, id := range ids {
		times := trips[id]
		d.tripList[i] = times
		for pos, st := range times {
			d.byDep[st.StopID] = append(d.byDep[st.StopID], tripRef{trip: int32(i), pos: int32(pos), sched: int32(st.ScheduledDepartureSec)})
			d.byArr[st.StopID] = append(d.byArr[st.StopID], tripRef{trip: int32(i), pos: int32(pos), sched: int32(st.ScheduledArrivalSec)})
		}
	}
	for stop, refs := range d.byDep {
		sort.Slice(refs, func(a, b int) bool { return refs[a].sched < refs[b].sched })
		d.byDep[stop] = refs
	}
	for stop, refs := range d.byArr {
		sort.Slice(refs, func(a, b int) bool { return refs[a].sched < refs[b].sched })
		d.byArr[stop] = refs
	}
	d.measureSpread()
	return d
}

// withTrips returns d's index over `trips`, a realtime overlay of d's own
// (same trip IDs, same stops in the same order, only times changed).
func (d *plannerDay) withTrips(trips map[string][]tripStopTime) *plannerDay {
	out := &plannerDay{
		trips:    trips,
		tripIDs:  d.tripIDs,
		tripList: make([][]tripStopTime, len(d.tripIDs)),
		byDep:    d.byDep,
		byArr:    d.byArr,
	}
	for i, id := range d.tripIDs {
		out.tripList[i] = trips[id]
	}
	out.measureSpread()
	return out
}

func (d *plannerDay) measureSpread() {
	late, early := 0, 0
	for _, times := range d.tripList {
		for _, st := range times {
			if dd := st.DepartureSec - st.ScheduledDepartureSec; dd > late {
				late = dd
			} else if -dd > early {
				early = -dd
			}
			if da := st.ArrivalSec - st.ScheduledArrivalSec; da > late {
				late = da
			} else if -da > early {
				early = -da
			}
		}
	}
	d.maxLateSec, d.maxEarlySec = late, early
}

// firstRef returns the index of the first ref with sched >= t.
func firstRef(refs []tripRef, t int) int {
	return sort.Search(len(refs), func(i int) bool { return int(refs[i].sched) >= t })
}

// --- Scan parameters ----------------------------------------------------------

// scanRules are a request's boarding rules, shared by every scan of one plan.
type scanRules struct {
	banned, only map[string]bool
	// originGapSec is the time needed at the first stop, transferGap the time
	// needed to board at a stop after arriving by another ride (or on foot).
	originGapSec int
	transferGap  func(stopID string) int
	// accessible restricts boarding and alighting to stops and trips the feed
	// doesn't mark as inaccessible (stopOK says which stops are fine).
	accessible bool
	stopOK     func(stopID string) bool
}

func (r *scanRules) gapAt(stopID string) int {
	if r.transferGap == nil {
		return minDirectTransferSeconds
	}
	return r.transferGap(stopID)
}

func (r *scanRules) tripAllowed(times []tripStopTime) bool {
	return len(times) > 0 && routeAllowed(times[0].RouteID, r.only, r.banned)
}

func (r *scanRules) canBoard(st tripStopTime) bool {
	if !st.TripUsable || !st.Boardable {
		return false
	}
	return !r.accessible || (st.Access != wheelchairNo && (r.stopOK == nil || r.stopOK(st.StopID)))
}

func (r *scanRules) canAlight(st tripStopTime) bool {
	if !st.TripUsable || !st.Alightable {
		return false
	}
	return !r.accessible || (st.Access != wheelchairNo && (r.stopOK == nil || r.stopOK(st.StopID)))
}

const (
	wheelchairUnknown int8 = 0
	wheelchairYes     int8 = 1
	wheelchairNo      int8 = 2
)

// scanHorizonSec is how far past the requested time a scan looks before any
// destination is reached - well beyond any city journey, and it keeps the
// first rounds from reading the rest of the day's timetable.
const scanHorizonSec = 6 * 3600

// targetSlackSec is how much later than the best arrival so far a scan still
// explores, so the 2nd/3rd best destinations (another stop, a shorter walk, a
// direct ride) are still found. Beyond it nothing can rank.
const targetSlackSec = 40 * 60

const infSec = math.MaxInt32

// --- Depart-after scan ----------------------------------------------------------
//
// Labels are kept per round (index 0: the walk from the origin; index r+1:
// RAPTOR round r, i.e. r changes). A stop's best arrival over all rounds
// prunes, but a journey is read back round by round: the ride into a stop in
// round r boarded at a stop labelled in round r-1. With a single label per
// stop, a later round could overwrite a stop half-way along an earlier
// journey, and reading it back spliced in extra rides past the change budget.

// departScan holds RAPTOR labels for depart-after searches over one day. The
// labels survive between runs, which is what the range scan relies on: a run
// for an earlier departure only has to find what improves on a later one.
type departScan struct {
	day     *plannerDay
	graph   map[string][]stopTransfer
	rules   *scanRules
	targets []StopWithDistance
	walk    float64

	arrival   map[string]int // best arrival over all rounds
	lastRound map[string]int // label index the best arrival came from
	preds     []map[string]stopPredecessor
}

func newDepartScan(day *plannerDay, stopMap map[string]Stop, graph map[string][]stopTransfer, rules *scanRules, targets []StopWithDistance, walkSpeedKmph float64) *departScan {
	s := &departScan{
		day:       day,
		graph:     graph,
		rules:     rules,
		targets:   targets,
		walk:      walkSpeedKmph,
		arrival:   make(map[string]int, len(stopMap)),
		lastRound: make(map[string]int, 256),
	}
	for id := range stopMap {
		s.arrival[id] = infSec
	}
	return s
}

func (s *departScan) predsAt(idx int) map[string]stopPredecessor {
	for len(s.preds) <= idx {
		s.preds = append(s.preds, make(map[string]stopPredecessor, 128))
	}
	return s.preds[idx]
}

// best returns the predecessor behind a stop's best arrival.
func (s *departScan) best(stopID string) (stopPredecessor, bool) {
	idx, ok := s.lastRound[stopID]
	if !ok {
		return stopPredecessor{}, false
	}
	p, ok := s.preds[idx][stopID]
	return p, ok
}

// seed walks from the origin at departSec to each nearby stop and returns the
// stops that improved.
func (s *departScan) seed(origins []StopWithDistance, departSec int) map[string]bool {
	updated := make(map[string]bool, len(origins))
	seeds := s.predsAt(0)
	for _, c := range origins {
		t := departSec + walkDurationSeconds(c.Distance, s.walk)
		if t < s.arrival[c.Stop.StopId] {
			s.arrival[c.Stop.StopId] = t
			seeds[c.Stop.StopId] = stopPredecessor{DepartSec: departSec, ArriveSec: t, Mode: "walk-origin"}
			s.lastRound[c.Stop.StopId] = 0
			updated[c.Stop.StopId] = true
		}
	}
	return updated
}

// bestTarget is the earliest arrival at the destination itself (stop arrival
// plus the walk from it) over all labels so far.
func (s *departScan) bestTarget() int {
	best := infSec
	for _, t := range s.targets {
		// Only stops a ride reaches count (see reachedByRide).
		if p, _ := s.best(t.Stop.StopId); p.Mode == "walk-transfer" {
			continue
		}
		if a := s.arrival[t.Stop.StopId]; a < infSec {
			if v := a + walkDurationSeconds(t.Distance, s.walk); v < best {
				best = v
			}
		}
	}
	return best
}

// chain reads back the journey to `dest` from its label at index idx, as a
// predecessor map holding just that journey's stops (what buildJourneyLegs
// walks). nil if it can't be read back.
func (s *departScan) chain(dest string, idx int) map[string]stopPredecessor {
	out := make(map[string]stopPredecessor, 8)
	cur := dest
	for guard := 0; guard < 64 && idx >= 0 && idx < len(s.preds); guard++ {
		p, ok := s.preds[idx][cur]
		if !ok {
			return nil
		}
		if _, loop := out[cur]; loop {
			return nil
		}
		out[cur] = p
		switch p.Mode {
		case "walk-origin":
			return out
		case "transit":
			idx--
		}
		cur = p.FromStopID
	}
	return nil
}

// bestChain is chain from the stop's best label.
func (s *departScan) bestChain(dest string) map[string]stopPredecessor {
	idx, ok := s.lastRound[dest]
	if !ok {
		return nil
	}
	return s.chain(dest, idx)
}

// run does the RAPTOR rounds from the `updated` stops. When wantDirect is set
// it returns the arrivals after the first round (before any change of
// vehicle): exactly what a separate direct-only scan would have found; read
// those journeys back with chain(stop, 1).
func (s *departScan) run(updated map[string]bool, departSec, maxTransfers int, slack int, wantDirect bool) (directArrival map[string]int) {
	horizon := departSec + scanHorizonSec
	for round := 0; round <= maxTransfers && len(updated) > 0; round++ {
		bound := horizon
		if bt := s.bestTarget(); bt < infSec && bt+slack < bound {
			bound = bt + slack
		}
		preds := s.predsAt(round + 1)

		// Which trips can be boarded, and from where: the earliest catchable
		// position on each trip serving a stop marked last round.
		boardAt := make(map[int32]int32)
		for stopID := range updated {
			gap := s.rules.originGapSec
			if round > 0 {
				gap = s.rules.gapAt(stopID)
			}
			ready := s.arrival[stopID] + gap
			if ready > bound {
				continue
			}
			refs := s.day.byDep[stopID]
			hi := bound + s.day.maxEarlySec
			for i := firstRef(refs, ready-s.day.maxLateSec); i < len(refs) && int(refs[i].sched) <= hi; i++ {
				ref := refs[i]
				times := s.day.tripList[ref.trip]
				st := times[ref.pos]
				if st.DepartureSec < ready || st.DepartureSec > bound || !s.rules.canBoard(st) {
					continue
				}
				if cur, ok := boardAt[ref.trip]; ok && cur <= ref.pos {
					continue
				}
				if !s.rules.tripAllowed(times) {
					continue
				}
				boardAt[ref.trip] = ref.pos
			}
		}

		nextUpdated := make(map[string]bool)
		for _, trip := range sortedTrips(boardAt) {
			pos := boardAt[trip]
			times := s.day.tripList[trip]
			board := times[pos]
			for _, st := range times[pos+1:] {
				if st.ArrivalSec > bound {
					break
				}
				if !s.rules.canAlight(st) || st.ArrivalSec < board.DepartureSec || st.ArrivalSec >= s.arrival[st.StopID] {
					continue
				}
				s.arrival[st.StopID] = st.ArrivalSec
				preds[st.StopID] = stopPredecessor{
					FromStopID:         board.StopID,
					TripID:             st.TripID,
					RouteID:            st.RouteID,
					DepartSec:          board.DepartureSec,
					ArriveSec:          st.ArrivalSec,
					ScheduledDepartSec: board.ScheduledDepartureSec,
					ScheduledArriveSec: st.ScheduledArrivalSec,
					RealtimeStatus:     st.RealtimeStatus,
					TripUsable:         st.TripUsable,
					Mode:               "transit",
				}
				nextUpdated[st.StopID] = true
			}
		}

		if round == 0 && wantDirect {
			directArrival = maps.Clone(s.arrival)
		}

		if round < maxTransfers && len(nextUpdated) > 0 {
			relaxFootTransfers(sortedStops(nextUpdated), s.graph, s.arrival, preds, nextUpdated)
		}
		for id := range nextUpdated {
			s.lastRound[id] = round + 1
		}
		updated = nextUpdated
	}
	return directArrival
}

// departTimesFrom lists the times, within [fromSec, toSec], at which leaving
// the origin catches a different first ride from one of the nearby stops -
// the departures a range scan iterates over. Latest first, at most `limit`
// (the earliest ones are kept).
func (s *departScan) departTimesFrom(origins []StopWithDistance, fromSec, toSec, limit int) []int {
	seen := map[int]bool{}
	for _, c := range origins {
		lead := walkDurationSeconds(c.Distance, s.walk) + s.rules.originGapSec
		refs := s.day.byDep[c.Stop.StopId]
		hi := toSec + lead + s.day.maxEarlySec
		for i := firstRef(refs, fromSec+lead-s.day.maxLateSec); i < len(refs) && int(refs[i].sched) <= hi; i++ {
			times := s.day.tripList[refs[i].trip]
			st := times[refs[i].pos]
			leave := st.DepartureSec - lead
			if leave < fromSec || leave > toSec || !s.rules.canBoard(st) || !s.rules.tripAllowed(times) {
				continue
			}
			seen[leave] = true
		}
	}
	out := make([]int, 0, len(seen))
	for t := range seen {
		out = append(out, t)
	}
	sort.Ints(out)
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	// Latest first: each earlier run then only explores what it improves.
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// rangeScan runs one depart-after search per departure time in [fromSec,
// toSec], latest first, keeping labels between runs (rRAPTOR). After each run
// that reaches the destination earlier than any later departure did, onHit is
// called with that departure time while the labels still describe its journey.
// The hits are the window's Pareto-best journeys: leave later or arrive
// earlier than any other.
func (s *departScan) rangeScan(origins []StopWithDistance, fromSec, toSec, maxTransfers, maxRuns int, onHit func(departSec int)) {
	best := infSec
	for _, t := range s.departTimesFrom(origins, fromSec, toSec, maxRuns) {
		updated := s.seed(origins, t)
		// Nothing can help unless it beats the arrival a later departure
		// already managed, so prune at that (no slack).
		s.run(updated, t, maxTransfers, 0, false)
		if bt := s.bestTarget(); bt < best {
			best = bt
			onHit(t)
		}
	}
}

// --- Arrive-by scan ---------------------------------------------------------------

// arriveScan is departScan's mirror: latest departure from each stop that
// still reaches the destination by the requested time. Index 0 holds the walk
// to the destination; r+1 is round r.
type arriveScan struct {
	day     *plannerDay
	graph   map[string][]stopTransfer
	rules   *scanRules
	origins []StopWithDistance
	walk    float64

	latest    map[string]int
	lastRound map[string]int
	succs     []map[string]stopSuccessor
}

func newArriveScan(day *plannerDay, stopMap map[string]Stop, graph map[string][]stopTransfer, rules *scanRules, origins []StopWithDistance, walkSpeedKmph float64) *arriveScan {
	s := &arriveScan{
		day:       day,
		graph:     graph,
		rules:     rules,
		origins:   origins,
		walk:      walkSpeedKmph,
		latest:    make(map[string]int, len(stopMap)),
		lastRound: make(map[string]int, 256),
	}
	for id := range stopMap {
		s.latest[id] = -1
	}
	return s
}

func (s *arriveScan) succsAt(idx int) map[string]stopSuccessor {
	for len(s.succs) <= idx {
		s.succs = append(s.succs, make(map[string]stopSuccessor, 128))
	}
	return s.succs[idx]
}

func (s *arriveScan) best(stopID string) (stopSuccessor, bool) {
	idx, ok := s.lastRound[stopID]
	if !ok {
		return stopSuccessor{}, false
	}
	n, ok := s.succs[idx][stopID]
	return n, ok
}

// seed walks back from the destination to each nearby stop.
func (s *arriveScan) seed(dests []StopWithDistance, arriveSec int) map[string]bool {
	updated := map[string]bool{}
	seeds := s.succsAt(0)
	for _, c := range dests {
		t := arriveSec - walkDurationSeconds(c.Distance, s.walk)
		if t < 0 || t <= s.latest[c.Stop.StopId] {
			continue
		}
		s.latest[c.Stop.StopId] = t
		seeds[c.Stop.StopId] = stopSuccessor{DepartSec: t, ArriveSec: arriveSec, Mode: "walk-destination"}
		s.lastRound[c.Stop.StopId] = 0
		updated[c.Stop.StopId] = true
	}
	return updated
}

// bestOrigin is the latest time the rider can leave the origin over all labels.
func (s *arriveScan) bestOrigin() int {
	best := -1
	for _, o := range s.origins {
		if n, _ := s.best(o.Stop.StopId); n.Mode == "walk-transfer" {
			continue
		}
		if l := s.latest[o.Stop.StopId]; l >= 0 {
			if v := l - walkDurationSeconds(o.Distance, s.walk); v > best {
				best = v
			}
		}
	}
	return best
}

// chain reads back the journey from `origin` at label index idx.
func (s *arriveScan) chain(origin string, idx int) map[string]stopSuccessor {
	out := make(map[string]stopSuccessor, 8)
	cur := origin
	for guard := 0; guard < 64 && idx >= 0 && idx < len(s.succs); guard++ {
		n, ok := s.succs[idx][cur]
		if !ok {
			return nil
		}
		if _, loop := out[cur]; loop {
			return nil
		}
		out[cur] = n
		switch n.Mode {
		case "walk-destination":
			return out
		case "transit":
			idx--
		}
		cur = n.ToStopID
	}
	return nil
}

func (s *arriveScan) bestChain(origin string) map[string]stopSuccessor {
	idx, ok := s.lastRound[origin]
	if !ok {
		return nil
	}
	return s.chain(origin, idx)
}

// run is departScan.run backwards; directLatest is the first round's labels
// (read back with chain(stop, 1)).
func (s *arriveScan) run(updated map[string]bool, arriveSec, maxTransfers int, wantDirect bool) (directLatest map[string]int) {
	horizon := arriveSec - scanHorizonSec
	for round := 0; round <= maxTransfers && len(updated) > 0; round++ {
		bound := horizon
		if bo := s.bestOrigin(); bo >= 0 && bo-targetSlackSec > bound {
			bound = bo - targetSlackSec
		}
		succs := s.succsAt(round + 1)

		// The latest catchable alighting position on each trip serving a
		// stop marked last round.
		alightAt := make(map[int32]int32)
		for stopID := range updated {
			gap := 0
			if round > 0 {
				gap = s.rules.gapAt(stopID)
			}
			deadline := s.latest[stopID] - gap
			if deadline < bound {
				continue
			}
			refs := s.day.byArr[stopID]
			lo := bound - s.day.maxLateSec
			for i := firstRef(refs, deadline+s.day.maxEarlySec+1) - 1; i >= 0 && int(refs[i].sched) >= lo; i-- {
				ref := refs[i]
				times := s.day.tripList[ref.trip]
				st := times[ref.pos]
				if st.ArrivalSec > deadline || st.ArrivalSec < bound || !s.rules.canAlight(st) {
					continue
				}
				if cur, ok := alightAt[ref.trip]; ok && cur >= ref.pos {
					continue
				}
				if !s.rules.tripAllowed(times) {
					continue
				}
				alightAt[ref.trip] = ref.pos
			}
		}

		nextUpdated := make(map[string]bool)
		for _, trip := range sortedTrips(alightAt) {
			pos := alightAt[trip]
			times := s.day.tripList[trip]
			alight := times[pos]
			for i := pos - 1; i >= 0; i-- {
				st := times[i]
				if st.DepartureSec < bound {
					break
				}
				if !s.rules.canBoard(st) || st.DepartureSec > alight.ArrivalSec || st.DepartureSec <= s.latest[st.StopID] {
					continue
				}
				s.latest[st.StopID] = st.DepartureSec
				succs[st.StopID] = stopSuccessor{
					ToStopID:           alight.StopID,
					TripID:             st.TripID,
					RouteID:            st.RouteID,
					DepartSec:          st.DepartureSec,
					ArriveSec:          alight.ArrivalSec,
					ScheduledDepartSec: st.ScheduledDepartureSec,
					ScheduledArriveSec: alight.ScheduledArrivalSec,
					RealtimeStatus:     st.RealtimeStatus,
					TripUsable:         st.TripUsable,
					Mode:               "transit",
				}
				nextUpdated[st.StopID] = true
			}
		}

		if round == 0 && wantDirect {
			directLatest = maps.Clone(s.latest)
		}

		if round < maxTransfers && len(nextUpdated) > 0 {
			relaxFootTransfersArriveAt(sortedStops(nextUpdated), s.graph, s.latest, succs, nextUpdated)
		}
		for id := range nextUpdated {
			s.lastRound[id] = round + 1
		}
		updated = nextUpdated
	}
	return directLatest
}

// sortedTrips / sortedStops give map keys in a fixed order, so ties between
// equally good labels always resolve the same way and a search returns the
// same plans every time.
func sortedTrips(m map[int32]int32) []int32 {
	out := make([]int32, 0, len(m))
	for t := range m {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func sortedStops(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for id := range m {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}
