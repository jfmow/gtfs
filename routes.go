package gtfs
import (
	"errors"

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
		case 3:
			route.VehicleType = "bus"
		case 2:
			route.VehicleType = "train"
		case 4:
			route.VehicleType = "ferry"
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
	case 3:
		route.VehicleType = "bus"
	case 2:
		route.VehicleType = "train"
	case 4:
		route.VehicleType = "ferry"
	}

	return route, nil
}