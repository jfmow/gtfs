package realtime

import (
	"errors"
	"sync"
	"time"
)

var (
	alertApiRequestMutex sync.Mutex
)

var (
	cachedAlertsData       map[string]AlertMap  = make(map[string]AlertMap)
	lastUpdatedAlertsCache map[string]time.Time = make(map[string]time.Time)
)

type AlertMap []Alert

func (v Realtime) GetAlerts() (AlertMap, error) {
	alertApiRequestMutex.Lock()
	defer alertApiRequestMutex.Unlock()
	if cachedAlertsData[v.uuid] != nil && len(cachedAlertsData[v.uuid]) >= 1 && lastUpdatedAlertsCache[v.uuid].Add(v.refreshPeriod).After(time.Now()) {
		return cachedAlertsData[v.uuid], nil
	}

	result, err := fetchData[[]struct {
		ID        string `json:"id"`
		Alert     Alert  `json:"alert"`
		Timestamp string `json:"timestamp"`
	}](v.alertsUrl, v.apiHeader, v.apiKey)
	if err != nil {
		return nil, err
	}

	var alerts AlertMap

	for _, i := range result {
		i.Alert.ID = i.ID
		alerts = append(alerts, i.Alert)
	}

	cachedAlertsData[v.uuid] = alerts
	lastUpdatedAlertsCache[v.uuid] = time.Now()

	return alerts, nil
}

func (alerts AlertMap) FindAlertsByRouteId(routeId string) (AlertMap, error) {
	var sorted AlertMap
	for _, i := range alerts {
		for _, b := range i.InformedEntity {
			if (string)(b.RouteID) == routeId || b.StopID == routeId {
				sorted = append(sorted, i)
				break
			}
		}
	}
	if len(sorted) == 0 {
		return AlertMap{}, errors.New("no alerts found for route/stop")
	}
	return sorted, nil
}

type Alert struct {
	ActivePeriod    []ActivePeriod   `json:"active_period"`
	InformedEntity  []InformedEntity `json:"informed_entity"`
	Cause           string           `json:"cause"`
	Effect          string           `json:"effect"`
	HeaderText      Text             `json:"header_text"`
	DescriptionText Text             `json:"description_text"`
	ID              string           `json:"alert_id"`
}

type ActivePeriod struct {
	Start int64 `json:"start"`
	End   int64 `json:"end"`
}

type Text struct {
	Translation []Translation `json:"translation"`
}

type Translation struct {
	Text     string `json:"text"`
	Language string `json:"language"`
}

type InformedEntity struct {
	StopID  string  `json:"stop_id"`
	RouteID RouteID `json:"route_id"`
}
