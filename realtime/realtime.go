package realtime

import (
	"errors"
	"regexp"
	"time"
)

type RealtimeS struct {
	apiKey    string
	apiHeader string
	name      string
}

type tripUpdates struct {
	url           string
	apiKey        string
	apiHeader     string
	name          string
	refreshPeriod time.Duration
}
type vehicles struct {
	url           string
	apiKey        string
	apiHeader     string
	name          string
	refreshPeriod time.Duration
}
type alerts struct {
	url           string
	apiKey        string
	apiHeader     string
	name          string
	refreshPeriod time.Duration
}

func New(apiKey string, apiHeader string, name string) (RealtimeS, error) {
	if apiKey == "" {
		return RealtimeS{}, errors.New("missing api key")
	}
	if apiHeader == "" {
		return RealtimeS{}, errors.New("missing api header")
	}
	if len(name) < 3 {
		return RealtimeS{}, errors.New("missing name")
	}
	return RealtimeS{
		apiKey:    apiKey,
		apiHeader: apiHeader,
	}, nil
}

func (v RealtimeS) Vehicles(url string, refreshPeriod time.Duration) (vehicles, error) {
	regex := regexp.MustCompile(`^(http:\/\/www\.|https:\/\/www\.|http:\/\/|https:\/\/|\/|\/\/)?[A-z0-9_-]*?[:]?[A-z0-9_-]*?[@]?[A-z0-9]+([\-\.]{1}[a-z0-9]+)*\.[a-z]{2,5}(:[0-9]{1,5})?(\/.*)?$`)

	if url == "" || !regex.MatchString(url) {
		return vehicles{}, errors.New("missing vehicles url/invalid url")
	}
	return vehicles{
		url:           url,
		apiKey:        v.apiKey,
		apiHeader:     v.apiHeader,
		name:          v.name,
		refreshPeriod: refreshPeriod,
	}, nil
}

func (v RealtimeS) TripUpdates(url string, refreshPeriod time.Duration) (tripUpdates, error) {
	regex := regexp.MustCompile(`^(http:\/\/www\.|https:\/\/www\.|http:\/\/|https:\/\/|\/|\/\/)?[A-z0-9_-]*?[:]?[A-z0-9_-]*?[@]?[A-z0-9]+([\-\.]{1}[a-z0-9]+)*\.[a-z]{2,5}(:[0-9]{1,5})?(\/.*)?$`)

	if url == "" || !regex.MatchString(url) {
		return tripUpdates{}, errors.New("missing trip updates url/invalid url")
	}
	return tripUpdates{
		url:           url,
		apiKey:        v.apiKey,
		apiHeader:     v.apiHeader,
		name:          v.name,
		refreshPeriod: refreshPeriod,
	}, nil
}

func (v RealtimeS) Alerts(url string, refreshPeriod time.Duration) (alerts, error) {
	regex := regexp.MustCompile(`^(http:\/\/www\.|https:\/\/www\.|http:\/\/|https:\/\/|\/|\/\/)?[A-z0-9_-]*?[:]?[A-z0-9_-]*?[@]?[A-z0-9]+([\-\.]{1}[a-z0-9]+)*\.[a-z]{2,5}(:[0-9]{1,5})?(\/.*)?$`)

	if url == "" || !regex.MatchString(url) {
		return alerts{}, errors.New("missing alerts url/invalid url")
	}
	return alerts{
		url:           url,
		apiKey:        v.apiKey,
		apiHeader:     v.apiHeader,
		name:          v.name,
		refreshPeriod: refreshPeriod,
	}, nil
}
