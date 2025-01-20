package gtfs

import (
	"errors"
	"strings"

	sq "github.com/Masterminds/squirrel"
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

func (v Database) GetRoutes() ([]Route, error) {
	db := v.db
	baseQuery := sq.Select("route_id", "agency_id", "route_short_name", "route_long_name", "route_type", "route_color").From("routes")

	rows, err := baseQuery.RunWith(db).Query()

	if err != nil {
		return nil, err
	}

	defer rows.Close() // Ensure the rows are closed after usage

	// Slice to hold all the trips
	var routes []Route

	// Iterate over the rows
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
		// Append each trip to the slice
		routes = append(routes, route)
	}

	// Check for any error encountered during iteration
	if err = rows.Err(); err != nil {
		return nil, err
	}

	// If no trips were found, return a custom error
	if len(routes) == 0 {
		return nil, errors.New("no routes found")
	}

	return routes, nil
}

func (v Database) GetRouteByID(routeID string) (Route, error) {
	db := v.db
	baseQuery := sq.Select("route_id", "agency_id", "route_short_name", "route_long_name", "route_type", "route_color").From("routes").
		Where(sq.Eq{"route_id": routeID})

	row := baseQuery.RunWith(db).QueryRow()

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

	return route, nil
}

type RouteSearch struct {
	RouteID        string `json:"route_id"`
	RouteLongName  string `json:"route_long_name"`
	RouteShortName string `json:"route_short_name"`
}

func (v Database) SearchForRouteByID(searchText string) ([]RouteSearch, error) {
	// Normalize the input search text and make it lowercase
	normalizedSearchText := strings.ToLower(searchText)

	// Create a SQL query to find matching stops
	query := sq.Select("route_id", "route_short_name", "route_long_name")
	query = query.From("routes")
	query = query.Where(sq.Like{"LOWER(route_id)": "%" + normalizedSearchText + "%"})

	// Run the query
	rows, err := query.RunWith(v.db).Query()
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var routeSearchResults []RouteSearch

	// Iterate over the rows
	for rows.Next() {
		var route Route
		err := rows.Scan(&route.RouteId, &route.RouteShortName, &route.RouteLongName)
		if err != nil {
			return nil, err
		}
		routeSearchResults = append(routeSearchResults, RouteSearch{RouteID: route.RouteId, RouteLongName: route.RouteLongName, RouteShortName: route.RouteShortName})
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
