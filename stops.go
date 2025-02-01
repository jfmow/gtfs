package gtfs

import (
	"database/sql"
	"errors"
	"math"
	"sort"
	"strings"
)

type Stop struct {
	LocationType       int     `json:"location_type"`
	ParentStation      string  `json:"parent_station"`
	StopCode           string  `json:"stop_code"`
	StopId             string  `json:"stop_id"`
	StopLat            float64 `json:"stop_lat"`
	StopLon            float64 `json:"stop_lon"`
	StopName           string  `json:"stop_name"`
	WheelChairBoarding int     `json:"wheelchair_boarding"`
	PlatformNumber     string  `json:"platform_number"`
	StopType           string  `json:"stop_type"`
	Sequence           int     `json:"stop_sequence"`
}

type StopSearch struct {
	Name       string `json:"name"`
	TypeOfStop string `json:"type_of_stop"`
}

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

	var stops Stops

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
		stop.StopType = typeOfStop(stop.StopName)
		stops = append(stops, stop)
	}

	// Check for any error encountered during iteration
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
*/
func (v Database) GetStopsForTripID(tripID string) ([]Stop, error) {
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
		ORDER BY
			st.stop_sequence
	`

	// Execute the query
	rows, err := db.Query(query, tripID)
	if err != nil {
		return nil, err
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
Get a stop by its name or its stop code
*/
func (v Database) GetStopByNameOrCode(nameOrCode string) (*Stop, error) {
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
			STOPS
		WHERE
			stop_name = ?
		OR
			stop_code = ?
		OR
			stop_name || ' ' || stop_code = ?
	`

	// Execute the query
	rows := db.QueryRow(query, nameOrCode, nameOrCode, nameOrCode)

	// Slice to hold all the stops
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
		if err == sql.ErrNoRows {
			return nil, errors.New("no stop found")
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
	// Normalize the input search text and make it lowercase
	normalizedSearchText := strings.ToLower(searchText)

	query := `
		SELECT
			stop_id,
			stop_code,
			stop_name,
			parent_station,
			location_type
		FROM
			stops
		WHERE
			LOWER(stop_name) LIKE ?
		OR
			stop_code LIKE ?
		OR
			stop_name || ' ' || stop_code LIKE ?
	`

	// Run the query
	parametrizedString := "%" + normalizedSearchText + "%"
	rows, err := v.db.Query(query, parametrizedString, parametrizedString, parametrizedString)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var stopSearchResults []StopSearch

	// Iterate over the rows
	for rows.Next() {
		var stop Stop
		err := rows.Scan(&stop.StopId, &stop.StopCode, &stop.StopName, &stop.ParentStation, &stop.LocationType)
		if err != nil {
			return nil, err
		}
		if stop.LocationType == 0 && stop.ParentStation != "" && !includeChildStops {
			continue
		}
		stop.StopType = typeOfStop(stop.StopName) // Set the stop type
		stopSearchResults = append(stopSearchResults, StopSearch{Name: stop.StopName + " " + stop.StopCode, TypeOfStop: stop.StopType})
	}

	// Check for any error encountered during iteration
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
