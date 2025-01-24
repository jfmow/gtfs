package gtfs

import (
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	sq "github.com/Masterminds/squirrel"
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

func (v Database) GetActiveTrips(date, currentWeekDay, stopID, departureTimeFilter string, limit int) ([]StopTimes, error) {
	// Open the SQLite database
	db := v.db // Assuming db is already connected, if not, you can open it here

	// Base query with placeholders for the date
	dayColumn := map[string]string{
		"Sunday":    "sunday",
		"Monday":    "monday",
		"Tuesday":   "tuesday",
		"Wednesday": "wednesday",
		"Thursday":  "thursday",
		"Friday":    "friday",
		"Saturday":  "saturday",
	}[currentWeekDay]

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
		rows, err = db.Query(query, date, date, date, date, departureTimeFilter, stopID)
	} else if departureTimeFilter != "" {
		rows, err = db.Query(query, date, date, date, date, departureTimeFilter)
	} else if stopID != "" {
		rows, err = db.Query(query, date, date, date, date, stopID)
	} else {
		rows, err = db.Query(query, date, date, date, date)
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
		return nil, errors.New("An error occurred building for the data")
	}
	return results, nil
}

// Function to determine the platform number based on stop name
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

//TODO: use pre processing

// Check if the current week has been processed
func (v Database) hasProcessedCurrentWeek(today time.Time) (bool, error) {
	var lastProcessedWeek string

	// Get the current week's number (Monday as the start of the week)
	currentYear, currentWeek := today.ISOWeek()
	currentWeekStr := fmt.Sprintf("%d-W%d", currentYear, currentWeek)

	// Build the query using squirrel
	baseQuery := sq.Select("last_processed_week").
		From("week_cache").
		Where(sq.Eq{"last_processed_week": currentWeekStr})

	// Execute the query
	err := baseQuery.RunWith(v.db).QueryRow().Scan(&lastProcessedWeek)
	if err != nil && err != sql.ErrNoRows {
		return false, fmt.Errorf("failed to query last processed week: %w", err)
	}

	// Check if the current week matches the last processed week
	return lastProcessedWeek == currentWeekStr, nil
}

// Mark the current week as processed
func (v Database) markWeekAsProcessed(today time.Time) error {
	currentYear, currentWeek := today.ISOWeek()
	currentWeekStr := fmt.Sprintf("%d-W%d", currentYear, currentWeek)

	// Delete any existing entry for the current week
	deleteQuery := sq.Delete("week_cache").
		Where(sq.Eq{"last_processed_week": currentWeekStr})

	_, err := deleteQuery.RunWith(v.db).Exec()
	if err != nil {
		return fmt.Errorf("failed to delete existing week entry: %w", err)
	}

	// Insert the current week string
	insertQuery := sq.Insert("week_cache").
		Columns("last_processed_week").
		Values(currentWeekStr)

	_, err = insertQuery.RunWith(v.db).Exec()
	if err != nil {
		return fmt.Errorf("failed to insert processed week: %w", err)
	}
	return nil
}

func getNext7Days(start, end time.Time) []string {
	var dates []string

	// Loop for the next 7 days or until the endDate, whichever comes first
	for i := 0; i < 7; i++ {
		// Calculate the current date
		current := start.AddDate(0, 0, i)

		// If the current date exceeds the end date, stop
		if current.After(end) {
			break
		}

		// Format the date as YYYYMMDD
		dateStr := fmt.Sprintf("%d%02d%02d", current.Year(), current.Month(), current.Day())
		dates = append(dates, dateStr)
	}

	return dates
}
