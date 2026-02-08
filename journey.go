package gtfs

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

type JourneyRequest struct {
	StartLat        float64
	StartLon        float64
	EndLat          float64
	EndLon          float64
	DepartAt        time.Time
	MaxWalkKm       float64
	WalkSpeedKmph   float64
	MaxTransfers    int
	MaxNearbyStops  int
	MinResults      int
	MaxResults      int
	IncludeChildren bool
	OsrmURL         string
}

type JourneyLeg struct {
	Mode           string
	FromStop       *Stop
	ToStop         *Stop
	TripID         string
	RouteID        string
	Route          *Route
	DepartureTime  time.Time
	ArrivalTime    time.Time
	Duration       time.Duration
	DistanceKm     float64
	StopSequenceID int
}

type JourneyPlan struct {
	StartLat      float64
	StartLon      float64
	EndLat        float64
	EndLon        float64
	DepartureTime time.Time
	ArrivalTime   time.Time
	TotalDuration time.Duration
	Transfers     int
	TransferStops []Stop
	Legs          []JourneyLeg
	RouteGeoJSON  map[string]interface{}
	ID            string
}

type tripStopTime struct {
	StopID        string
	StopSequence  int
	ArrivalSec    int
	DepartureSec  int
	RouteID       string
	TripID        string
	ArrivalTime   string
	DepartureTime string
}

type stopPredecessor struct {
	FromStopID string
	TripID     string
	RouteID    string
	DepartSec  int
	ArriveSec  int
	Mode       string
}

type journeyCandidate struct {
	Stop       StopWithDistance
	ArrivalSec int
}

// PlanJourneyRaptor computes a basic journey plan between two coordinates using a RAPTOR-style scan.
func (v Database) PlanJourneyRaptor(req JourneyRequest) (*[]JourneyPlan, error) {
	plans, err := v.PlanJourneysRaptor(req)
	if err != nil {
		return nil, err
	}
	if len(plans) == 0 {
		return nil, errors.New("no journey found between the given coordinates")
	}
	return &plans, nil
}

