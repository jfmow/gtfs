package realtime

import (
	"errors"
	"sync"
	"time"

	"github.com/jfmow/gtfs/realtime/proto"
)

var (
	alertApiRequestMutex sync.Mutex
)

var (
	cachedAlertsData       map[string]AlertMap  = make(map[string]AlertMap)
	lastUpdatedAlertsCache map[string]time.Time = make(map[string]time.Time)
)

type AlertMap map[string]*proto.Alert
type AlertSlice []*proto.Alert
type Alert *proto.Alert

func (v Realtime) GetAlerts() (AlertMap, error) {
	alertApiRequestMutex.Lock()
	defer alertApiRequestMutex.Unlock()
	if cachedAlertsData[v.uuid] != nil && len(cachedAlertsData[v.uuid]) >= 1 && lastUpdatedAlertsCache[v.uuid].Add(v.refreshPeriod).After(time.Now()) {
		return cachedAlertsData[v.uuid], nil
	}

	result, err := fetchProto(v.alertsUrl, v.apiHeader, v.apiKey)
	if err != nil {
		return nil, err
	}

	var alerts AlertMap = make(AlertMap)

	for _, i := range result {
		alerts[i.GetId()] = i.Alert
	}

	cachedAlertsData[v.uuid] = alerts
	lastUpdatedAlertsCache[v.uuid] = time.Now()

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
