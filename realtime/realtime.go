package realtime

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"time"
)

func hashKey(data string) string {
	hash := sha256.Sum256([]byte(data)) // Get the 32-byte hash
	return fmt.Sprintf("%x", hash)      // Convert to hex string
}

func NewClient(ApiKey string, ApiHeader string, refreshPeriod time.Duration, vehiclesUrl, tripUpdatesUrl, alertsUrl string) (Realtime, error) {
	if ApiKey == "" {
		return Realtime{}, errors.New("missing api key")
	}
	if ApiHeader == "" {
		return Realtime{}, errors.New("missing api header")
	}

	urlRegex := regexp.MustCompile(`^(http:\/\/www\.|https:\/\/www\.|http:\/\/|https:\/\/|\/|\/\/)?[A-z0-9_-]*?[:]?[A-z0-9_-]*?[@]?[A-z0-9]+([\-\.]{1}[a-z0-9]+)*\.[a-z]{2,5}(:[0-9]{1,5})?(\/.*)?$`)

	if vehiclesUrl == "" || !urlRegex.MatchString(vehiclesUrl) {
		return Realtime{}, errors.New("invalid vehicles url")
	}
	if tripUpdatesUrl == "" || !urlRegex.MatchString(tripUpdatesUrl) {
		return Realtime{}, errors.New("invalid trip updates url")
	}
	if alertsUrl == "" || !urlRegex.MatchString(alertsUrl) {
		return Realtime{}, errors.New("invalid alerts url")
	}

	return Realtime{
		apiKey:         ApiKey,
		apiHeader:      ApiHeader,
		refreshPeriod:  refreshPeriod,
		vehiclesUrl:    vehiclesUrl,
		tripUpdatesUrl: tripUpdatesUrl,
		alertsUrl:      alertsUrl,
		uuid:           hashKey(vehiclesUrl + tripUpdatesUrl + alertsUrl),
	}, nil
}

type Realtime struct {
	apiKey    string
	apiHeader string

	vehiclesUrl    string
	tripUpdatesUrl string
	alertsUrl      string

	refreshPeriod time.Duration

	uuid string
}

// Returns the entity(s) from the response
func fetchData[EntityType any](url, apiHeader, apiKey string) (EntityType, error) {
	var zeroValue EntityType
	if url == "" {
		return zeroValue, fmt.Errorf("fetchData: missing URL")
	}
	if apiKey == "" {
		return zeroValue, fmt.Errorf("fetchData: missing API key")
	}
	if apiHeader == "" {
		apiHeader = "Authorization" // Default header
	}

	httpClient := http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return zeroValue, fmt.Errorf("fetchData: error creating request: %w", err)
	}

	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set(apiHeader, apiKey)

	resp, err := httpClient.Do(req)
	if err != nil {
		return zeroValue, fmt.Errorf("fetchData: error making request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return zeroValue, fmt.Errorf("fetchData: unexpected status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return zeroValue, fmt.Errorf("fetchData: error reading response body: %w", err)
	}

	var result RealtimeResponse[EntityType]
	if err := json.Unmarshal(body, &result); err != nil {
		return zeroValue, fmt.Errorf("fetchData: error parsing JSON: %w", err)
	}

	if result.Status != nil {
		// Handle case where Status and Response are present
		if result.Response != nil {
			return result.Response.Entity, nil
		}
	} else {
		// Handle case where Status and Response are not present (use header and entity)
		return result.Entity, nil
	}

	return zeroValue, nil
}

type RealtimeResponse[T any] struct {
	Status   *string `json:"status,omitempty"`
	Response *struct {
		Header struct {
			Timestamp           float64 `json:"timestamp"`
			GtfsRealtimeVersion string  `json:"gtfs_realtime_version"`
			Incrementality      int64   `json:"incrementality"`
		} `json:"header"`
		Entity T `json:"entity"`
	} `json:"response,omitempty"`
	Header struct {
		Timestamp           float64 `json:"timestamp"`
		GtfsRealtimeVersion string  `json:"gtfs_realtime_version"`
		Incrementality      int64   `json:"incrementality"`
	} `json:"header"`
	Entity T `json:"entity"`
}