// PlanJourneysRaptor computes multiple journey options between two coordinates using a RAPTOR-style scan.
func (v Database) PlanJourneysRaptor(req JourneyRequest) ([]JourneyPlan, error) {
	if req.MaxWalkKm <= 0 {
		req.MaxWalkKm = 1.0
	}
	if req.WalkSpeedKmph <= 0 {
		req.WalkSpeedKmph = 4.8
	}
	if req.MaxTransfers <= 0 {
		req.MaxTransfers = 2
	}
	if req.MaxNearbyStops <= 0 {
		req.MaxNearbyStops = 50
	}
	if req.MinResults < 0 {
		req.MinResults = 0
	}
	if req.MaxResults <= 0 {
		req.MaxResults = 1
	}
	if req.MinResults > 0 && req.MaxResults < req.MinResults {
		req.MaxResults = req.MinResults
	}
	if req.DepartAt.IsZero() {
		return nil, errors.New("depart time required")
	}

	departAt := req.DepartAt.In(v.timeZone)
	dayStart := time.Date(departAt.Year(), departAt.Month(), departAt.Day(), 0, 0, 0, 0, v.timeZone)
	departSec := int(departAt.Sub(dayStart).Seconds())

	stops, err := v.GetStops(req.IncludeChildren)
	if err != nil {
		return nil, err
	}
	stopMap := make(map[string]Stop, len(stops))
	for _, stop := range stops {
		stopMap[stop.StopId] = stop
	}

	nearbyStartStops := filterNearbyStops(stops, req.StartLat, req.StartLon, req.MaxWalkKm, req.MaxNearbyStops)
	nearbyEndStops := filterNearbyStops(stops, req.EndLat, req.EndLon, req.MaxWalkKm, req.MaxNearbyStops)

	if len(nearbyStartStops) == 0 || len(nearbyEndStops) == 0 {
		return nil, errors.New("no nearby stops found for start or end")
	}

	trips, err := v.loadTripStopTimes(dayStart)
	if err != nil {
		return nil, err
	}
	routes, err := v.GetRoutes()
	if err != nil {
		return nil, err
	}
	routeMap := make(map[string]Route, len(routes))
	for _, route := range routes {
		routeMap[route.RouteId] = route
	}

	arrival := make(map[string]int, len(stopMap))
	predecessor := make(map[string]stopPredecessor, len(stopMap))
	updated := make(map[string]bool, len(stopMap))
	const inf = math.MaxInt32
	for stopID := range stopMap {
		arrival[stopID] = inf
	}

	for _, candidate := range nearbyStartStops {
		walkSeconds := walkDurationSeconds(candidate.Distance, req.WalkSpeedKmph)
		arrivalTime := departSec + walkSeconds
		if arrivalTime < arrival[candidate.Stop.StopId] {
			arrival[candidate.Stop.StopId] = arrivalTime
			predecessor[candidate.Stop.StopId] = stopPredecessor{
				FromStopID: "",
				TripID:     "",
				RouteID:    "",
				DepartSec:  departSec,
				ArriveSec:  arrivalTime,
				Mode:       "walk-origin",
			}
			updated[candidate.Stop.StopId] = true
		}
	}

	for round := 0; round <= req.MaxTransfers; round++ {
		nextUpdated := make(map[string]bool)
		for _, tripTimes := range trips {
			boarded := false
			boardStopID := ""
			boardDepartSec := 0
			for _, stopTime := range tripTimes {
				if !boarded {
					if updated[stopTime.StopID] && arrival[stopTime.StopID] <= stopTime.DepartureSec {
						boarded = true
						boardStopID = stopTime.StopID
						boardDepartSec = stopTime.DepartureSec
					}
					continue
				}

				if stopTime.ArrivalSec < arrival[stopTime.StopID] {
					arrival[stopTime.StopID] = stopTime.ArrivalSec
					predecessor[stopTime.StopID] = stopPredecessor{
						FromStopID: boardStopID,
						TripID:     stopTime.TripID,
						RouteID:    stopTime.RouteID,
						DepartSec:  boardDepartSec,
						ArriveSec:  stopTime.ArrivalSec,
						Mode:       "transit",
					}
					nextUpdated[stopTime.StopID] = true
				}
			}
		}

		if len(nextUpdated) == 0 {
			break
		}
		updated = nextUpdated
	}

	bestCandidates := selectBestDestinations(nearbyEndStops, arrival, departSec, req.WalkSpeedKmph, req.MaxResults)
	if len(bestCandidates) == 0 {
		return nil, errors.New("no journey found between the given coordinates")
	}

	var plans []JourneyPlan
	for _, candidate := range bestCandidates {
		legs, transfers, transferStops := buildJourneyLegs(candidate.Stop, candidate.ArrivalSec, predecessor, stopMap, routeMap, departAt, dayStart, req.WalkSpeedKmph, req.StartLat, req.StartLon)
		if len(legs) == 0 {
			continue
		}
		arrivalTime := dayStart.Add(time.Duration(candidate.ArrivalSec) * time.Second)
		plan := JourneyPlan{
			StartLat:      req.StartLat,
			StartLon:      req.StartLon,
			EndLat:        req.EndLat,
			EndLon:        req.EndLon,
			DepartureTime: departAt,
			ArrivalTime:   arrivalTime,
			TotalDuration: arrivalTime.Sub(departAt),
			Transfers:     transfers,
			TransferStops: transferStops,
			Legs:          legs,
			RouteGeoJSON:  buildJourneyGeoJSON(v, req, legs),
			ID:            uuid.NewString(),
		}
		plans = append(plans, plan)
	}

	if len(plans) == 0 {
		return nil, errors.New("no journey legs available")
	}

	return plans, nil
}

