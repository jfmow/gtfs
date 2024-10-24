package realtime

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

var (
	vehiclesClient          = &http.Client{}
	vehiclesApiRequestMutex sync.Mutex
)

var (
	cachedVehiclesData       VehiclesMap
	lastUpdatedVehiclesCache time.Time
)

type VehiclesMap map[string]Vehicle

func (v vehicles) GetVehicles() (VehiclesMap, error) {
	vehiclesApiRequestMutex.Lock()
	defer vehiclesApiRequestMutex.Unlock()
	if len(cachedVehiclesData) >= 1 && lastUpdatedVehiclesCache.Add(15*time.Second).After(time.Now()) {
		return cachedVehiclesData, nil
	}

	url := v.url
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("error creating request: %w", err)
	}
	req.Header.Set("Cache-Control", "no-cache")
	if v.apiHeader != "" {
		req.Header.Set(v.apiHeader, v.apiKey)
	}

	resp, err := vehiclesClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error making request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading response body: %w", err)
	}

	var vehicles VehiclesMap

	// Try parsing the response assuming the "status" field exists
	var resultWithStatus struct {
		Status   string `json:"status"`
		Response struct {
			Header struct {
				Timestamp           float64 `json:"timestamp"`
				GtfsRealtimeVersion string  `json:"gtfs_realtime_version"`
				Incrementality      int64   `json:"incrementality"`
			} `json:"header"`
			Entity []struct {
				ID        string  `json:"id"`
				Vehicle   Vehicle `json:"vehicle"`
				IsDeleted bool    `json:"is_deleted"`
			} `json:"entity"`
		} `json:"response"`
	}

	err = json.Unmarshal(body, &resultWithStatus)
	if err == nil && resultWithStatus.Status == "OK" {
		// Successfully parsed the response with status
		vehicles = make(VehiclesMap)
		for _, entity := range resultWithStatus.Response.Entity {
			vehicles[entity.Vehicle.Trip.TripID] = entity.Vehicle
		}
	} else {
		// If parsing with status fails, try parsing directly to header/entity structure
		var resultWithoutStatus struct {
			Header struct {
				Timestamp           float64 `json:"timestamp"`
				GtfsRealtimeVersion string  `json:"gtfs_realtime_version"`
				Incrementality      int64   `json:"incrementality"`
			} `json:"header"`
			Entity []struct {
				ID        string  `json:"id"`
				Vehicle   Vehicle `json:"vehicle"`
				IsDeleted bool    `json:"is_deleted"`
			} `json:"entity"`
		}

		err = json.Unmarshal(body, &resultWithoutStatus)
		if err != nil {
			return nil, fmt.Errorf("error parsing both response formats: %w", err)
		}

		// Populate vehicles from the header/entity response (no status)
		vehicles = make(VehiclesMap)
		for _, entity := range resultWithoutStatus.Entity {
			vehicles[entity.Vehicle.Trip.TripID] = entity.Vehicle
		}
	}

	// Cache the result
	cachedVehiclesData = vehicles
	lastUpdatedVehiclesCache = time.Now()

	return vehicles, nil
}

func (vehicles VehiclesMap) GetVehicleByTripID(tripID string) (Vehicle, error) {
	vehicle, found := vehicles[tripID]
	if !found {
		return Vehicle{}, errors.New("no vehicle found for trip id")
	}
	return vehicle, nil
}

//Structs

type VehicleResponse struct {
	Status   string `json:"status"`
	Response struct {
		Header struct {
			Timestamp           float64 `json:"timestamp"`
			GtfsRealtimeVersion string  `json:"gtfs_realtime_version"`
			Incrementality      int64   `json:"incrementality"`
		} `json:"header"`
		Entity []struct {
			ID        string  `json:"id"`
			Vehicle   Vehicle `json:"vehicle"`
			IsDeleted bool    `json:"is_deleted"`
		} `json:"entity"`
	} `json:"response"`
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
}
