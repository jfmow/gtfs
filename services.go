package gtfs

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

type StopTimes struct {
	TripID         string `json:"trip_id"`
	ArrivalTime    string `json:"arrival_time"`
	DepartureTime  string `json:"departure_time"`
	StopId         string `json:"stop_id"`
	StopSequence   int    `json:"stop_sequence"`
	StopHeadsign   string `json:"stop_headsign"`
	Platform       string `json:"platform"`
	StopData       Stop   `json:"stop_data"`
	TripData       Trip   `json:"trip_data"`
	RouteColor     string `json:"route_color"`
	RouteShortName string `json:"route_short_name"`
}

/*
Get all the services stopping at a given stop (child stop/parent with not children)

  - StopId: the id of the stop. REQUIRED
  - departureTimeFilter: the time to filter from, so any services after departureTimeFilter ("15:03:00"). NOT required, can be ""
  - limit: the amount of services to get. REQUIRED
  - date: "20060102"
*/
func (v Database) GetActiveTrips(stopID, departureTimeFilter string, date time.Time, limit int) ([]StopTimes, error) {
	if departureTimeFilter != "" {
		_, err := time.Parse("15:04:05", departureTimeFilter)
		if err != nil {
			return nil, fmt.Errorf("invalid departureTimeFilter format, expected HH:MM:SS: %v", err)
		}
	}

	if limit < 0 {
		return nil, errors.New("limit cannot be negative")
	}

	db := v.db

	now := date
	dayColumn := strings.ToLower(now.Weekday().String())
	dateString := now.Format("20060102")

	var queryBuilder strings.Builder

	fmt.Fprintf(&queryBuilder, `
	WITH active_services AS (
		SELECT service_id
		FROM calendar
		WHERE start_date <= ? 
		  AND end_date >= ? 
		  AND %s = 1
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
		SELECT DISTINCT service_id
		FROM active_services
		WHERE service_id NOT IN (SELECT service_id FROM removed_services)
	)
	SELECT 
		t.trip_id, 
		t.service_id,
		t.route_id,
		t.direction_id,
		t.shape_id,
		t.trip_headsign,
		t.wheelchair_accessible,
		t.bikes_allowed,
		st.arrival_time, 
		st.departure_time, 
		st.stop_id, 
		st.stop_sequence, 
		st.stop_headsign, 
		r.route_color, 
		r.route_short_name,
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
	WHERE (st.pickup_type IS NULL OR st.pickup_type = 0)
    AND (st.drop_off_type IS NULL OR st.drop_off_type = 0)
	`, dayColumn)

	if departureTimeFilter != "" {
		queryBuilder.WriteString(" AND st.departure_time > ?")
	}

	if stopID != "" {
		if departureTimeFilter != "" {
			queryBuilder.WriteString(" AND st.stop_id = ?")
		} else {
			queryBuilder.WriteString(" AND st.stop_id = ?")
		}
	}

	queryBuilder.WriteString(" ORDER BY st.departure_time ASC")

	if limit > 0 {
		fmt.Fprintf(&queryBuilder, " LIMIT %d", limit)
	}

	query := queryBuilder.String()

	// Prepare arguments in correct order
	args := []interface{}{dateString, dateString, dateString, dateString}
	if departureTimeFilter != "" {
		args = append(args, departureTimeFilter)
	}
	if stopID != "" {
		args = append(args, stopID)
	}

	rows, err := db.Query(query, args...)
	if err != nil {
		fmt.Println(err)
		return nil, errors.New("an error occurred querying for the data")
	}
	defer rows.Close()

	reStationPlatform := regexp.MustCompile(`Train Station (\d)$`)
	reCapitalLetter := regexp.MustCompile(`[A-Z]$`)

	var results []StopTimes
	for rows.Next() {
		var result struct {
			TripId               string
			ServiceId            string
			RouteId              string
			DirectionId          int
			ShapeId              string
			TripHeadsign         string
			ArrivalTime          string
			DepartureTime        string
			StopId               string
			StopSequence         int
			StopHeadsign         string
			RouteColor           string
			StopName             string
			StopLat              float64
			StopLon              float64
			StopCode             string
			StopLocationType     int
			StopParentStationId  string
			Platform             string
			RouteShortName       string
			WheelchairAccessible int
			BikesAllowed         int
		}

		if err := rows.Scan(
			&result.TripId,
			&result.ServiceId,
			&result.RouteId,
			&result.DirectionId,
			&result.ShapeId,
			&result.TripHeadsign,
			&result.WheelchairAccessible,
			&result.BikesAllowed,
			&result.ArrivalTime,
			&result.DepartureTime,
			&result.StopId,
			&result.StopSequence,
			&result.StopHeadsign,
			&result.RouteColor,
			&result.RouteShortName,
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

		stopData := Stop{
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

		tripData := Trip{
			BikesAllowed:         result.BikesAllowed,
			DirectionID:          result.DirectionId,
			RouteID:              result.RouteId,
			ServiceID:            result.ServiceId,
			ShapeID:              result.ShapeId,
			TripHeadsign:         result.TripHeadsign,
			TripID:               result.TripId,
			WheelchairAccessible: result.WheelchairAccessible,
		}

		results = append(results, StopTimes{
			TripID:         result.TripId,
			ArrivalTime:    result.ArrivalTime,
			DepartureTime:  result.DepartureTime,
			StopId:         result.StopId,
			StopSequence:   result.StopSequence,
			StopHeadsign:   result.StopHeadsign,
			Platform:       result.Platform,
			StopData:       stopData,
			TripData:       tripData,
			RouteColor:     result.RouteColor,
			RouteShortName: result.RouteShortName,
		})
	}

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
			r.route_short_name,
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
		AND st.pickup_type != 1 -- Exclude drop_off_only stops
		AND st.drop_off_type != 1 -- Exclude pick_up_only stops
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
		RouteShortName      string
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
		&result.RouteShortName,
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
		TripID:         result.TripId,
		ArrivalTime:    result.ArrivalTime,
		DepartureTime:  result.DepartureTime,
		StopId:         result.StopId,
		StopSequence:   result.StopSequence,
		StopHeadsign:   result.StopHeadsign,
		Platform:       result.Platform,
		StopData:       stopData,
		TripData:       tripData,
		RouteShortName: result.RouteShortName,
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