func (v Database) loadTripStopTimes(dayStart time.Time) (map[string][]tripStopTime, error) {
	weekday := strings.ToLower(dayStart.Weekday().String()) // "monday", "tuesday", etc.

	query := fmt.Sprintf(`
	WITH active_services AS (
		SELECT service_id
		FROM calendar
		WHERE start_date <= ?
		  AND end_date >= ?
		  AND %s = 1

		UNION ALL

		SELECT service_id
		FROM calendar_dates
		WHERE date = ?
		  AND exception_type = 1
	),
	removed_services AS (
		SELECT service_id
		FROM calendar_dates
		WHERE date = ?
		  AND exception_type = 2
	),
	adjusted_services AS (
		SELECT DISTINCT service_id
		FROM active_services
		WHERE service_id NOT IN (
			SELECT service_id FROM removed_services
		)
	)
	SELECT
		st.trip_id,
		t.route_id,
		st.stop_id,
		st.stop_sequence,
		st.arrival_time,
		st.departure_time
	FROM stop_times st
	JOIN trips t ON st.trip_id = t.trip_id
	JOIN adjusted_services a ON t.service_id = a.service_id
	WHERE (st.drop_off_type = 0 OR st.drop_off_type IS NULL)
	  AND (st.pickup_type = 0 OR st.pickup_type IS NULL)
	ORDER BY st.trip_id, st.stop_sequence
	`, weekday)

	day := dayStart.Format("20060102")

	rows, err := v.db.Query(query, day, day, day, day)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	trips := make(map[string][]tripStopTime)

	for rows.Next() {
		var tripID, routeID, stopID, arrivalTime, departureTime string
		var sequence int

		if err := rows.Scan(
			&tripID,
			&routeID,
			&stopID,
			&sequence,
			&arrivalTime,
			&departureTime,
		); err != nil {
			return nil, err
		}

		arrivalSec, err := parseTimeToSeconds(arrivalTime)
		if err != nil {
			arrivalSec = 0
		}

		departureSec, err := parseTimeToSeconds(departureTime)
		if err != nil {
			departureSec = arrivalSec
		}

		trips[tripID] = append(trips[tripID], tripStopTime{
			StopID:        stopID,
			StopSequence:  sequence,
			ArrivalSec:    arrivalSec,
			DepartureSec:  departureSec,
			RouteID:       routeID,
			TripID:        tripID,
			ArrivalTime:   arrivalTime,
			DepartureTime: departureTime,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	if len(trips) == 0 {
		return nil, errors.New("no trip times found for active services")
	}

	return trips, nil
}

func filterNearbyStops(stops []Stop, lat, lon, maxDistanceKm float64, maxStops int) []StopWithDistance {
	var stopDistances []StopWithDistance
	for _, stop := range stops {
		distance := calculateDistance(lat, lon, stop.StopLat, stop.StopLon)
		if distance <= maxDistanceKm {
			stopDistances = append(stopDistances, StopWithDistance{Stop: stop, Distance: distance})
		}
	}

	sort.Slice(stopDistances, func(i, j int) bool {
		return stopDistances[i].Distance < stopDistances[j].Distance
	})

	if maxStops < len(stopDistances) {
		stopDistances = stopDistances[:maxStops]
	}

	return stopDistances
}

func walkDurationSeconds(distanceKm, speedKmph float64) int {
	if speedKmph <= 0 {
		speedKmph = 4.8
	}
	return int(math.Round((distanceKm / speedKmph) * 3600))
}

func parseTimeToSeconds(timeStr string) (int, error) {
	if strings.TrimSpace(timeStr) == "" {
		return 0, errors.New("empty time")
	}
	parts := strings.Split(timeStr, ":")
	if len(parts) != 3 {
		return 0, fmt.Errorf("invalid time format: %s", timeStr)
	}

	hours, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, err
	}
	minutes, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, err
	}
	seconds, err := strconv.Atoi(parts[2])
	if err != nil {
		return 0, err
	}

	return hours*3600 + minutes*60 + seconds, nil
}

func selectBestDestinations(candidates []StopWithDistance, arrival map[string]int, departSec int, walkSpeedKmph float64, maxResults int) []journeyCandidate {
	var results []journeyCandidate
	for _, candidate := range candidates {
		arrivalAtStop := arrival[candidate.Stop.StopId]
		if arrivalAtStop == math.MaxInt32 {
			continue
		}
		walkSeconds := walkDurationSeconds(candidate.Distance, walkSpeedKmph)
		totalArrival := arrivalAtStop + walkSeconds
		if totalArrival >= departSec {
			results = append(results, journeyCandidate{
				Stop:       candidate,
				ArrivalSec: totalArrival,
			})
		}
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].ArrivalSec < results[j].ArrivalSec
	})
	if maxResults > 0 && len(results) > maxResults {
		results = results[:maxResults]
	}
	return results
}

