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

	result, fetchedAt, err := v.vehiclesFeed.get(time.Now())
	if err != nil {
		return nil, err
	}
	if v.vehiclesCache.data != nil && fetchedAt.Equal(v.vehiclesCache.lastUpdated) {
		return v.vehiclesCache.data, nil // nothing newer from the feed
	}

	var vehicles = make(VehiclesMap)

	for _, i := range result {
		if i.GetVehicle() == nil {
			continue // a combined feed's trip updates/alerts
		}
		tripId := i.GetVehicle().GetTrip().GetTripId()
		vehicles[tripId] = i.GetVehicle()
	}

	v.vehiclesCache.data = vehicles
	v.vehiclesCache.lastUpdated = fetchedAt
	v.addVehicleHistory(vehicles, fetchedAt)

	return vehicles, nil
}

func (vehicles VehiclesMap) ByTripID(tripID string) (*proto.VehiclePosition, error) {
	vehicle, found := vehicles[tripID]
	if !found {
		return nil, errors.New("no vehicle found for trip id")
	}
	return vehicle, nil
}
