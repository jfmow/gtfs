package gtfs

import (
	"database/sql"
	"encoding/json"
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

func (v Database) GetServicesAtStop(stopID string, startHour int, hourRange int, date string) ([]StopTimes, error) {
	db := v.db

	// Precompute date and dayOfWeek once
	today := time.Now()
	if date == "" {
		date = fmt.Sprintf("%d%02d%02d", today.Year(), int(today.Month()), today.Day())
	}
	dayOfWeek := today.Weekday().String()

	// Combine query to check for child stops and use original stop if none found
	childStopsQuery := sq.Select("stop_id").
		From("stops").
		Where(sq.Eq{"parent_station": stopID})
	childRows, err := childStopsQuery.RunWith(db).Query()
	if err != nil {
		return nil, err
	}
	defer childRows.Close()

	var stopIDsToQuery []string
	for childRows.Next() {
		var childStopID string
		if err := childRows.Scan(&childStopID); err != nil {
			return nil, err
		}
		stopIDsToQuery = append(stopIDsToQuery, childStopID)
	}
	if len(stopIDsToQuery) == 0 {
		stopIDsToQuery = []string{stopID}
	}

	// Precompute service_id SQL with UNION and reuse in the main query
	// Get regular services for the date
	serviceQuery := sq.Select("service_id").From("calendar").
		Where(sq.LtOrEq{"start_date": date}).
		Where(sq.GtOrEq{"end_date": date}).
		Where(sq.Eq{dayOfWeek: 1})
	
	// Get special added services (exception_type = 1) from calendar_dates
	specialServiceQuery := sq.Select("service_id").From("calendar_dates").
		Where(sq.Eq{"date": date, "exception_type": 1})

	// Exclude services that are explicitly removed (exception_type = 2) on this date
	excludedServiceQuery := sq.Select("service_id").From("calendar_dates").
		Where(sq.Eq{"date": date, "exception_type": 2})

	serviceSQL, serviceArgs, err := serviceQuery.ToSql()
	if err != nil {
		return nil, err
	}
	specialServiceSQL, specialArgs, err := specialServiceQuery.ToSql()
	if err != nil {
		return nil, err
	}
	excludedServiceSQL, excludedArgs, err := excludedServiceQuery.ToSql()
	if err != nil {
		return nil, err
	}

	// Combine regular services and special services with UNION
	unionSQL := fmt.Sprintf("%s UNION %s", serviceSQL, specialServiceSQL)
	serviceArgs = append(serviceArgs, specialArgs...)

	startTime := time.Date(0, 1, 1, startHour, 0, 0, 0, time.UTC) // startHour in HH:00:00 format
	endTime := startTime.Add(time.Duration(hourRange) * time.Hour)
	endOfDay := time.Date(today.Year(), today.Month(), today.Day(), 23, 59, 59, 0, time.UTC)
	if endTime.After(endOfDay) {
		endTime = endOfDay // If it exceeds, set to the end of the day
	}
	// Format the start and end times as "HH:MM:SS" strings
	startTimeStr := startTime.Format("15:04:05")
	endTimeStr := endTime.Format("15:04:05")

	// Main query needs to exclude services from `excludedServiceQuery`
	// Format excludedServiceSQL as a subquery for filtering
	mainQuery := sq.Select(
		"st.trip_id", "st.arrival_time", "st.departure_time", "st.stop_id", "st.stop_sequence", "st.stop_headsign",
		"s.stop_id", "s.stop_name", "s.stop_lat", "s.stop_lon", "s.stop_code", "s.location_type", "s.parent_station",
		"s.wheelchair_boarding",
		"t.route_id", "t.trip_headsign", "t.shape_id", "t.service_id", "t.direction_id", "t.wheelchair_accessible", "t.bikes_allowed", "r.route_color").
		From("stop_times st").
		Join("trips t ON st.trip_id = t.trip_id"). // Joining trips to fetch trip data
		Join("stops s ON st.stop_id = s.stop_id").
		Join("routes r ON t.route_id = r.route_id").
		Where(sq.Eq{"st.stop_id": stopIDsToQuery}).
		Where(fmt.Sprintf("t.service_id IN (%s)", unionSQL), serviceArgs...).
		Where(fmt.Sprintf("t.service_id NOT IN (%s)", excludedServiceSQL), excludedArgs...). // Exclude removed services
		Where(sq.GtOrEq{"st.arrival_time": startTimeStr}).
		Where(sq.LtOrEq{"st.arrival_time": endTimeStr}). // Filtering by hour range
		OrderBy("st.arrival_time")

	rows, err := mainQuery.RunWith(db).Query()
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// Pre-compiled regex for determining the platform
	reStationPlatform := regexp.MustCompile(`Train Station (\d)$`)
	reCapitalLetter := regexp.MustCompile(`[A-Z]$`)

	// Process results
	var services []StopTimes
	for rows.Next() {
		var trip StopTimes
		var stop Stop
		var tripData Trip

		err := rows.Scan(
			&trip.TripID,
			&trip.ArrivalTime,
			&trip.DepartureTime,
			&trip.StopId,
			&trip.StopSequence,
			&trip.StopHeadsign,
			&stop.StopId,
			&stop.StopName,
			&stop.StopLat,
			&stop.StopLon,
			&stop.StopCode,
			&stop.LocationType,
			&stop.ParentStation,
			&stop.WheelChairBoarding,
			&tripData.RouteID,
			&tripData.TripHeadsign,
			&tripData.ShapeID,
			&tripData.ServiceID,
			&tripData.DirectionID,
			&tripData.WheelchairAccessible,
			&tripData.BikesAllowed,
			&trip.RouteColor,
		)
		if err != nil {
			return nil, err
		}

		// Assign stop data and trip data
		stop.StopType = typeOfStop(stop.StopName)
		trip.StopData = stop
		trip.TripData = tripData

		// Assign platform
		trip.Platform = determinePlatform(stop.StopName, reStationPlatform, reCapitalLetter)

		// Collect results
		services = append(services, trip)
	}

	// Check for errors after row iteration
	if err = rows.Err(); err != nil {
		return nil, err
	}

	// Return error if no services found
	if len(services) == 0 {
		return nil, errors.New("no trips found for the given stop on this day")
	}

	return services, nil
}


func (v Database) GetCachedServicesAtStop(stopID string, startHour int, hourRange int, date string) ([]StopTimes, error) {
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

func (v Database) precomputeServices() (bool, error) {
	fmt.Println("Precomputing services for the next 7 days.")

	// Get the current date
	today := time.Now()

	createTableIfNotExists(v.db, "week_cache", []string{"last_processed_week"})

	// Check if the current week has already been processed
	alreadyProcessed, err := v.hasProcessedCurrentWeek(today)
	if err != nil {
		fmt.Println(err)
	}

	// Only allow updates on Sunday or Monday, or if not already processed
	if alreadyProcessed && !(today.Weekday() == time.Sunday || today.Weekday() == time.Monday) {
		fmt.Println("Services for this week have already been processed, skipping computation.")
		return true, nil
	}

	// Get stops data
	stopsData, err := v.GetStops()
	if err != nil {
		return false, err
	}

	// Get the end date from the feed
	endDate, err := v.FeedEndDate()
	if err != nil {
		return false, err
	}

	// Get the next 7 days or up to the endDate, whichever is earlier
	dates := getNext7Days(today, endDate)

	// Prepare to cache the services
	createTableIfNotExists(v.db, "services_cache", []string{"stop_id", "service_data", "date"})

	tx, err := v.db.Begin() // Start transaction for better performance
	if err != nil {
		return false, fmt.Errorf("error starting transaction: %v", err)
	}

	// Loop over stops and dates, and store the services
	for index, stop := range stopsData {
		for _, date := range dates {
			servicesData, err := v.GetServicesAtStop(stop.StopId, 0, 22, date)
			if err == nil {
				stringData, err := json.Marshal(servicesData)
				if err == nil {
					insertRecord(tx, "services_cache", []CSVRecord{
						{Header: "stop_id", Data: stop.StopId},
						{Header: "service_data", Data: string(stringData)},
						{Header: "date", Data: date},
					})
				}
			}
		}
		fmt.Printf("Processed stop %d/%d\n", index+1, len(stopsData))
	}

	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("error committing transaction: %v", err)
	}

	// Mark the current week as processed
	err = v.markWeekAsProcessed(today)
	if err != nil {
		return false, err
	}

	fmt.Println("Precomputed services for the next 7 days")
	return true, nil
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
