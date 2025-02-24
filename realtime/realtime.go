package realtime

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"regexp"
	"time"
)

func hashKey(data string) string {
	hash := sha256.Sum256([]byte(data)) // Get the 32-byte hash
	return fmt.Sprintf("%x", hash)      // Convert to hex string
}

func NewClient(ApiKey string, ApiHeader string, refreshPeriod time.Duration, vehiclesUrl, tripUpdatesUrl, alertsUrl string) (RealtimeRedo, error) {
	if ApiKey == "" {
		return RealtimeRedo{}, errors.New("missing api key")
	}
	if ApiHeader == "" {
		return RealtimeRedo{}, errors.New("missing api header")
	}

	urlRegex := regexp.MustCompile(`^(http:\/\/www\.|https:\/\/www\.|http:\/\/|https:\/\/|\/|\/\/)?[A-z0-9_-]*?[:]?[A-z0-9_-]*?[@]?[A-z0-9]+([\-\.]{1}[a-z0-9]+)*\.[a-z]{2,5}(:[0-9]{1,5})?(\/.*)?$`)

	if vehiclesUrl == "" || !urlRegex.MatchString(vehiclesUrl) {
		return RealtimeRedo{}, errors.New("invalid vehicles url")
	}
	if tripUpdatesUrl == "" || !urlRegex.MatchString(tripUpdatesUrl) {
		return RealtimeRedo{}, errors.New("invalid trip updates url")
	}
	if alertsUrl == "" || !urlRegex.MatchString(alertsUrl) {
		return RealtimeRedo{}, errors.New("invalid alerts url")
	}

	return RealtimeRedo{
		apiKey:         ApiKey,
		apiHeader:      ApiHeader,
		refreshPeriod:  refreshPeriod,
		vehiclesUrl:    vehiclesUrl,
		tripUpdatesUrl: tripUpdatesUrl,
		alertsUrl:      alertsUrl,
		uuid:           hashKey(vehiclesUrl + tripUpdatesUrl + alertsUrl),
	}, nil
}

type RealtimeRedo struct {
	apiKey    string
	apiHeader string

	vehiclesUrl    string
	tripUpdatesUrl string
	alertsUrl      string

	refreshPeriod time.Duration

	uuid string
}
