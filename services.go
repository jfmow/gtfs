package gtfs

import (
	"database/sql"
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

/*
Date e.g: 20241120
Day of week: monday
*/
func (v Database) GetServicesAtStop(stopID, dayOfWeek, date string) ([]StopTimes, error) {
	db := v.db

	// Initialize squirrel builder
	sb := sq.StatementBuilder.PlaceholderFormat(sq.Question)

	// Step 1: Define the active services query
	activeServices := sb.Select("service_id").From("calendar").
		Where(fmt.Sprintf("%s = ?", dayOfWeek), 1).
		Where("start_date <= ?", date).
		Where("end_date >= ?", date)

	// Step 2: Add exceptions for added services on the specific date
	addedExceptions := sb.Select("service_id").From("calendar_dates").
		Where("date = ?", date).
		Where("exception_type = ?", 1)

	// Step 3: Identify inactive services for the given date
	inactiveExceptions := sb.Select("service_id").From("calendar_dates").
		Where("date = ?", date).
		Where("exception_type = ?", 2)

	// Convert each part to SQL
	activeServicesSQL, activeArgs, err := activeServices.ToSql()
	if err != nil {
		return nil, err
	}
	addedExceptionsSQL, addedArgs, err := addedExceptions.ToSql()
	if err != nil {
		return nil, err
	}
	inactiveExceptionsSQL, inactiveArgs, err := inactiveExceptions.ToSql()
	if err != nil {
		return nil, err
	}

	// Step 4: Construct the CTEs using a WITH clause
	fullQuery := fmt.Sprintf(`
	WITH active_services AS (
		%s
		UNION
		%s
	),
	inactive_services AS (
		%s
	),
	valid_services AS (
		SELECT service_id
		FROM active_services
		WHERE service_id NOT IN (SELECT service_id FROM inactive_services)
	),
	trips_for_services AS (
		SELECT trip_id, service_id, route_id
		FROM trips
		WHERE service_id IN (SELECT service_id FROM valid_services)
	)
	SELECT 
		st.trip_id, 
		st.arrival_time, 
		st.departure_time, 
		st.stop_id, 
		st.stop_sequence, 
		st.stop_headsign, 
		tfs.service_id, 
		tfs.route_id, 
		r.route_color,
		s.stop_id, 
		s.stop_name, 
		s.stop_lat, 
		s.stop_lon, 
		s.stop_code, 
		s.location_type, 
		s.parent_station, 
		s.platform_code
	FROM stop_times AS st
	JOIN trips_for_services AS tfs ON st.trip_id = tfs.trip_id
	JOIN stops AS s ON st.stop_id = s.stop_id
	JOIN routes AS r ON tfs.route_id = r.route_id
	WHERE s.stop_id = ?
	ORDER BY st.arrival_time
`, activeServicesSQL, addedExceptionsSQL, inactiveExceptionsSQL)

	// Combine all arguments
	args := append(activeArgs, addedArgs...)
	args = append(args, inactiveArgs...)
	args = append(args, stopID)

	// Execute the query
	rows, err := db.Query(fullQuery, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	reStationPlatform := regexp.MustCompile(`Train Station (\d)$`)
	reCapitalLetter := regexp.MustCompile(`[A-Z]$`)

	// Collect the results
	var results []StopTimes
	for rows.Next() {
		var row StopTimes
		var stop Stop
		var trip Trip
		if err := rows.Scan(
			&row.TripID,
			&row.ArrivalTime,
			&row.DepartureTime,
			&row.StopId,
			&row.StopSequence,
			&row.StopHeadsign,
			&trip.ServiceID,
			&trip.RouteID,
			&row.RouteColor, // Scan route_color from routes table
			&stop.StopId,
			&stop.StopName,
			&stop.StopLat,
			&stop.StopLon,
			&stop.StopCode,
			&stop.LocationType,
			&stop.ParentStation,
			&stop.PlatformNumber,
		); err != nil {
			return nil, err
		}
		row.StopData = stop
		row.TripData = trip

		if row.Platform == "" {
			row.Platform = determinePlatform(stop.StopName, reStationPlatform, reCapitalLetter)
		}
		row.TripData = trip

		stop.StopType = typeOfStop(stop.StopName)
		row.StopData = stop
		results = append(results, row)
	}

	return results, nil
}

/*func (v Database) GetCachedServicesAtStop(stopID string, startHour int, hourRange int, date string) ([]StopTimes, error) {
	today := time.Now()
	if date == "" {
		date = fmt.Sprintf("%d%02d%02d", today.Year(), int(today.Month()), today.Day())
	}

	baseQuery := sq.Select("service_data").
		From("services_cache").
		Where(sq.Eq{"stop_id": stopID, "date": date})

	row := baseQuery.RunWith(v.db).QueryRow()

	type cachedData struct {
		Data string
	}

	var cache cachedData

	row.Scan(
		&cache.Data,
	)

	var result []StopTimes

	err := json.Unmarshal([]byte(cache.Data), &result)
	if err != nil {
		return nil, err
	}

	return result, nil
}*/

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
