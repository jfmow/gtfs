package gtfs

import (
	"errors"
	"strings"
)

type Route struct {
	RouteId        string `json:"route_id"`
	AgencyId       string `json:"agency_id"`
	RouteShortName string `json:"route_short_name"`
	RouteLongName  string `json:"route_long_name"`
	RouteType      int    `json:"route_type"`
	RouteColor     string `json:"route_color"`
	VehicleType    string `json:"vehicle_type"`
}
type RouteId string

/*
Get all the stored routes
*/
func (v Database) GetRoutes() ([]Route, error) {
	db := v.db
	query := `
		SELECT 
			route_id,
			agency_id,
			route_short_name,
			route_long_name,
			route_type,
			route_color
		FROM
			routes
	`

	rows, err := db.Query(query)

	if err != nil {
		return nil, err
	}

	defer rows.Close() // Ensure the rows are closed after usage

	// Map to hold all routes, initialize as empty
	var routes []Route

	for rows.Next() {
		var route Route
		// Scan the row data into the trip struct
		err := rows.Scan(
			&route.RouteId,
			&route.AgencyId,
			&route.RouteShortName,
			&route.RouteLongName,
			&route.RouteType,
			&route.RouteColor,
		)
		if err != nil {
			return nil, err
		}

		route.VehicleType = getRouteVehicleType(route)

		routes = append(routes, route)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	if len(routes) == 0 {
		return nil, errors.New("no routes found")
	}

	return routes, nil
}

/*
Get a route by its route ids
*/
func (v Database) GetRouteByID(routeID string) (Route, error) {
	db := v.db
	query := `
		SELECT
			route_id,
			agency_id,
			route_short_name,
			route_long_name,
			route_type,
			route_color
		FROM
			routes
		WHERE
			route_id = ?
	`

	row := db.QueryRow(query, routeID)

	// Slice to hold all the trips
	var route Route

	// Iterate over the rows
	err := row.Scan(
		&route.RouteId,
		&route.AgencyId,
		&route.RouteShortName,
		&route.RouteLongName,
		&route.RouteType,
		&route.RouteColor,
	)
	if err != nil {
		return Route{}, err
	}

	route.VehicleType = getRouteVehicleType(route)

	return route, nil
}

/*
Get all the routes that pass through a given stops
*/
func (v Database) GetRoutesByStopId(stopId string) ([]Route, error) {
	query := `
		SELECT DISTINCT r.route_id, r.route_short_name, r.route_long_name, r.route_type, r.route_color
		FROM stop_times st
		JOIN trips t ON st.trip_id = t.trip_id
		JOIN routes r ON t.route_id = r.route_id
		WHERE st.stop_id = ?;
	`
	db := v.db

	rows, err := db.Query(query, stopId)
	if err != nil {
		return nil, errors.New("no routes found for stop")
	}

	var routes []Route
	defer rows.Close()

	for rows.Next() {
		var route Route
		// Scan the row data into the trip struct
		err := rows.Scan(
			&route.RouteId,
			&route.RouteShortName,
			&route.RouteLongName,
			&route.RouteType,
			&route.RouteColor,
		)
		if err != nil {
			return nil, err
		}
		route.VehicleType = getRouteVehicleType(route)
		// Append each trip to the slice
		routes = append(routes, route)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	if len(routes) == 0 {
		return nil, errors.New("no routes found")
	}
	return routes, nil
}

/*
Determine the type of vehicle a given route uses
*/
func getRouteVehicleType(route Route) string {
	switch route.RouteType {
	case 0:
		return "Tram/Light Rail"
	case 1:
		return "Subway/Metro"
	case 2:
		return "Train"
	case 3:
		return "Bus"
	case 4:
		return "Ferry"
	case 5:
		return "Cable Tram"
	case 6:
		return "Gondola"
	case 7:
		return "Train (up hill)"
	case 11:
		return "Trolleybus"
	case 12:
		return "Monorail"
	}
	return "unknown"
}

/*
Search for a route based on a partial match to its id
*/
func (v Database) SearchForRouteByID(searchText string) ([]Route, error) {
	// Normalize the input search text and make it lowercase
	normalizedSearchText := strings.ToLower(searchText)

	// Create a SQL query to find matching stops
	query := `
		SELECT 
			route_id,
			agency_id,
			route_short_name,
			route_long_name,
			route_type,
			route_color
		FROM 
			routes
		WHERE
			LOWER(route_id) LIKE ?
	`

	// Run the query
	rows, err := v.db.Query(query, "%"+normalizedSearchText+"%")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var routeSearchResults []Route

	// Iterate over the rows
	for rows.Next() {
		var route Route
		err := rows.Scan(
			&route.RouteId,
			&route.AgencyId,
			&route.RouteShortName,
			&route.RouteLongName,
			&route.RouteType,
			&route.RouteColor,
		)
		if err != nil {
			return nil, err
		}
		switch route.RouteType {
		case 0:
			route.VehicleType = "Tram/Light Rail"
		case 1:
			route.VehicleType = "Subway/Metro"
		case 2:
			route.VehicleType = "Train"
		case 3:
			route.VehicleType = "Bus"
		case 4:
			route.VehicleType = "Ferry"
		case 5:
			route.VehicleType = "Cable Tram"
		case 6:
			route.VehicleType = "Gondola"
		case 7:
			route.VehicleType = "Train (up hill)"
		case 11:
			route.VehicleType = "Trolleybus"
		case 12:
			route.VehicleType = "Monorail"
		}
		routeSearchResults = append(routeSearchResults, route)
	}

	// Check for any error encountered during iteration
	if err = rows.Err(); err != nil {
		return nil, err
	}

	if len(routeSearchResults) == 0 {
		return nil, errors.New("no routes found for search")
	}

	return routeSearchResults, nil
}
