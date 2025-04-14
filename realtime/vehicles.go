package realtime

import (
	"errors"
	"sync"
	"time"

	"github.com/jfmow/gtfs/realtime/proto"
)

var (
	vehiclesApiRequestMutex sync.Mutex
)

var (
	cachedVehiclesData       map[string]VehiclesMap = make(map[string]VehiclesMap)
	lastUpdatedVehiclesCache map[string]time.Time   = make(map[string]time.Time)
)

type VehiclesMap map[string]*proto.VehiclePosition

func (v Realtime) GetVehicles() (VehiclesMap, error) {
	vehiclesApiRequestMutex.Lock()
	defer vehiclesApiRequestMutex.Unlock()
	if cachedVehiclesData[v.uuid] != nil && len(cachedVehiclesData[v.uuid]) >= 1 && lastUpdatedVehiclesCache[v.uuid].Add(v.refreshPeriod).After(time.Now()) {
		return cachedVehiclesData[v.uuid], nil
	}

	result, err := fetchProto(v.vehiclesUrl, v.apiHeader, v.apiKey)
	if err != nil {
		return nil, err
	}

	var vehicles = make(VehiclesMap)

	for _, i := range result {
		tripId := i.GetVehicle().GetTrip().GetTripId()
		vehicles[tripId] = i.GetVehicle()
	}

	cachedVehiclesData[v.uuid] = vehicles
	lastUpdatedVehiclesCache[v.uuid] = time.Now()

	return vehicles, nil
}

func (vehicles VehiclesMap) ByTripID(tripID string) (*proto.VehiclePosition, error) {
	vehicle, found := vehicles[tripID]
	if !found {
		return nil, errors.New("no vehicle found for trip id")
	}
	return vehicle, nil
}
