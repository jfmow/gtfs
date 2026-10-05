package realtime

import (
	"errors"
	"sync"
	"time"

	"github.com/jfmow/gtfs/realtime/proto"
)

type alertsCache struct {
	mu          sync.Mutex
	data        AlertMap
	lastUpdated time.Time
}

type AlertMap map[string]*proto.Alert
type AlertSlice []*proto.Alert
type Alert *proto.Alert

func (v Realtime) GetAlerts() (AlertMap, error) {
	v.alertsCache.mu.Lock()
	defer v.alertsCache.mu.Unlock()

	result, fetchedAt, err := v.alertsFeed.get(time.Now())
	if err != nil {
		return nil, err
	}
	if v.alertsCache.data != nil && fetchedAt.Equal(v.alertsCache.lastUpdated) {
		return v.alertsCache.data, nil // nothing newer from the feed
	}

	var alerts AlertMap = make(AlertMap)

	for _, i := range result {
		if i.GetAlert() == nil {
			continue // a combined feed's vehicles/trip updates
		}
		alerts[i.GetId()] = i.Alert
	}

	v.alertsCache.data = alerts
	v.alertsCache.lastUpdated = fetchedAt

	return alerts, nil
}

func (alerts AlertMap) FindAlertsByRouteId(routeId string) (AlertMap, error) {
	var sorted AlertMap = make(AlertMap)
	for alertId, i := range alerts {
		for _, b := range i.GetInformedEntity() {
			if (string)(b.GetRouteId()) == routeId || b.GetStopId() == routeId {
				sorted[alertId] = i
				break
			}
		}
	}
	if len(sorted) == 0 {
		return AlertMap{}, errors.New("no alerts found for route/stop")
	}
	return sorted, nil
}
