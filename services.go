package gtfs

import (
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

type StopTimes struct {
	TripID        string `json:"trip_id"`
	ArrivalTime   string `json:"arrival_time"`
	DepartureTime string `json:"departure_time"`
	StopId        string `json:"stop_id"`
	StopSequence  int    `json:"stop_sequence"`
	StopHeadsign  string `json:"stop_headsign"`
	Platform      string `json:"platform"`
	StopData      Stop   `json:"stop_data"`
	TripData      Trip   `json:"trip_data"`
	RouteColor    string `json:"route_color"`
}

/*
Get all the services stopping at a given stop (child stop/parent with not children)

  - StopId: the id of the stop. REQUIRED
  - departureTimeFilter: the time to filter from, so any services after departureTimeFilter ("15:03:00"). NOT required, can be ""
  - limit: the amount of services to get. REQUIRED
*/
func (v Database) GetActiveTrips(stopID, departureTimeFilter string, limit int) ([]StopTimes, error) {
	// Open the SQLite database
	db := v.db // Assuming db is already connected, if not, you can open it here

	now := time.Now().In(v.timeZone)
	dayColumn := strings.ToLower(now.Weekday().String())
	dateString := now.Format("20060102")

	// Base query with placeholders for the date and dynamic weekday column
	query := fmt.Sprintf(`
	WITH active_services AS (
		-- Select services from the calendar where today's date falls within start_date and end_date
		SELECT service_id
		FROM calendar
		WHERE start_date <= ? 
		  AND end_date >= ? 
		  AND %s = 1 -- Ensure the service is active on the current day
		UNION ALL
		-- Add services from calendar_dates where exception_type = 1 (added services)
		SELECT service_id
		FROM calendar_dates
		WHERE date = ? AND exception_type = 1
	),
	removed_services AS (
		-- Select services from calendar_dates where exception_type = 2 (removed services)
		SELECT service_id
		FROM calendar_dates
		WHERE date = ? AND exception_type = 2
	),
	adjusted_services AS (
		-- Remove services from active_services that are marked as removed
		SELECT DISTINCT service_id
		FROM active_services
		WHERE service_id NOT IN (SELECT service_id FROM removed_services)
	)
	-- Select trip details for active service_ids from trips and stop_times
	SELECT 
		t.trip_id, 
		t.service_id,
		t.route_id,
		t.direction_id,
		t.shape_id,
		t.trip_headsign,
		st.arrival_time, 
		st.departure_time, 
		st.stop_id, 
		st.stop_sequence, 
		st.stop_headsign, 
		r.route_color, 
		s.stop_name, 
		s.stop_lat, 
		s.stop_lon, 
		s.stop_code, 
		s.location_type, 
		s.parent_station,
		s.platform_code
	FROM trips t
	JOIN adjusted_services a ON t.service_id = a.service_id
	JOIN stop_times st ON t.trip_id = st.trip_id
	JOIN stops s ON st.stop_id = s.stop_id
	JOIN routes r ON t.route_id = r.route_id
	`, dayColumn)

	// Add the departure time filter if specified
	if departureTimeFilter != "" {
		query += " WHERE st.departure_time > ?"
	}

	// If a stop_id is provided, add a filter for stop_id
	if stopID != "" {
		if departureTimeFilter != "" {
			query += " AND st.stop_id = ?"
		} else {
			query += " WHERE st.stop_id = ?"
		}
	}

	query += " ORDER BY st.departure_time ASC"

	// Add limit to the query if specified
	if limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", limit)
	}

	// Execute the query with the variable date, departure time filter, and optionally the stop_id
	var rows *sql.Rows
	var err error
	if departureTimeFilter != "" && stopID != "" {
		rows, err = db.Query(query, dateString, dateString, dateString, dateString, departureTimeFilter, stopID)
	} else if departureTimeFilter != "" {
		rows, err = db.Query(query, dateString, dateString, dateString, dateString, departureTimeFilter)
	} else if stopID != "" {
		rows, err = db.Query(query, dateString, dateString, dateString, dateString, stopID)
	} else {
		rows, err = db.Query(query, dateString, dateString, dateString, dateString)
	}

	if err != nil {
		fmt.Println(err)
		return nil, errors.New("an error occurred querying for the data")
	}
	defer rows.Close()

	// Regular expressions for platform determination
	reStationPlatform := regexp.MustCompile(`Train Station (\d)$`)
	reCapitalLetter := regexp.MustCompile(`[A-Z]$`)

	// Iterate through the result set
	var results []StopTimes
	for rows.Next() {
		var result struct {
			TripId              string
			ServiceId           string
			RouteId             string
			DirectionId         int
			ShapeId             string
			TripHeadsign        string
			ArrivalTime         string
			DepartureTime       string
			StopId              string
			StopSequence        int
			StopHeadsign        string
			RouteColor          string
			StopName            string
			StopLat             float64
			StopLon             float64
			StopCode            string
			StopLocationType    int
			StopParentStationId string
			Platform            string
		}

		// Scan the results into StopTimes, Stop, and Trip
		if err := rows.Scan(
			&result.TripId,
			&result.ServiceId,
			&result.RouteId,
			&result.DirectionId,
			&result.ShapeId,
			&result.TripHeadsign,
			&result.ArrivalTime,
			&result.DepartureTime,
			&result.StopId,
			&result.StopSequence,
			&result.StopHeadsign,
			&result.RouteColor,
			&result.StopName,
			&result.StopLat,
			&result.StopLon,
			&result.StopCode,
			&result.StopLocationType,
			&result.StopParentStationId,
			&result.Platform,
		); err != nil {
			return nil, err
		}

		if result.Platform == "" {
			result.Platform = determinePlatform(result.StopName, reStationPlatform, reCapitalLetter)
		}

		var stopData = Stop{
			LocationType:       result.StopLocationType,
			ParentStation:      result.StopParentStationId,
			StopCode:           result.StopCode,
			StopId:             result.StopId,
			StopLat:            result.StopLat,
			StopLon:            result.StopLon,
			StopName:           result.StopName,
			WheelChairBoarding: 0,
			PlatformNumber:     result.Platform,
			StopType:           typeOfStop(result.StopName),
			Sequence:           result.StopSequence,
		}
		var tripData = Trip{
			BikesAllowed:         0,
			DirectionID:          result.DirectionId,
			RouteID:              result.RouteId,
			ServiceID:            result.ServiceId,
			ShapeID:              result.ShapeId,
			TripHeadsign:         result.TripHeadsign,
			TripID:               result.TripId,
			WheelchairAccessible: 0,
		}

		var stopTimeData StopTimes = StopTimes{
			TripID:        result.TripId,
			ArrivalTime:   result.ArrivalTime,
			DepartureTime: result.DepartureTime,
			StopId:        result.StopId,
			StopSequence:  result.StopSequence,
			StopHeadsign:  result.StopHeadsign,
			Platform:      result.Platform,
			StopData:      stopData,
			TripData:      tripData,
		}

		// Append the result
		results = append(results, stopTimeData)
	}

	// Check for any error during iteration
	if err := rows.Err(); err != nil {
		fmt.Println(err)
		return nil, errors.New("an error occurred going through the retrieved data")
	}
	return results, nil
}

