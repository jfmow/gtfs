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

	// Units reporting a position but no trip; see GetUntrippedVehicles.
	untripped []*proto.VehiclePosition

	// Inferred from each poll; see Consists.
	consists     Consists
	consistPairs map[string]*consistPair
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
	var untripped []*proto.VehiclePosition

	for _, i := range result {
		if i.GetVehicle() == nil {
			continue // a combined feed's trip updates/alerts
		}
		tripId := i.GetVehicle().GetTrip().GetTripId()
		if tripId == "" {
			pos := i.GetVehicle().GetPosition()
			if pos != nil && (pos.GetLatitude() != 0 || pos.GetLongitude() != 0) {
				untripped = append(untripped, i.GetVehicle())
			}
		}
		vehicles[tripId] = i.GetVehicle()
	}

	v.vehiclesCache.data = vehicles
	v.vehiclesCache.untripped = untripped
	v.vehiclesCache.lastUpdated = fetchedAt
	v.updateConsists(result, fetchedAt)
	v.addVehicleHistory(vehicles, fetchedAt)

	return vehicles, nil
}

// GetUntrippedVehicles returns the units in the latest vehicle feed that report
// a position but no trip - VehiclesMap is keyed by trip id, so it can't hold
// them. These are stabled or out-of-service vehicles, the trailing unit of a
// coupled train (see GetConsists), or vehicles the operator simply isn't
// publishing a trip for.
func (v Realtime) GetUntrippedVehicles() ([]*proto.VehiclePosition, error) {
	if _, err := v.GetVehicles(); err != nil {
		return nil, err
	}

	v.vehiclesCache.mu.Lock()
	defer v.vehiclesCache.mu.Unlock()

	result := make([]*proto.VehiclePosition, len(v.vehiclesCache.untripped))
	copy(result, v.vehiclesCache.untripped)
	return result, nil
}

func (vehicles VehiclesMap) ByTripID(tripID string) (*proto.VehiclePosition, error) {
	vehicle, found := vehicles[tripID]
	if !found {
		return nil, errors.New("no vehicle found for trip id")
	}
	return vehicle, nil
}
