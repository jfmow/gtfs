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
	cachedAlertsData       map[string]AlertMap  = make(map[string]AlertMap)
	lastUpdatedAlertsCache map[string]time.Time = make(map[string]time.Time)
)

type AlertMap []Alert

func (v alerts) GetAlerts() (AlertMap, error) {
	alertApiRequestMutex.Lock()
	defer alertApiRequestMutex.Unlock()
	if cachedAlertsData[v.name] != nil && len(cachedAlertsData[v.name]) >= 1 && lastUpdatedAlertsCache[v.name].Add(v.refreshPeriod).After(time.Now()) {
		return cachedAlertsData[v.name], nil
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

	// Check if Status is present
	if result.Status != nil {
		// Handle case where Status and Response are present
		if result.Response != nil {
			for _, i := range result.Response.Entity {
				i.Alert.ID = i.ID
				alerts = append(alerts, i.Alert)
			}
		}
	} else {
		// Handle case where Status and Response are not present (use header and entity)
		for _, i := range result.Entity {
			i.Alert.ID = i.ID
			alerts = append(alerts, i.Alert)
		}
	}

	cachedAlertsData[v.name] = alerts
	lastUpdatedAlertsCache[v.name] = time.Now()

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
	Status   *string `json:"status,omitempty"`
	Response *struct {
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
	} `json:"response,omitempty"`
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
