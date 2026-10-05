package realtime

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jfmow/gtfs/realtime/proto" // Replace with your actual module path
	googleProto "google.golang.org/protobuf/proto"
)

func hashKey(data string) string {
	hash := sha256.Sum256([]byte(data))
	return fmt.Sprintf("%x", hash)
}

var urlRegex = regexp.MustCompile(`^(http:\/\/www\.|https:\/\/www\.|http:\/\/|https:\/\/|\/|\/\/)?[A-z0-9_-]*?[:]?[A-z0-9_-]*?[@]?[A-z0-9]+([\-\.]{1}[a-z0-9]+)*\.[a-z]{2,5}(:[0-9]{1,5})?(\/.*)?$`)

// NewClient reads vehicles, trip updates and alerts from three separate feed
// URLs. Each URL is requested at most once per refreshPeriod.
func NewClient(apiKey string, apiHeader string, refreshPeriod time.Duration, vehiclesUrl, tripUpdatesUrl, alertsUrl string, localTimeZone time.Location) (Realtime, error) {
	if err := checkKey(apiKey, apiHeader); err != nil {
		return Realtime{}, err
	}
	if vehiclesUrl == "" || !urlRegex.MatchString(vehiclesUrl) {
		return Realtime{}, errors.New("invalid vehicles url")
	}
	if tripUpdatesUrl == "" || !urlRegex.MatchString(tripUpdatesUrl) {
		return Realtime{}, errors.New("invalid trip updates url")
	}
	if alertsUrl == "" || !urlRegex.MatchString(alertsUrl) {
		return Realtime{}, errors.New("invalid alerts url")
	}

	return newRealtime(
		hashKey(vehiclesUrl+tripUpdatesUrl+alertsUrl),
		newFeedFetcher(vehiclesUrl, apiHeader, apiKey, refreshPeriod),
		newFeedFetcher(tripUpdatesUrl, apiHeader, apiKey, refreshPeriod),
		newFeedFetcher(alertsUrl, apiHeader, apiKey, refreshPeriod),
		localTimeZone,
	), nil
}

// NewCombinedClient reads vehicles, trip updates and alerts from one feed URL
// that carries all three entity types (e.g. Auckland Transport's
// /realtime/legacy). One request refreshes everything, so refreshPeriod is the
// request rate for the whole API key - a third of the separate-feed rate.
func NewCombinedClient(apiKey string, apiHeader string, refreshPeriod time.Duration, feedUrl string, localTimeZone time.Location) (Realtime, error) {
	if err := checkKey(apiKey, apiHeader); err != nil {
		return Realtime{}, err
	}
	if feedUrl == "" || !urlRegex.MatchString(feedUrl) {
		return Realtime{}, errors.New("invalid feed url")
	}

	feed := newFeedFetcher(feedUrl, apiHeader, apiKey, refreshPeriod)
	return newRealtime(hashKey(feedUrl), feed, feed, feed, localTimeZone), nil
}

func checkKey(apiKey, apiHeader string) error {
	if apiHeader != "" && apiKey == "" {
		return errors.New("missing api key")
	}
	if apiHeader == "" && apiKey != "" {
		return errors.New("missing api header")
	}
	return nil
}

func newRealtime(uuid string, vehicles, tripUpdates, alerts *feedFetcher, localTimeZone time.Location) Realtime {
	return Realtime{
		vehiclesFeed:     vehicles,
		tripUpdatesFeed:  tripUpdates,
		alertsFeed:       alerts,
		uuid:             uuid,
		tripUpdatesCache: &tripUpdateCache{},
		alertsCache:      &alertsCache{},
		vehiclesCache:    &vehiclesCache{},
		tripHistoryCache: &tripHistoryCache{},
		localTimeZone:    &localTimeZone,
	}
}

type Realtime struct {
	// Where each entity type comes from. The same fetcher for all three with
	// a combined feed.
	vehiclesFeed    *feedFetcher
	tripUpdatesFeed *feedFetcher
	alertsFeed      *feedFetcher

	uuid string

	tripUpdatesCache *tripUpdateCache
	vehiclesCache    *vehiclesCache
	alertsCache      *alertsCache
	tripHistoryCache *tripHistoryCache

	localTimeZone *time.Location
}

// Fetches and parses protobuf GTFS-realtime data. retryAfter is the upstream's
// Retry-After on a failed request (0 when it didn't send one). An empty feed
// is not an error - overnight, or with no current alerts, that's the answer.
func fetchProto(url, apiHeader, apiKey string) (entities []*proto.FeedEntity, retryAfter time.Duration, err error) {
	if url == "" {
		return nil, 0, fmt.Errorf("missing URL")
	}

	client := http.Client{Timeout: 20 * time.Second}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("error creating request: %w", err)
	}

	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("Accept", "application/x-protobuf")
	if apiHeader != "" && apiKey != "" {
		req.Header.Set(apiHeader, apiKey)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("error performing request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// The body says why (e.g. an API gateway's "Out of call volume quota").
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
		return nil, parseRetryAfter(resp.Header.Get("Retry-After")),
			fmt.Errorf("unexpected status code: %d: %s", resp.StatusCode, strings.TrimSpace(string(snippet)))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, fmt.Errorf("error reading response body: %w", err)
	}

	var feed proto.FeedMessage
	if err := googleProto.Unmarshal(body, &feed); err != nil {
		return nil, 0, fmt.Errorf("error unmarshalling protobuf: %w", err)
	}

	return feed.Entity, 0, nil
}

// parseRetryAfter reads a Retry-After header in seconds; 0 if absent or in
// another form.
func parseRetryAfter(h string) time.Duration {
	secs, err := strconv.Atoi(strings.TrimSpace(h))
	if err != nil || secs <= 0 {
		return 0
	}
	return time.Duration(secs) * time.Second
}
