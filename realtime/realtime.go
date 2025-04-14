package realtime

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"time"

	"github.com/jfmow/gtfs/realtime/proto" // Replace with your actual module path
	googleProto "google.golang.org/protobuf/proto"
)

func hashKey(data string) string {
	hash := sha256.Sum256([]byte(data))
	return fmt.Sprintf("%x", hash)
}

func NewClient(apiKey string, apiHeader string, refreshPeriod time.Duration, vehiclesUrl, tripUpdatesUrl, alertsUrl string) (Realtime, error) {
	if apiKey == "" {
		return Realtime{}, errors.New("missing api key")
	}
	if apiHeader == "" {
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
		apiKey:         apiKey,
		apiHeader:      apiHeader,
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
	uuid          string
}

// Fetches and parses protobuf GTFS-realtime data
func fetchProto(url, apiHeader, apiKey string) ([]*proto.FeedEntity, error) {
	if url == "" {
		return nil, fmt.Errorf("missing URL")
	}
	if apiKey == "" {
		return nil, fmt.Errorf("missing API key")
	}
	if apiHeader == "" {
		apiHeader = "Authorization"
	}

	client := http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("error creating request: %w", err)
	}

	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("Accept", "application/x-protobuf")
	req.Header.Set(apiHeader, apiKey)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error performing request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading response body: %w", err)
	}

	var feed proto.FeedMessage
	if err := googleProto.Unmarshal(body, &feed); err != nil {
		return nil, fmt.Errorf("error unmarshalling protobuf: %w", err)
	}

	if len(feed.Entity) == 0 {
		return nil, errors.New("no results returned from the api")
	}

	return feed.Entity, nil
}
