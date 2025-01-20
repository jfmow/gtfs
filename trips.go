package gtfs

import (
	"errors"

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