/*
Get the service stopping at a given stop, based on its trip id

Because it's searching by trip id only one service will be returned (if found)
*/
func (v Database) GetServiceByTripAndStop(tripID, stopId, departureTimeFilter string) (StopTimes, error) {
	if tripID == "" {
		return StopTimes{}, errors.New("missing trip id")
	}

	// Open the SQLite database
	db := v.db // Assuming db is already connected

	// Base query to fetch details for the specific trip_id
	query := `
		SELECT 
			t.trip_id, 
			t.service_id,
			t.route_id,
			t.direction_id,
			t.shape_id,
			t.trip_headsign,
			st.arrival_time, 
			st.departure_time, 
			st.stop_id, 
			st.stop_sequence, 
			st.stop_headsign, 
			r.route_color, 
			s.stop_name, 
			s.stop_lat, 
			s.stop_lon, 
			s.stop_code, 
			s.location_type, 
			s.parent_station,
			s.platform_code
		FROM trips t
		JOIN stop_times st ON t.trip_id = st.trip_id
		JOIN stops s ON st.stop_id = s.stop_id
		JOIN routes r ON t.route_id = r.route_id
		WHERE t.trip_id = ? -- Filter by trip_id
		AND st.stop_id = ? -- Filter by stop_id
	`

	if departureTimeFilter != "" {
		query += " AND st.departure_time > ?"
	}

	query += " ORDER BY st.departure_time ASC"

	// Execute the query with the provided trip_id
	rows := db.QueryRow(query, tripID, stopId, departureTimeFilter)

	// Regular expressions for platform determination
	reStationPlatform := regexp.MustCompile(`Train Station (\d)$`)
	reCapitalLetter := regexp.MustCompile(`[A-Z]$`)

	// Struct to hold the result data
	var result struct {
		TripId              string
		ServiceId           string
		RouteId             string
		DirectionId         int
		ShapeId             string
		TripHeadsign        string
		ArrivalTime         string
		DepartureTime       string
		StopId              string
		StopSequence        int
		StopHeadsign        string
		RouteColor          string
		StopName            string
		StopLat             float64
		StopLon             float64
		StopCode            string
		StopLocationType    int
		StopParentStationId string
		Platform            string
	}

	// Scan the result into the struct
	if err := rows.Scan(
		&result.TripId,
		&result.ServiceId,
		&result.RouteId,
		&result.DirectionId,
		&result.ShapeId,
		&result.TripHeadsign,
		&result.ArrivalTime,
		&result.DepartureTime,
		&result.StopId,
		&result.StopSequence,
		&result.StopHeadsign,
		&result.RouteColor,
		&result.StopName,
		&result.StopLat,
		&result.StopLon,
		&result.StopCode,
		&result.StopLocationType,
		&result.StopParentStationId,
		&result.Platform,
	); err != nil {
		return StopTimes{}, err
	}

	// If platform is empty, use the helper function to determine the platform
	if result.Platform == "" {
		result.Platform = determinePlatform(result.StopName, reStationPlatform, reCapitalLetter)
	}

	// Create Stop data
	var stopData = Stop{
		LocationType:       result.StopLocationType,
		ParentStation:      result.StopParentStationId,
		StopCode:           result.StopCode,
		StopId:             result.StopId,
		StopLat:            result.StopLat,
		StopLon:            result.StopLon,
		StopName:           result.StopName,
		WheelChairBoarding: 0,
		PlatformNumber:     result.Platform,
		StopType:           typeOfStop(result.StopName),
		Sequence:           result.StopSequence,
	}

	// Create Trip data
	var tripData = Trip{
		BikesAllowed:         0,
		DirectionID:          result.DirectionId,
		RouteID:              result.RouteId,
		ServiceID:            result.ServiceId,
		ShapeID:              result.ShapeId,
		TripHeadsign:         result.TripHeadsign,
		TripID:               result.TripId,
		WheelchairAccessible: 0,
	}

	// Combine the data into StopTimes struct
	var stopTimeData StopTimes = StopTimes{
		TripID:        result.TripId,
		ArrivalTime:   result.ArrivalTime,
		DepartureTime: result.DepartureTime,
		StopId:        result.StopId,
		StopSequence:  result.StopSequence,
		StopHeadsign:  result.StopHeadsign,
		Platform:      result.Platform,
		StopData:      stopData,
		TripData:      tripData,
	}

	// Check for any error during the query execution
	if err := rows.Err(); err != nil {
		fmt.Println(err)
		return StopTimes{}, errors.New("an error occurred building for the data")
	}

	// Return the result
	return stopTimeData, nil
}

/*
Function to determine the platform number based on stop name

(only use if you don't have a platform_code)
*/
func determinePlatform(stopName string, reStationPlatform, reCapitalLetter *regexp.Regexp) string {
	if matches := reStationPlatform.FindStringSubmatch(stopName); len(matches) > 1 {
		return matches[1]
	}
	if strings.HasSuffix(stopName, "Train Station") && !regexp.MustCompile(`\d$`).MatchString(stopName) {
		return "1"
	}
	if reCapitalLetter.MatchString(stopName) {
		return string(stopName[len(stopName)-1])
	}
	return "no platform"
}
