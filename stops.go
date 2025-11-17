package gtfs

import (
	"database/sql"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

type Stop struct {
	LocationType       int     `json:"location_type"`
	ParentStation      string  `json:"parent_station"`
	StopCode           string  `json:"stop_code"`
	StopId             string  `json:"stop_id"`
	StopLat            float64 `json:"stop_lat"`
	StopLon            float64 `json:"stop_lon"`
	StopName           string  `json:"stop_name"`
	StopHeadsign       string  `json:"stop_headsign"`
	WheelChairBoarding int     `json:"wheelchair_boarding"`
	PlatformNumber     string  `json:"platform_number"`
	StopType           string  `json:"stop_type"`
	Sequence           int     `json:"stop_sequence"`
	IsChildStop        bool    `json:"is_child_stop"`
}

type StopSearch struct {
	Name       string `json:"name"`
	TypeOfStop string `json:"type_of_stop"`
}

type StopId string

/*
Get all the stored stops
*/
func (v Database) GetStops(includeChildStops bool) ([]Stop, error) {
	db := v.db
	query := `
		SELECT
			stop_id,
			stop_code,
			stop_name,
			stop_lat,
			stop_lon,
			location_type,
			parent_station,
			platform_code,
			wheelchair_boarding
		FROM
			stops
	`
	if !includeChildStops {
		// Add filtering to exclude child stops
		query += ` WHERE (location_type == 1 OR parent_station = '')`
	}

	rows, err := db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close() // Ensure the rows are closed after usage

	var stops []Stop

	for rows.Next() {
		var stop Stop
		err := rows.Scan(
			&stop.StopId,
			&stop.StopCode,
			&stop.StopName,
			&stop.StopLat,
			&stop.StopLon,
			&stop.LocationType,
			&stop.ParentStation,
			&stop.PlatformNumber,
			&stop.WheelChairBoarding,
		)
		if err != nil {
			return nil, err
		}
		stop.StopType = typeOfStop(stop.StopName)
		stops = append(stops, stop)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	if len(stops) == 0 {
		return nil, errors.New("no stops found")
	}

	return stops, nil
}

/*
Get all the stored stops mapped by their id
*/
func (v Database) GetStopsMap(includeChildStops bool) (map[string]Stop, error) {
	db := v.db
	query := `
		SELECT
			stop_id,
			stop_code,
			stop_name,
			stop_lat,
			stop_lon,
			location_type,
			parent_station,
			platform_code,
			wheelchair_boarding
		FROM
			stops
	`
	if !includeChildStops {
		// Add filtering to exclude child stops
		query += ` WHERE (location_type == 1 OR parent_station = '')`
	}

	rows, err := db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close() // Ensure the rows are closed after usage

	var stops map[string]Stop = make(map[string]Stop)

	for rows.Next() {
		var stop Stop
		err := rows.Scan(
			&stop.StopId,
			&stop.StopCode,
			&stop.StopName,
			&stop.StopLat,
			&stop.StopLon,
			&stop.LocationType,
			&stop.ParentStation,
			&stop.PlatformNumber,
			&stop.WheelChairBoarding,
		)
		if err != nil {
			return nil, err
		}
		if stop.LocationType == 0 && stop.ParentStation != "" {
			stop.IsChildStop = true
		}
		stop.StopType = typeOfStop(stop.StopName)
		stops[stop.StopId] = stop
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	if len(stops) == 0 {
		return nil, errors.New("no stops found")
	}

	return stops, nil
}

/*
Get the child stops of a parent stop
*/
func (v Database) GetChildStopsByParentStopID(stopID string) ([]Stop, error) {
	db := v.db

	// Query to fetch parent stop and its children
	query := `
		SELECT
			stop_id,
			stop_code,
			stop_name,
			stop_lat,
			stop_lon,
			location_type,
			parent_station,
			platform_code,
			wheelchair_boarding
		FROM
			stops
		WHERE
			(stop_id = ? AND parent_station = '' AND location_type == 0) OR parent_station = ?
	`

	rows, err := db.Query(query, stopID, stopID)
	if err != nil {
		return nil, err
	}
	defer rows.Close() // Ensure the rows are closed after usage

	var stops Stops

	for rows.Next() {
		var stop Stop
		err := rows.Scan(
			&stop.StopId,
			&stop.StopCode,
			&stop.StopName,
			&stop.StopLat,
			&stop.StopLon,
			&stop.LocationType,
			&stop.ParentStation,
			&stop.PlatformNumber,
			&stop.WheelChairBoarding,
		)
		if err != nil {
			return nil, err
		}
		stop.StopType = typeOfStop(stop.StopName)
		stops = append(stops, stop)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	if len(stops) == 0 {
		return nil, errors.New("no child stops found")
	}

	return stops, nil
}

/*
Get the stops for a trip

returned int is the lowest sequence returned, if -1, then its unknown
*/
func (v Database) GetStopsForTripID(tripID string) ([]Stop, int, error) {
	db := v.db

	query := `
		SELECT
			s.stop_id,
			s.stop_code,
			s.stop_name,
			s.stop_lat,
			s.stop_lon,
			s.location_type,
			s.parent_station,
			s.platform_code,
			s.wheelchair_boarding,
			st.stop_sequence
		FROM
			stop_times st
		JOIN
			stops s ON st.stop_id = s.stop_id
		WHERE
			st.trip_id = ?
		AND (st.drop_off_type = 0 OR st.drop_off_type IS NULL)
  		AND (st.pickup_type = 1 OR st.pickup_type = 0 OR st.pickup_type IS NULL)
		ORDER BY
			st.stop_sequence
	`

	// Execute the query
	rows, err := db.Query(query, tripID)
	if err != nil {
		return nil, -1, err
	}
	defer rows.Close()

	// Slice to hold the stops
	var stops Stops
	var lowestSequence = -1

	// Iterate over the rows
	for rows.Next() {
		var stop Stop
		// Scan the row data into the Stop struct
		err := rows.Scan(
			&stop.StopId,
			&stop.StopCode,
			&stop.StopName,
			&stop.StopLat,
			&stop.StopLon,
			&stop.LocationType,
			&stop.ParentStation,
			&stop.PlatformNumber,
			&stop.WheelChairBoarding,
			&stop.Sequence,
		)
		if err != nil {
			return nil, -1, err
		}
		stop.StopType = typeOfStop(stop.StopName)
		// Append each stop to the slice
		stops = append(stops, stop)
		if stop.Sequence < lowestSequence || lowestSequence == -1 {
			lowestSequence = stop.Sequence
		}
	}

	// Check for any error encountered during iteration
	if err = rows.Err(); err != nil {
		return nil, -1, err
	}

	// If no stops were found, return a custom error
	if len(stops) == 0 {
		return nil, -1, errors.New("no stops found for the given trip ID")
	}

	return stops, lowestSequence, nil
}

// GetStopTimesForTripID returns the stop times (arrival and departure) for all stops in a given trip.
func (v Database) GetStopTimesForTripID(tripID string) (map[string]struct {
	Stop
	ArrivalTime   string
	DepartureTime string
}, error) {
	db := v.db

	query := `
		SELECT
			s.stop_id,
			s.stop_code,
			s.stop_name,
			s.stop_lat,
			s.stop_lon,
			s.location_type,
			s.parent_station,
			s.platform_code,
			s.wheelchair_boarding,
			st.stop_sequence,
			st.arrival_time,
			st.departure_time
		FROM
			stop_times st
		JOIN
			stops s ON st.stop_id = s.stop_id
		WHERE
			st.trip_id = ?
		AND (st.drop_off_type = 0 OR st.drop_off_type IS NULL)
  		AND (st.pickup_type = 1 OR st.pickup_type = 0 OR st.pickup_type IS NULL)
		ORDER BY
			st.stop_sequence
	`

	rows, err := db.Query(query, tripID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results map[string]struct {
		Stop
		ArrivalTime   string
		DepartureTime string
	} = make(map[string]struct {
		Stop
		ArrivalTime   string
		DepartureTime string
	})

	for rows.Next() {
		var stop Stop
		var arrival, departure string
		err := rows.Scan(
			&stop.StopId,
			&stop.StopCode,
			&stop.StopName,
			&stop.StopLat,
			&stop.StopLon,
			&stop.LocationType,
			&stop.ParentStation,
			&stop.PlatformNumber,
			&stop.WheelChairBoarding,
			&stop.Sequence,
			&arrival,
			&departure,
		)
		if err != nil {
			return nil, err
		}
		stop.StopType = typeOfStop(stop.StopName)
		results[stop.StopId] = struct {
			Stop
			ArrivalTime   string
			DepartureTime string
		}{
			Stop:          stop,
			ArrivalTime:   arrival,
			DepartureTime: departure,
		}
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	if len(results) == 0 {
		return nil, errors.New("no stop times found for the given trip ID")
	}

	return results, nil
}

/*
Returns the child stops for a trip
*/
func (v Database) GetStopsForTrips(days int) (map[string][]Stop, error) {
	db := v.db

	// Calculate the date range for filtering
	startDate := time.Now().In(v.timeZone).Format("20060102")
	endDate := time.Now().In(v.timeZone).AddDate(0, 0, days).Format("20060102")

	query := `
	WITH active_services AS (
		SELECT DISTINCT service_id
		FROM calendar
		WHERE start_date <= ? AND end_date >= ?
		UNION ALL
		SELECT service_id
		FROM calendar_dates
		WHERE date = ? AND exception_type = 1
	),
	removed_services AS (
		SELECT service_id
		FROM calendar_dates
		WHERE date = ? AND exception_type = 2
	),
	adjusted_services AS (
		SELECT service_id
		FROM active_services
		WHERE service_id NOT IN (SELECT service_id FROM removed_services)
	)
	SELECT DISTINCT
		st.trip_id,
		s.stop_id,
		s.stop_code,
		s.stop_name,
		st.stop_headsign,
		s.stop_lat,
		s.stop_lon,
		s.location_type,
		s.parent_station,
		s.platform_code,
		s.wheelchair_boarding,
		st.stop_sequence
	FROM
		stop_times st
	JOIN
		stops s ON st.stop_id = s.stop_id
	JOIN
		trips t ON st.trip_id = t.trip_id
	JOIN
		adjusted_services a ON t.service_id = a.service_id
	WHERE (st.drop_off_type = 0 OR st.drop_off_type IS NULL)
  	AND (st.pickup_type = 1 OR st.pickup_type = 0 OR st.pickup_type IS NULL)
	ORDER BY
		st.trip_id,
		st.stop_sequence
`

	rows, err := db.Query(query, startDate, endDate, startDate, endDate, startDate, endDate)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	trips := make(map[string][]Stop)

	for rows.Next() {
		var tripID string
		var stop Stop

		err := rows.Scan(
			&tripID,
			&stop.StopId,
			&stop.StopCode,
			&stop.StopName,
			&stop.StopHeadsign,
			&stop.StopLat,
			&stop.StopLon,
			&stop.LocationType,
			&stop.ParentStation,
			&stop.PlatformNumber,
			&stop.WheelChairBoarding,
			&stop.Sequence,
		)
		if err != nil {
			return nil, err
		}

		stop.StopType = typeOfStop(stop.StopName)
		trips[tripID] = append(trips[tripID], stop)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	if len(trips) == 0 {
		return nil, errors.New("no stops found for any trips")
	}

	return trips, nil
}

/*
Get a stop by its name or its stop code
*/
func (v Database) GetStopByNameOrCode(nameOrCode string) (*Stop, error) {
	db := v.db

	// Format current date as YYYYMMDD
	now := time.Now().In(v.timeZone).Format("20060102")

	// Check if columns exist
	hasStartDate, hasEndDate := false, false
	rows, err := db.Query(`PRAGMA table_info(stops)`)
	if err != nil {
		return nil, fmt.Errorf("failed to get table info: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dfltValue interface{}
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dfltValue, &pk); err != nil {
			return nil, err
		}
		if name == "start_date" {
			hasStartDate = true
		}
		if name == "end_date" {
			hasEndDate = true
		}
	}

	// Build query conditionally
	var query string
	if hasStartDate && hasEndDate {
		query = `
			SELECT
				stop_id,
				stop_code,
				stop_name,
				stop_lat,
				stop_lon,
				location_type,
				parent_station,
				platform_code,
				wheelchair_boarding
			FROM
				stops
			WHERE
				(stop_name = ? OR stop_code = ? OR stop_name || ' ' || stop_code = ?)
				AND (
					(
						(start_date IS NULL OR start_date = '' OR start_date <= ?)
						AND
						(end_date IS NULL OR end_date = '' OR end_date >= ?)
					)
					OR (start_date IS NULL AND end_date IS NULL)
				)
			LIMIT 1
		`
	} else {
		// Simplified query when those columns don't exist
		query = `
			SELECT
				stop_id,
				stop_code,
				stop_name,
				stop_lat,
				stop_lon,
				location_type,
				parent_station,
				platform_code,
				wheelchair_boarding
			FROM
				stops
			WHERE
				(stop_name = ? OR stop_code = ? OR stop_name || ' ' || stop_code = ?)
			LIMIT 1
		`
	}

	var row *sql.Row
	if hasStartDate && hasEndDate {
		row = db.QueryRow(query, nameOrCode, nameOrCode, nameOrCode, now, now)
	} else {
		row = db.QueryRow(query, nameOrCode, nameOrCode, nameOrCode)
	}

	var stop Stop
	err = row.Scan(
		&stop.StopId,
		&stop.StopCode,
		&stop.StopName,
		&stop.StopLat,
		&stop.StopLon,
		&stop.LocationType,
		&stop.ParentStation,
		&stop.PlatformNumber,
		&stop.WheelChairBoarding,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, errors.New("no active stop found")
		}
		return nil, err
	}

	stop.StopType = typeOfStop(stop.StopName)
	return &stop, nil
}

/*
Get a stop by its id
*/
func (v Database) GetStopByStopID(stopID string) (*Stop, error) {
	db := v.db

	query := `
		SELECT
			stop_id,
			stop_code,
			stop_name,
			stop_lat,
			stop_lon,
			location_type,
			parent_station,
			platform_code,
			wheelchair_boarding
		FROM 
			stops
		WHERE
			stop_id = ?
	`

	// Execute the query
	rows := db.QueryRow(query, stopID)

	var stop Stop
	// Scan the row data into the Stop struct
	err := rows.Scan(
		&stop.StopId,
		&stop.StopCode,
		&stop.StopName,
		&stop.StopLat,
		&stop.StopLon,
		&stop.LocationType,
		&stop.ParentStation,
		&stop.PlatformNumber,
		&stop.WheelChairBoarding,
	)
	if err != nil {
		return nil, err
	}
	stop.StopType = typeOfStop(stop.StopName)

	return &stop, nil
}

/*
Get the parent stop to a child stop (if the child is its own parent you just get back the child)
*/
func (v Database) GetParentStopByChildStopID(childStopID string) (*Stop, error) {
	db := v.db

	// Query to fetch either the parent stop or the stop itself if it has no parent
	query := `
		SELECT
			stop_id,
			stop_code,
			stop_name,
			stop_lat,
			stop_lon,
			location_type,
			parent_station,
			platform_code,
			wheelchair_boarding
		FROM
			stops
		WHERE
			stop_id = (
				SELECT
					CASE
						WHEN parent_station = '' OR parent_station IS NULL THEN stop_id
						ELSE parent_station
					END
				FROM
					stops
				WHERE
					stop_id = ?
			)
	`

	// Execute the query with the child stop ID
	row := db.QueryRow(query, childStopID)

	var stop Stop
	err := row.Scan(
		&stop.StopId,
		&stop.StopCode,
		&stop.StopName,
		&stop.StopLat,
		&stop.StopLon,
		&stop.LocationType,
		&stop.ParentStation,
		&stop.PlatformNumber,
		&stop.WheelChairBoarding,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, errors.New("no parent stop or self stop found for the given stop ID")
		}
		return nil, err
	}

	// Determine the stop type (optional, based on your existing logic)
	stop.StopType = typeOfStop(stop.StopName)

	return &stop, nil
}

func (v Database) GetParentStopsByRouteId(routeId string) ([]Stop, error) {
	query := `
    SELECT DISTINCT 
        COALESCE(NULLIF(s.parent_station, ''), s.stop_id) AS parent_stop_id,
        ps.stop_code,
        ps.stop_name,
        ps.stop_lat,
        ps.stop_lon,
        ps.location_type,
        ps.parent_station,
        ps.platform_code,
        ps.wheelchair_boarding
    FROM routes r
    JOIN trips t ON r.route_id = t.route_id
    JOIN stop_times st ON t.trip_id = st.trip_id
    JOIN stops s ON st.stop_id = s.stop_id
    LEFT JOIN stops ps ON ps.stop_id = COALESCE(NULLIF(s.parent_station, ''), s.stop_id)
    WHERE r.route_id = ?
      AND (ps.location_type = 1 OR (ps.location_type = 0 AND (ps.parent_station IS NULL OR ps.parent_station = '')))
    ORDER BY ps.stop_id;
    `

	rows, err := v.db.Query(query, routeId)
	if err != nil {
		return nil, errors.New("no parent stops found for route")
	}
	defer rows.Close()

	// Slice to hold the parent stops
	var parentStops []Stop

	// Iterate over the rows
	for rows.Next() {
		var stop Stop
		err := rows.Scan(
			&stop.StopId,
			&stop.StopCode,
			&stop.StopName,
			&stop.StopLat,
			&stop.StopLon,
			&stop.LocationType,
			&stop.ParentStation,
			&stop.PlatformNumber,
			&stop.WheelChairBoarding,
		)
		if err != nil {
			return nil, err
		}
		parentStops = append(parentStops, stop)
	}

	// Check for any error encountered during iteration
	if err = rows.Err(); err != nil {
		return nil, err
	}

	// If no parent stops were found, return a custom error
	if len(parentStops) == 0 {
		return nil, errors.New("no parent stops found for the given route ID")
	}

	return parentStops, nil
}

/*
Get the stops for a given route
*/
func (v Database) GetStopsByRouteId(routeId string) ([]Stop, error) {
	query := `
	SELECT DISTINCT s.stop_id, s.stop_code, s.stop_name, s.stop_lat, s.stop_lon, s.location_type, s.parent_station, s.platform_code, s.wheelchair_boarding, st.stop_sequence
	FROM routes r
	JOIN trips t ON r.route_id = t.route_id
	JOIN stop_times st ON t.trip_id = st.trip_id
	JOIN stops s ON st.stop_id = s.stop_id
	WHERE r.route_id = ?
	ORDER BY s.stop_id;
	`
	rows, err := v.db.Query(query, routeId)
	if err != nil {
		return nil, errors.New("no stops found for route")
	}

	defer rows.Close()

	// Slice to hold the stops
	var stops Stops

	// Iterate over the rows
	for rows.Next() {
		var stop Stop
		// Scan the row data into the Stop struct
		err := rows.Scan(
			&stop.StopId,
			&stop.StopCode,
			&stop.StopName,
			&stop.StopLat,
			&stop.StopLon,
			&stop.LocationType,
			&stop.ParentStation,
			&stop.PlatformNumber,
			&stop.WheelChairBoarding,
			&stop.Sequence,
		)
		if err != nil {
			return nil, err
		}
		stop.StopType = typeOfStop(stop.StopName)
		// Append each stop to the slice
		stops = append(stops, stop)
	}

	// Check for any error encountered during iteration
	if err = rows.Err(); err != nil {
		return nil, err
	}

	// If no stops were found, return a custom error
	if len(stops) == 0 {
		return nil, errors.New("no stops found for the given trip ID")
	}

	return stops, nil
}

/*
Search the db of stops for a partial name match of a stop
*/
func (v Database) SearchForStopsByNameOrCode(searchText string, includeChildStops bool) ([]StopSearch, error) {
	normalizedSearchText := strings.ToLower(strings.TrimSpace(searchText))
	if normalizedSearchText == "" {
		return nil, errors.New("empty search text")
	}

	words := strings.Fields(normalizedSearchText)

	// Build scoring expression
	scoreExprs := []string{}
	args := []interface{}{}
	for _, w := range words {
		// exact word match (word boundaries using spaces)
		scoreExprs = append(scoreExprs, fmt.Sprintf(`
			(CASE 
				WHEN LOWER(s.stop_name) LIKE '%% ' || ? || ' %%' THEN 3
				WHEN LOWER(s.stop_name) LIKE ? || '%%' THEN 2
				WHEN LOWER(s.stop_name) LIKE '%%' || ? || '%%' THEN 1
				ELSE 0
			END)
		`))
		// arguments for the three checks
		args = append(args, w, w, w)
	}

	scoreExpr := strings.Join(scoreExprs, " + ")

	// Base WHERE clause: require all words appear somewhere
	conditions := []string{}
	for _, w := range words {
		cond := `(LOWER(s.stop_name) LIKE '%' || ? || '%'
		          OR LOWER(s.stop_code) LIKE '%' || ? || '%'
		          OR LOWER(s.stop_id) LIKE '%' || ? || '%'
		          OR LOWER(n.ngram) LIKE '%' || ? || '%')`
		conditions = append(conditions, cond)
		args = append(args, w, w, w, w)
	}

	whereClause := strings.Join(conditions, " AND ")

	query := fmt.Sprintf(`
		SELECT DISTINCT
			s.stop_id,
			s.stop_code,
			s.stop_name,
			s.parent_station,
			s.location_type,
			(%s) AS score
		FROM
			stops s
		LEFT JOIN
			stop_ngrams n ON s.stop_id = n.stop_id
		WHERE %s
		ORDER BY score DESC, s.stop_name ASC
		LIMIT 100;
	`, scoreExpr, whereClause)

	rows, err := v.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var stopSearchResults []StopSearch

	for rows.Next() {
		var stop Stop
		var score int
		err := rows.Scan(&stop.StopId, &stop.StopCode, &stop.StopName, &stop.ParentStation, &stop.LocationType, &score)
		if err != nil {
			return nil, err
		}

		if stop.LocationType == 0 && stop.ParentStation != "" && !includeChildStops {
			continue
		}

		stop.StopType = typeOfStop(stop.StopName)
		stopSearchResults = append(stopSearchResults, StopSearch{
			Name:       stop.StopName + " " + stop.StopCode,
			TypeOfStop: stop.StopType,
		})
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	if len(stopSearchResults) == 0 {
		return nil, errors.New("no stops found for search")
	}

	return stopSearchResults, nil
}

/*
Try to figure out the type of stop based on name
*/
//TODO: fix
func typeOfStop(stopName string) string {
	isFerryTerminal := strings.Contains(stopName, "Ferry Terminal")
	isTrainStation := strings.Contains(stopName, "Train Station")

	if isFerryTerminal {
		return "ferry"
	}
	if isTrainStation {
		return "train"
	}

	//Otherwise it must be a bus right?
	//BECAUSE WHY DO BUS STOPS NOT HAVE `BUS STOP` IN THEM!!!!!
	return "bus"
}

type Stops []Stop

/*
Find the closest stop(s) to a given set of coordinates
*/
func (stops Stops) FindClosestStops(lat, lon float64) []Stop {
	var stopDistances []StopWithDistance

	// Iterate through the map, where each key corresponds to a slice of stops
	// Iterate through the slice of stops for each stop name
	for _, v := range stops {
		stop := v
		// Calculate the distance between the user and the stop
		distance := calculateDistance(lat, lon, stop.StopLat, stop.StopLon)

		// Store stop and its distance
		stopDistances = append(stopDistances, StopWithDistance{
			Stop:     stop,
			Distance: distance,
		})
	}

	// Sort the stops by distance
	sort.Slice(stopDistances, func(i, j int) bool {
		return stopDistances[i].Distance < stopDistances[j].Distance
	})

	// Get the closest 20 stops
	var closestStops Stops
	for i := 0; i < 50 && i < len(stopDistances); i++ {
		closestStops = append(closestStops, stopDistances[i].Stop)
	}

	return closestStops
}

/*
Struct to hold stop data with distance
*/
type StopWithDistance struct {
	Stop     Stop
	Distance float64
}

/*
Calculates the distance between 2 lat and long points
*/
func calculateDistance(lat1, lon1, lat2, lon2 float64) float64 {
	const R = 6371.0 // Radius of the Earth in kilometers
	dLat := (lat2 - lat1) * (math.Pi / 180)
	dLon := (lon2 - lon1) * (math.Pi / 180)

	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1*(math.Pi/180))*math.Cos(lat2*(math.Pi/180))*
			math.Sin(dLon/2)*math.Sin(dLon/2)

	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))

	return R * c // Distance in kilometers
}
