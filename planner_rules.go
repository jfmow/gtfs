package gtfs

import (
	"strconv"
	"sync"
	"time"
)

// --- Change times per interchange ---------------------------------------------
//
// One minute is enough to step off a bus and onto another at the same pole, but
// not to get from a train to a ferry gangway or across Britomart. Each stop gets
// its own minimum time to board after arriving by another ride:
//
//   - bus stops: minDirectTransferSeconds (1 min)
//   - rail platforms: 2 min (find the platform, doors close early)
//   - ferry wharves: 3 min (boarding closes before departure)
//   - platforms of a big station (4+ rail platforms): 3 min
//   - never less than the feed's own transfers.txt min_transfer_time for a
//     change at that stop.
//
// A change between two different stops also walks the transfer-graph edge, and
// that edge is never shorter than transfers.txt says for that pair.

const (
	railBoardGapSec      = 120
	ferryBoardGapSec     = 180
	bigStationBoardGap   = 180
	bigStationPlatforms  = 4
	interchangeGapsTTL   = 30 * time.Minute
	accessibleExtraGap   = 120 // extra time per change for the "easier access" option
	accessibleMaxWalkKm  = 0.5
	accessibleMaxHopKm   = 0.1 // longest walk between two stops at a change
	accessibleWalkCapKmh = 3.6
)

type interchangeMemo struct {
	mu      sync.Mutex
	key     string
	builtAt time.Time
	gaps    map[string]int
}

var interchangeMemos sync.Map // database name -> *interchangeMemo

// interchangeGaps returns each stop's minimum boarding time after another
// ride (stops not listed use minDirectTransferSeconds). Memoised per database.
func (v Database) interchangeGaps(stopMap map[string]Stop, includeChildren bool) map[string]int {
	key := v.name + "|" + strconv.FormatBool(includeChildren)
	mi, _ := interchangeMemos.LoadOrStore(v.name, &interchangeMemo{})
	m := mi.(*interchangeMemo)
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.gaps != nil && m.key == key && time.Since(m.builtAt) < interchangeGapsTTL {
		return m.gaps
	}
	self, _ := v.feedTransferTimes()
	m.gaps = buildInterchangeGaps(stopMap, v.stopRouteTypes(), self)
	m.key, m.builtAt = key, time.Now()
	return m.gaps
}

// stopRouteTypes maps each stop to the GTFS route types calling there.
func (v Database) stopRouteTypes() map[string]map[int]bool {
	out := map[string]map[int]bool{}
	rows, err := v.db.Query(`
		SELECT DISTINCT st.stop_id, r.route_type
		FROM stop_times st
		JOIN trips t ON t.trip_id = st.trip_id
		JOIN routes r ON r.route_id = t.route_id`)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var stopID string
		var routeType int
		if rows.Scan(&stopID, &routeType) != nil {
			continue
		}
		if out[stopID] == nil {
			out[stopID] = map[int]bool{}
		}
		out[stopID][routeType] = true
	}
	return out
}

// feedTransferTimes reads transfers.txt's minimum change times (transfer_type
// 2). A row naming specific trips still says how long that physical change
// takes, so the longest time seen for a stop pair is kept.
func (v Database) feedTransferTimes() (self map[string]int, pairs map[[2]string]int) {
	self, pairs = map[string]int{}, map[[2]string]int{}
	rows, err := v.db.Query(`
		SELECT from_stop_id, to_stop_id, COALESCE(min_transfer_time, 0)
		FROM transfers
		WHERE transfer_type = 2 AND from_stop_id != '' AND to_stop_id != ''`)
	if err != nil {
		return self, pairs
	}
	defer rows.Close()
	for rows.Next() {
		var from, to string
		var sec int
		if rows.Scan(&from, &to, &sec) != nil || sec <= 0 {
			continue
		}
		if from == to {
			self[from] = max(self[from], sec)
		} else {
			k := [2]string{from, to}
			pairs[k] = max(pairs[k], sec)
		}
	}
	return self, pairs
}

func isRailRouteType(t int) bool { return t == 0 || t == 1 || t == 2 || t == 12 }

