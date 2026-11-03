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
	// Trips more than one unit claims, with every claimant in feed order.
	// Operators leave a parked bus signed on to a trip while another bus
	// runs it, so the same trip id can arrive twice.
	contested := make(map[string][]*proto.VehiclePosition)

	for _, i := range result {
		vp := i.GetVehicle()
		if vp == nil {
			continue // a combined feed's trip updates/alerts
		}
		tripId := vp.GetTrip().GetTripId()
		if tripId == "" {
			pos := vp.GetPosition()
			if pos != nil && (pos.GetLatitude() != 0 || pos.GetLongitude() != 0) {
				untripped = append(untripped, vp)
			}
			vehicles[tripId] = vp
			continue
		}
		existing, ok := vehicles[tripId]
		if !ok {
			vehicles[tripId] = vp
			continue
		}
		if _, seen := contested[tripId]; !seen {
			contested[tripId] = []*proto.VehiclePosition{existing}
		}
		contested[tripId] = append(contested[tripId], vp)
	}

	if len(contested) > 0 {
		assigned := v.tripUpdateVehicleIDs(contested)
		for tripId, claimants := range contested {
			vehicles[tripId] = pickTripVehicle(claimants, assigned[tripId])
		}
	}

	v.vehiclesCache.data = vehicles
	v.vehiclesCache.untripped = untripped
	v.vehiclesCache.lastUpdated = fetchedAt
	v.updateConsists(result, fetchedAt)
	v.addVehicleHistory(vehicles, fetchedAt)

	return vehicles, nil
}

// tripUpdateVehicleIDs returns, for each contested trip, the vehicle id its
// trip update says is running it - the operator's own assignment, which a
// stale sign-on on another unit doesn't change. Trips whose update names no
// vehicle (or that have no update) are left out.
func (v Realtime) tripUpdateVehicleIDs(contested map[string][]*proto.VehiclePosition) map[string]string {
	assigned := make(map[string]string)
	if v.tripUpdatesFeed == nil {
		return assigned
	}
	updates, err := v.GetTripUpdates()
	if err != nil {
		return assigned
	}
	for tripId := range contested {
		if id := updates[tripId].GetVehicle().GetId(); id != "" {
			assigned[tripId] = id
		}
	}
	return assigned
}

// pickTripVehicle chooses which of the units claiming one trip is running it:
// the unit the trip update names, else the one that reported most recently,
// else the first in the feed.
func pickTripVehicle(claimants []*proto.VehiclePosition, assignedID string) *proto.VehiclePosition {
	if assignedID != "" {
		for _, vp := range claimants {
			if vp.GetVehicle().GetId() == assignedID {
				return vp
			}
		}
	}
	best := claimants[0]
	for _, vp := range claimants[1:] {
		if vp.GetTimestamp() > best.GetTimestamp() {
			best = vp
		}
	}
	return best
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
