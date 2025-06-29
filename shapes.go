package gtfs

import (
	"errors"
)

// Shape represents a GTFS shape point
type Shape struct {
	ShapeID     string  `json:"shape_id"`
	Coordinates []Point `json:"coordinates"`
}

// Point represents a single latitude/longitude coordinate
type Point struct {
	Lat, Lon     float64
	DistTraveled float64
}

// GetShapeByID retrieves the shape points for a given trip_id
func (v Database) GetShapeByTripID(tripID string) (Shape, error) {
	db := v.db

	query := `
		SELECT s.shape_id, s.shape_pt_lat, s.shape_pt_lon, s.shape_dist_traveled
		FROM shapes s
		INNER JOIN trips t ON s.shape_id = t.shape_id
		WHERE t.trip_id = ?
		ORDER BY s.shape_pt_sequence
	`

	rows, err := db.Query(query, tripID)
	if err != nil {
		return Shape{}, err
	}
	defer rows.Close()

	var shape Shape
	shape.Coordinates = []Point{}

	for rows.Next() {
		var point Point
		err := rows.Scan(&shape.ShapeID, &point.Lat, &point.Lon, &point.DistTraveled)
		if err != nil {
			return Shape{}, err
		}
		shape.Coordinates = append(shape.Coordinates, point)
	}

	if len(shape.Coordinates) == 0 {
		return Shape{}, errors.New("no shape found with id")
	}

	return shape, nil
}

// GetShapeByID retrieves the shape points for a given shape_id
func (v Database) GetShapeByID(shapeID string) (Shape, error) {
	db := v.db

	query := `
		SELECT shape_id, shape_pt_lat, shape_pt_lon
		FROM shapes
		WHERE shape_id = ?
		ORDER BY shape_pt_sequence
	`

	rows, err := db.Query(query, shapeID)
	if err != nil {
		return Shape{}, err
	}
	defer rows.Close()

	var shape Shape
	shape.ShapeID = shapeID
	shape.Coordinates = []Point{}

	for rows.Next() {
		var point Point
		err := rows.Scan(&shape.ShapeID, &point.Lat, &point.Lon)
		if err != nil {
			return Shape{}, err
		}
		shape.Coordinates = append(shape.Coordinates, point)
	}

	if len(shape.Coordinates) == 0 {
		return Shape{}, errors.New("no shape found with id")
	}

	return shape, nil
}

// ToGeoJSON converts a Shape to a GeoJSON LineString
func (s Shape) ToGeoJSON() (map[string]interface{}, error) {
	// Build the 3‑dimensional coordinate array.
	coords := make([][]float64, len(s.Coordinates))
	for i, p := range s.Coordinates {
		coords[i] = []float64{p.Lon, p.Lat, p.DistTraveled}
	}

	geoJSON := map[string]interface{}{
		"type": "Feature",
		"geometry": map[string]interface{}{
			"type":        "LineString",
			"coordinates": coords,
		},
		"properties": map[string]interface{}{
			"shape_id": s.ShapeID,
		},
	}

	return geoJSON, nil
}

// Helper function to convert Shape coordinates into GeoJSON format
func (s Shape) toCoordinatesArray() [][]float64 {
	coords := [][]float64{}
	for _, point := range s.Coordinates {
		coords = append(coords, []float64{point.Lon, point.Lat})
	}
	return coords
}
