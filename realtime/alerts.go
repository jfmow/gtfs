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

	if len(v.alertsCache.data) >= 1 && v.alertsCache.lastUpdated.Add(v.refreshPeriod).After(time.Now()) {
		return v.alertsCache.data, nil
	}

	result, err := fetchProto(v.alertsUrl, v.apiHeader, v.apiKey)
	if err != nil {
		return nil, err
	}

	var alerts AlertMap = make(AlertMap)

	for _, i := range result {
		alerts[i.GetId()] = i.Alert
	}

	v.alertsCache.data = alerts
	v.alertsCache.lastUpdated = time.Now()

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
