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
	alertClient          = &http.Client{}
	alertApiRequestMutex sync.Mutex
)

var (
	cachedAlertsData       AlertMap
	lastUpdatedAlertsCache time.Time
)

type AlertMap []Alert

func (v alerts) GetAlerts() (AlertMap, error) {
	alertApiRequestMutex.Lock()
	defer alertApiRequestMutex.Unlock()
	if len(cachedAlertsData) >= 1 && lastUpdatedAlertsCache.Add(15*time.Second).After(time.Now()) {
		return cachedAlertsData, nil
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

	resp, err := alertClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error making request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading response body: %w", err)
	}

	var result alertResponse
	err = json.Unmarshal(body, &result)
	if err != nil {
		return nil, fmt.Errorf("error parsing JSON: %w", err)
	}

	var alerts AlertMap

	for _, i := range result.Response.Entity {
		alerts = append(alerts, i.Alert)
	}

	cachedAlertsData = alerts
	lastUpdatedAlertsCache = time.Now()

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

type alertResponse struct {
	Status   string `json:"status"`
	Response struct {
		Header struct {
			Timestamp           float64 `json:"timestamp"`
			GtfsRealtimeVersion string  `json:"gtfs_realtime_version"`
			Incrementality      int64   `json:"incrementality"`
		} `json:"header"`
		Entity []struct {
			ID        string `json:"id"`
			Alert     Alert  `json:"alert"`
			Timestamp string `json:"timestamp"`
		} `json:"entity"`
	} `json:"response"`
}

type Alert struct {
	ActivePeriod    []ActivePeriod   `json:"active_period"`
	InformedEntity  []InformedEntity `json:"informed_entity"`
	Cause           string           `json:"cause"`
	Effect          string           `json:"effect"`
	HeaderText      Text             `json:"header_text"`
	DescriptionText Text             `json:"description_text"`
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
