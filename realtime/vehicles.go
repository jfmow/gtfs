package realtime

import (
	"errors"
	"sync"
	"time"

	"github.com/jfmow/gtfs/realtime/proto"
)

type vehiclesCache struct {
	mu          sync.Mutex
	data        VehiclesMap
	lastUpdated time.Time
}

type VehiclesMap map[string]*proto.VehiclePosition

func (v Realtime) GetVehicles() (VehiclesMap, error) {

	v.vehiclesCache.mu.Lock()
	defer v.vehiclesCache.mu.Unlock()

	if len(v.vehiclesCache.data) >= 1 && v.vehiclesCache.lastUpdated.Add(v.refreshPeriod).After(time.Now()) {
		return v.vehiclesCache.data, nil
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

	v.vehiclesCache.data = vehicles
	v.vehiclesCache.lastUpdated = time.Now()

	return vehicles, nil
}

func (vehicles VehiclesMap) ByTripID(tripID string) (*proto.VehiclePosition, error) {
	vehicle, found := vehicles[tripID]
	if !found {
		return nil, errors.New("no vehicle found for trip id")
	}
	return vehicle, nil
}
