package realtime

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"
)

var (
	vehiclesApiRequestMutex sync.Mutex
)

var (
	cachedVehiclesData       map[string]VehiclesMap = make(map[string]VehiclesMap)
	lastUpdatedVehiclesCache map[string]time.Time   = make(map[string]time.Time)
)

type VehiclesMap map[string]Vehicle

func (v Realtime) GetVehicles() (VehiclesMap, error) {
	vehiclesApiRequestMutex.Lock()
	defer vehiclesApiRequestMutex.Unlock()
	if cachedVehiclesData[v.uuid] != nil && len(cachedVehiclesData[v.uuid]) >= 1 && lastUpdatedVehiclesCache[v.uuid].Add(v.refreshPeriod).After(time.Now()) {
		return cachedVehiclesData[v.uuid], nil
	}

	result, err := fetchData[[]struct {
		ID        string  `json:"id"`
		Vehicle   Vehicle `json:"vehicle"`
		IsDeleted bool    `json:"is_deleted"`
	}](v.vehiclesUrl, v.apiHeader, v.apiKey)
	if err != nil {
		return nil, err
	}

	var vehicles = make(VehiclesMap)

	for _, i := range result {
		vehicles[i.Vehicle.Trip.TripID] = i.Vehicle
	}

	cachedVehiclesData[v.uuid] = vehicles
	lastUpdatedVehiclesCache[v.uuid] = time.Now()

	return vehicles, nil
}

func (vehicles VehiclesMap) ByTripID(tripID string) (Vehicle, error) {
	vehicle, found := vehicles[tripID]
	if !found {
		return Vehicle{}, errors.New("no vehicle found for trip id")
	}
	return vehicle, nil
}

type Vehicle struct {
	Trip            VehicleTrip    `json:"trip"`
	Position        Position       `json:"position"`
	Timestamp       int64          `json:"timestamp"`
	Vehicle         VehicleVehicle `json:"vehicle"`
	OccupancyStatus int            `json:"occupancy_status"`
}

type Position struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Speed     float64 `json:"speed"`
}

type VehicleTrip struct {
	TripID               string  `json:"trip_id"`
	StartTime            string  `json:"start_time"`
	StartDate            string  `json:"start_date"`
	ScheduleRelationship int64   `json:"schedule_relationship"`
	RouteID              RouteID `json:"route_id"`
}

type RouteID string

// UnmarshalJSON allows RouteID to accept both string and numeric values
func (r *RouteID) UnmarshalJSON(data []byte) error {
	// Attempt to unmarshal as a string
	var str string
	if err := json.Unmarshal(data, &str); err == nil {
		*r = RouteID(str)
		return nil
	}

	// Attempt to unmarshal as a number and convert it to string
	var num float64
	if err := json.Unmarshal(data, &num); err == nil {
		*r = RouteID(fmt.Sprintf("%.0f", num)) // Convert float to string
		return nil
	}

	// If neither works, return an error
	return fmt.Errorf("cannot unmarshal %s into RouteID", string(data))
}

type VehicleVehicle struct {
	ID           string `json:"id"`
	Label        string `json:"label"`
	LicensePlate string `json:"license_plate"`
	Type         string `json:"type"` //Blank always
}