func buildInterchangeGaps(stopMap map[string]Stop, routeTypes map[string]map[int]bool, feedSelf map[string]int) map[string]int {
	// Rail platforms per station, to find the big ones.
	platforms := map[string]int{}
	for id, s := range stopMap {
		for t := range routeTypes[id] {
			if isRailRouteType(t) {
				platforms[stationKey(s)]++
				break
			}
		}
	}
	gaps := make(map[string]int)
	for id, s := range stopMap {
		gap := minDirectTransferSeconds
		types := routeTypes[id]
		for t := range types {
			switch {
			case t == 4:
				gap = max(gap, ferryBoardGapSec)
			case isRailRouteType(t):
				gap = max(gap, railBoardGapSec)
				if platforms[stationKey(s)] >= bigStationPlatforms {
					gap = max(gap, bigStationBoardGap)
				}
			}
		}
		if len(types) == 0 && isRareStopType(s.StopType) {
			gap = max(gap, railBoardGapSec)
		}
		gap = max(gap, feedSelf[id])
		if gap != minDirectTransferSeconds {
			gaps[id] = gap
		}
	}
	return gaps
}

// withFeedPairTimes returns graph with each edge at least as long as the
// feed's transfers.txt time for that pair (a copy only when something changes).
func withFeedPairTimes(graph map[string][]stopTransfer, pairs map[[2]string]int) map[string][]stopTransfer {
	if len(pairs) == 0 {
		return graph
	}
	var out map[string][]stopTransfer
	for from, edges := range graph {
		for i, tr := range edges {
			min, ok := pairs[[2]string{from, tr.ToStopID}]
			if !ok || tr.WalkSec >= min {
				continue
			}
			if out == nil {
				out = make(map[string][]stopTransfer, len(graph))
				for k, e := range graph {
					out[k] = e
				}
			}
			if &out[from][0] == &graph[from][0] {
				out[from] = append([]stopTransfer(nil), edges...)
			}
			out[from][i].WalkSec = min
			out[from][i].MinSec = min
		}
	}
	if out == nil {
		return graph
	}
	return out
}

// accessibleGraph keeps only the changes that need no real walk: another
// platform of the same station, or a stop within accessibleMaxHopKm.
func accessibleGraph(graph map[string][]stopTransfer) map[string][]stopTransfer {
	out := make(map[string][]stopTransfer, len(graph))
	for from, edges := range graph {
		var kept []stopTransfer
		for _, tr := range edges {
			if tr.DistKm <= accessibleMaxHopKm {
				kept = append(kept, tr)
			}
		}
		if len(kept) > 0 {
			out[from] = kept
		}
	}
	return out
}

// accessibleStop reports whether the feed doesn't mark a stop (or its parent
// station, when the stop itself says nothing) as not wheelchair accessible.
func accessibleStop(stopMap map[string]Stop, stopID string) bool {
	s, ok := stopMap[stopID]
	if !ok {
		return true
	}
	if s.WheelChairBoarding == int(wheelchairNo) {
		return false
	}
	if s.WheelChairBoarding == int(wheelchairUnknown) && s.ParentStation != "" {
		if p, ok := stopMap[s.ParentStation]; ok && p.WheelChairBoarding == int(wheelchairNo) {
			return false
		}
	}
	return true
}

// newScanRules builds the boarding rules for a request.
func (v Database) newScanRules(req JourneyRequest, stopMap map[string]Stop, onlyRoutes map[string]bool) *scanRules {
	gaps := v.interchangeGaps(stopMap, req.IncludeChildren)
	extra := req.MinTransferSec
	if req.Accessible {
		extra += accessibleExtraGap
	}
	r := &scanRules{
		only:         onlyRoutes,
		originGapSec: originWalkBufferSeconds,
		accessible:   req.Accessible,
		transferGap: func(stopID string) int {
			if g, ok := gaps[stopID]; ok {
				return g + extra
			}
			return minDirectTransferSeconds + extra
		},
	}
	if req.Accessible {
		r.stopOK = func(stopID string) bool { return accessibleStop(stopMap, stopID) }
	}
	return r
}

// requestGraph is the transfer graph a request walks: re-timed for the
// rider's pace, cut down for easier access.
func (v Database) requestGraph(req JourneyRequest, stopMap map[string]Stop) map[string][]stopTransfer {
	g := transferGraphAtSpeed(v.stopTransferGraph(stopMap, req.IncludeChildren), req.WalkSpeedKmph)
	if req.Accessible {
		g = accessibleGraph(g)
	}
	return g
}
