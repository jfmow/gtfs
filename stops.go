package gtfs

import (
	"errors"
	"math"
	"sort"
	"strings"

	sq "github.com/Masterminds/squirrel"
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

type Stops []Stop

func (v Database) GetStops() (Stops, error) {
	db := v.db
	baseQuery := sq.Select("stop_id", "stop_code", "stop_name", "stop_lat", "stop_lon", "location_type", "parent_station", "platform_code", "wheelchair_boarding").From("stops")

	rows, err := baseQuery.RunWith(db).Query()

	if err != nil {
		return nil, err
	}

	defer rows.Close() // Ensure the rows are closed after usage

	// Slice to hold all the trips
	var stops Stops

	// Iterate over the rows
	for rows.Next() {
		var stop Stop
		// Scan the row data into the trip struct
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
		// Append each trip to the slice
		stops = append(stops, stop)
	}

	// Check for any error encountered during iteration
	if err = rows.Err(); err != nil {
		return nil, err
	}

	// If no trips were found, return a custom error
	if len(stops) == 0 {
		return nil, errors.New("no stops found")
	}

	return stops, nil
}

func (v Database) GetStopsForTripID(tripID string) (Stops, error) {
	db := v.db

	// Query to select stop details for a given tripID by joining stop_times with stops
	baseQuery := sq.Select(
		"s.stop_id", "s.stop_code", "s.stop_name", "s.stop_lat", "s.stop_lon", "s.location_type", "s.parent_station", "s.platform_code", "s.wheelchair_boarding", "st.stop_sequence").
		From("stop_times st").
		Join("stops s ON st.stop_id = s.stop_id").
		Where(sq.Eq{"st.trip_id": tripID}).
		OrderBy("st.stop_sequence") // Order by the stop sequence in stop_times

	// Execute the query
	rows, err := baseQuery.RunWith(db).Query()
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

func (v Database) GetStopByNameOrCode(nameOrCode string) (Stops, error) {
	db := v.db

	// Base query to select stops
	baseQuery := sq.Select(
		"stop_id", "stop_code", "stop_name", "stop_lat", "stop_lon",
		"location_type", "parent_station", "platform_code", "wheelchair_boarding").
		From("stops")

	// Use a WHERE clause to search by stop name, stop code, or a combination of both
	whereClause := sq.Or{
		sq.Eq{"stop_name": nameOrCode},                           // Search by stop name
		sq.Eq{"stop_code": nameOrCode},                           // Search by stop code
		sq.Expr("stop_name || ' ' || stop_code = ?", nameOrCode), // Search by name + stop code
	}

	baseQuery = baseQuery.Where(whereClause)

	// Execute the query
	rows, err := baseQuery.RunWith(db).Query()
	if err != nil {
		return nil, err
	}
	defer rows.Close() // Ensure the rows are closed after usage

	// Slice to hold all the stops
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
		return nil, errors.New("no stops found")
	}

	return stops, nil
}

func (v Database) GetStopByStopID(stopID string) (Stops, error) {
	db := v.db

	// Base query to select stops
	baseQuery := sq.Select(
		"stop_id", "stop_code", "stop_name", "stop_lat", "stop_lon",
		"location_type", "parent_station", "platform_code", "wheelchair_boarding").
		From("stops")

	// Use a WHERE clause to search by stop name, stop code, or a combination of both
	whereClause := sq.Or{
		sq.Eq{"stop_id": stopID}, // Search by name + stop code
	}

	baseQuery = baseQuery.Where(whereClause)

	// Execute the query
	rows, err := baseQuery.RunWith(db).Query()
	if err != nil {
		return nil, err
	}
	defer rows.Close() // Ensure the rows are closed after usage

	// Slice to hold all the stops
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
		return nil, errors.New("no stops found")
	}

	return stops, nil
}

func (v Database) GetChildStopsByParentStopID(stopID string) (Stops, error) {
	// Base query to select stops
	stops, err := v.GetStops()
	if err != nil {
		return nil, errors.New("no stops found")
	}

	var result Stops

	for _, a := range stops {
		if a.ParentStation == stopID || a.StopId == stopID {
			result = append(result, a)
		}
	}

	// If no stops were found, return a custom error
	if len(result) == 0 {
		return nil, errors.New("no child stops found")
	}

	return result, nil
}

// SearchForStopsByName searches for stops based on a partial name match
func (v Database) SearchForStopsByName(searchText string) ([]StopSearch, error) {
	// Normalize the input search text and make it lowercase
	normalizedSearchText := strings.ToLower(searchText)

	// Create a SQL query to find matching stops
	query := sq.Select("stop_id, stop_code, stop_name")
	query = query.From("stops")
	query = query.Where(sq.Like{"LOWER(stop_name)": "%" + normalizedSearchText + "%"})

	// Run the query
	rows, err := query.RunWith(v.db).Query()
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var stopSearchResults []StopSearch

	// Iterate over the rows
	for rows.Next() {
		var stop Stop
		err := rows.Scan(&stop.StopId, &stop.StopCode, &stop.StopName)
		if err != nil {
			return nil, err
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

func (stops Stops) FindClosestStops(userLat, userLon float64) Stops {
	var stopDistances []StopWithDistance

	// Iterate through the map, where each key corresponds to a slice of stops
	// Iterate through the slice of stops for each stop name
	for _, v := range stops {
		stop := v
		// Calculate the distance between the user and the stop
		distance := calculateDistance(userLat, userLon, stop.StopLat, stop.StopLon)

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
