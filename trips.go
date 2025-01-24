package gtfs

import (
	"errors"
	"fmt"

	sq "github.com/Masterminds/squirrel"
)

type Trip struct {
	BikesAllowed         int    `json:"bikes_allowed"`
	DirectionID          int    `json:"direction_id"`
	RouteID              string `json:"route_id"`
	ServiceID            string `json:"service_id"`
	ShapeID              string `json:"shape_id"`
	TripHeadsign         string `json:"trip_headsign"`
	TripID               string `json:"trip_id"`
	WheelchairAccessible int    `json:"wheelchair_accessible"`
}

func (v Database) GetTripByID(tripID string) (Trip, error) {
	db := v.db
	baseQuery := sq.Select("trip_id", "route_id", "trip_headsign", "shape_id", "service_id", "direction_id", "wheelchair_accessible", "bikes_allowed").From("trips")

	active := baseQuery.Where(sq.Eq{"trip_id": tripID})

	row := active.RunWith(db).QueryRow()

	var trip Trip

	err := row.Scan(
		&trip.TripID,
		&trip.RouteID,
		&trip.TripHeadsign,
		&trip.ShapeID,
		&trip.ServiceID,
		&trip.DirectionID,
		&trip.WheelchairAccessible,
		&trip.BikesAllowed,
	)

	if err != nil {
		return Trip{}, errors.New("no trip found with id")
	}

	return trip, nil
}

// Get the stopId's of the stops a trip stops at
func (v Database) GetServicesStopsByTripId(tripId string) ([]string, error) {
	query := `
		SELECT stop_id FROM stop_times WHERE trip_id = ?
	`

	rows, err := v.db.Query(query, tripId)
	if err != nil {
		fmt.Println(err)
		return nil, errors.New("problem querying db")
	}

	defer rows.Close()

	var stops []string

	for rows.Next() {
		var stopId string

		err := rows.Scan(
			&stopId,
		)
		if err != nil {
			fmt.Println(err)
			return nil, errors.New("unable to scan row")
		}

		var allStops Stops

		parentStops, err := v.GetParentStopsByChildStopID(stopId)
		if err != nil {
			return nil, errors.New("invalid stop id")
		}
		allStops = append(allStops, parentStops...)

		for _, stop := range allStops {
			stops = append(stops, stop.StopId)
		}

	}

	if len(stops) == 0 {
		return nil, errors.New("no stops found")
	}

	return stops, nil
}