func buildJourneyLegs(endStop StopWithDistance, endArrivalSec int, predecessor map[string]stopPredecessor, stopMap map[string]Stop, routeMap map[string]Route, departAt time.Time, dayStart time.Time, walkSpeedKmph float64, startLat, startLon float64) ([]JourneyLeg, int, []Stop) {
	var legs []JourneyLeg
	transfers := 0
	var transferStops []Stop
	currentStopID := endStop.Stop.StopId
	lastTripID := ""
	var lastStop *Stop

	if currentStopID == "" {
		return nil, 0, nil
	}

	walkToDestination := JourneyLeg{
		Mode:          "walk",
		FromStop:      &endStop.Stop,
		ToStop:        nil,
		TripID:        "",
		RouteID:       "",
		DepartureTime: dayStart.Add(time.Duration(endArrivalSec-walkDurationSeconds(endStop.Distance, walkSpeedKmph)) * time.Second),
		ArrivalTime:   dayStart.Add(time.Duration(endArrivalSec) * time.Second),
		Duration:      time.Duration(walkDurationSeconds(endStop.Distance, walkSpeedKmph)) * time.Second,
		DistanceKm:    endStop.Distance,
	}
	legs = append(legs, walkToDestination)
	lastStop = &endStop.Stop

	for currentStopID != "" {
		pred, ok := predecessor[currentStopID]
		if !ok {
			break
		}
		if pred.Mode == "walk-origin" {
			stop := stopMap[currentStopID]
			walkLeg := JourneyLeg{
				Mode:          "walk",
				FromStop:      nil,
				ToStop:        &stop,
				TripID:        "",
				RouteID:       "",
				DepartureTime: departAt,
				ArrivalTime:   dayStart.Add(time.Duration(pred.ArriveSec) * time.Second),
				Duration:      time.Duration(pred.ArriveSec-int(departAt.Sub(dayStart).Seconds())) * time.Second,
				DistanceKm:    calculateDistance(startLat, startLon, stop.StopLat, stop.StopLon),
			}
			legs = append(legs, walkLeg)
			lastStop = &stop
			break
		}

		fromStop := stopMap[pred.FromStopID]
		toStop := stopMap[currentStopID]
		var routePtr *Route
		if route, ok := routeMap[pred.RouteID]; ok {
			routeCopy := route
			routePtr = &routeCopy
		}
		leg := JourneyLeg{
			Mode:          "transit",
			FromStop:      &fromStop,
			ToStop:        &toStop,
			TripID:        pred.TripID,
			RouteID:       pred.RouteID,
			Route:         routePtr,
			DepartureTime: dayStart.Add(time.Duration(pred.DepartSec) * time.Second),
			ArrivalTime:   dayStart.Add(time.Duration(pred.ArriveSec) * time.Second),
			Duration:      time.Duration(pred.ArriveSec-pred.DepartSec) * time.Second,
		}
		if lastTripID != "" && lastTripID != pred.TripID {
			transfers++
			if lastStop != nil {
				transferStops = append(transferStops, *lastStop)
			}
		}
		lastTripID = pred.TripID
		legs = append(legs, leg)
		lastStop = &fromStop
		currentStopID = pred.FromStopID
	}

	reverseLegs(legs)

	return legs, transfers, transferStops
}

func reverseLegs(legs []JourneyLeg) {
	for i, j := 0, len(legs)-1; i < j; i, j = i+1, j-1 {
		legs[i], legs[j] = legs[j], legs[i]
	}
}

func buildJourneyGeoJSON(db Database, req JourneyRequest, legs []JourneyLeg) map[string]interface{} {
	var features []map[string]interface{}
	for _, leg := range legs {
		switch leg.Mode {
		case "transit":
			if leg.TripID == "" {
				continue
			}
			shape, err := db.GetShapeByTripID(leg.TripID)
			if err != nil {
				continue
			}
			shape = shapeSegmentForLeg(db, shape, leg)
			if len(shape.Coordinates) == 0 {
				continue
			}
			geoJSON, err := shape.ToGeoJSON()
			if err != nil {
				continue
			}
			if props, ok := geoJSON["properties"].(map[string]interface{}); ok {
				props["mode"] = "transit"
				if leg.RouteID != "" {
					props["route_id"] = leg.RouteID
				}
				if leg.TripID != "" {
					props["trip_id"] = leg.TripID
				}
			}
			features = append(features, geoJSON)
		case "walk":
			startLat, startLon, endLat, endLon, ok := walkLegCoordinates(req, leg)
			if !ok {
				continue
			}
			feature := buildWalkFeature(req.OsrmURL, startLat, startLon, endLat, endLon)
			if feature == nil {
				continue
			}
			features = append(features, feature)
		}
	}

	return map[string]interface{}{
		"type":     "FeatureCollection",
		"features": features,
	}
}

func walkLegCoordinates(req JourneyRequest, leg JourneyLeg) (float64, float64, float64, float64, bool) {
	if leg.FromStop == nil && leg.ToStop != nil {
		return req.StartLat, req.StartLon, leg.ToStop.StopLat, leg.ToStop.StopLon, true
	}
	if leg.FromStop != nil && leg.ToStop == nil {
		return leg.FromStop.StopLat, leg.FromStop.StopLon, req.EndLat, req.EndLon, true
	}
	if leg.FromStop != nil && leg.ToStop != nil {
		return leg.FromStop.StopLat, leg.FromStop.StopLon, leg.ToStop.StopLat, leg.ToStop.StopLon, true
	}
	return 0, 0, 0, 0, false
}

func shapeSegmentForLeg(db Database, shape Shape, leg JourneyLeg) Shape {
	if leg.FromStop == nil || leg.ToStop == nil || len(shape.Coordinates) == 0 {
		return shape
	}

	if leg.TripID != "" && shapeHasDistance(shape.Coordinates) {
		fromDist, okFrom := stopShapeDistance(db, leg.TripID, leg.FromStop.StopId)
		toDist, okTo := stopShapeDistance(db, leg.TripID, leg.ToStop.StopId)
		if okFrom && okTo {
			minDist := math.Min(fromDist, toDist)
			maxDist := math.Max(fromDist, toDist)
			segment := segmentShapeByDistance(shape.Coordinates, minDist, maxDist)
			if len(segment) > 1 {
				shape.Coordinates = segment
				return shape
			}
		}
	}

	startIdx := nearestShapeIndex(shape.Coordinates, leg.FromStop.StopLat, leg.FromStop.StopLon)
	endIdx := nearestShapeIndex(shape.Coordinates, leg.ToStop.StopLat, leg.ToStop.StopLon)
	if startIdx == -1 || endIdx == -1 {
		return shape
	}
	if startIdx > endIdx {
		startIdx, endIdx = endIdx, startIdx
	}
	segment := make([]Point, endIdx-startIdx+1)
	copy(segment, shape.Coordinates[startIdx:endIdx+1])
	shape.Coordinates = segment
	return shape
}

func stopShapeDistance(db Database, tripID, stopID string) (float64, bool) {
	query := `
		SELECT shape_dist_traveled
		FROM stop_times
		WHERE trip_id = ? AND stop_id = ?
		LIMIT 1
	`
	var dist float64
	err := db.db.QueryRow(query, tripID, stopID).Scan(&dist)
	if err != nil {
		return 0, false
	}
	if dist <= 0 {
		return 0, false
	}
	return dist, true
}

func shapeHasDistance(points []Point) bool {
	for _, point := range points {
		if point.DistTraveled > 0 {
			return true
		}
	}
	return false
}

func segmentShapeByDistance(points []Point, minDist, maxDist float64) []Point {
	var segment []Point
	for _, point := range points {
		if point.DistTraveled >= minDist && point.DistTraveled <= maxDist {
			segment = append(segment, point)
		}
	}
	return segment
}

func nearestShapeIndex(points []Point, lat, lon float64) int {
	if len(points) == 0 {
		return -1
	}
	bestIdx := 0
	bestDist := math.MaxFloat64
	for i, point := range points {
		dist := calculateDistance(lat, lon, point.Lat, point.Lon)
		if dist < bestDist {
			bestDist = dist
			bestIdx = i
		}
	}
	return bestIdx
}

func buildWalkFeature(osrmURL string, startLat, startLon, endLat, endLon float64) map[string]interface{} {
	if osrmURL != "" {
		feature, ok := osrmWalkFeature(osrmURL, startLat, startLon, endLat, endLon)
		if ok {
			return feature
		}
	}
	return straightLineWalkFeature(startLat, startLon, endLat, endLon)
}

func osrmWalkFeature(osrmURL string, startLat, startLon, endLat, endLon float64) (map[string]interface{}, bool) {
	normalized := strings.TrimRight(osrmURL, "/")
	endpoint := fmt.Sprintf("%s/route/v1/foot/%f,%f;%f,%f", normalized, startLon, startLat, endLon, endLat)
	query := url.Values{}
	query.Set("overview", "full")
	query.Set("geometries", "geojson")
	endpoint = endpoint + "?" + query.Encode()

	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, false
	}
	client := http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, false
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, false
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, false
	}

	var payload struct {
		Code   string `json:"code"`
		Routes []struct {
			Geometry struct {
				Type        string      `json:"type"`
				Coordinates interface{} `json:"coordinates"`
			} `json:"geometry"`
			Distance float64 `json:"distance"`
			Duration float64 `json:"duration"`
		} `json:"routes"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, false
	}
	if payload.Code != "Ok" || len(payload.Routes) == 0 {
		return nil, false
	}

	route := payload.Routes[0]
	return map[string]interface{}{
		"type": "Feature",
		"geometry": map[string]interface{}{
			"type":        route.Geometry.Type,
			"coordinates": route.Geometry.Coordinates,
		},
		"properties": map[string]interface{}{
			"mode":             "walk",
			"distance_meters":  route.Distance,
			"duration_seconds": route.Duration,
		},
	}, true
}

func straightLineWalkFeature(startLat, startLon, endLat, endLon float64) map[string]interface{} {
	return map[string]interface{}{
		"type": "Feature",
		"geometry": map[string]interface{}{
			"type": "LineString",
			"coordinates": [][]float64{
				{startLon, startLat},
				{endLon, endLat},
			},
		},
		"properties": map[string]interface{}{
			"mode": "walk",
		},
	}
}
