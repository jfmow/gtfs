package realtime

import (
	"fmt"
	"sync"
	"time"

	"github.com/jfmow/gtfs/realtime/proto"
)

const (
	// Longest wait between retries after the upstream keeps failing.
	maxFetchBackoff = 5 * time.Minute
	// How long the last good feed is still served while the upstream is
	// failing. Past this, callers get the error instead of very old data.
	maxStaleAge = 10 * time.Minute
)

// feedFetcher owns every upstream request for one feed URL. All of the
// per-type caches (vehicles, trip updates, alerts) read through one, so the
// refresh period is a limit on requests to that URL however many callers or
// caches ask. With a combined feed all three share a single fetcher.
//
// After a failed fetch it waits before trying again (doubling up to
// maxFetchBackoff, or the upstream's Retry-After) and keeps serving the last
// good entities for up to maxStaleAge, so a rate-limited or erroring upstream
// isn't hit again by every incoming request.
type feedFetcher struct {
	mu sync.Mutex

	url       string
	apiHeader string
	apiKey    string
	period    time.Duration

	entities  []*proto.FeedEntity
	fetchedAt time.Time // of entities; zero until the first good fetch

	failures int
	retryAt  time.Time
	lastErr  error

	fetch func(url, apiHeader, apiKey string) ([]*proto.FeedEntity, time.Duration, error)
}

func newFeedFetcher(url, apiHeader, apiKey string, period time.Duration) *feedFetcher {
	return &feedFetcher{url: url, apiHeader: apiHeader, apiKey: apiKey, period: period, fetch: fetchProto}
}

// get returns the feed's entities and when they were fetched, requesting the
// upstream only when the cached copy is older than the refresh period and no
// backoff is in effect. Callers compare fetchedAt with their own to tell
// whether there's anything new to rebuild from.
func (f *feedFetcher) get(now time.Time) ([]*proto.FeedEntity, time.Time, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if !f.fetchedAt.IsZero() && now.Sub(f.fetchedAt) < f.period {
		return f.entities, f.fetchedAt, nil
	}
	if now.Before(f.retryAt) {
		return f.staleOrErr(now)
	}

	entities, retryAfter, err := f.fetch(f.url, f.apiHeader, f.apiKey)
	if err != nil {
		f.failures++
		f.lastErr = err
		f.retryAt = now.Add(backoff(f.period, f.failures, retryAfter))
		return f.staleOrErr(now)
	}

	f.failures = 0
	f.lastErr = nil
	f.retryAt = time.Time{}
	f.entities = entities
	f.fetchedAt = now
	return f.entities, f.fetchedAt, nil
}

func (f *feedFetcher) staleOrErr(now time.Time) ([]*proto.FeedEntity, time.Time, error) {
	if !f.fetchedAt.IsZero() && now.Sub(f.fetchedAt) < maxStaleAge {
		return f.entities, f.fetchedAt, nil
	}
	if f.lastErr == nil {
		return nil, time.Time{}, fmt.Errorf("feed unavailable")
	}
	return nil, time.Time{}, f.lastErr
}

// backoff is the wait before the next attempt after `failures` failures in a
// row: the refresh period doubled per failure, capped at maxFetchBackoff, and
// never shorter than an upstream Retry-After.
func backoff(period time.Duration, failures int, retryAfter time.Duration) time.Duration {
	wait := period
	for i := 1; i < failures && wait < maxFetchBackoff; i++ {
		wait *= 2
	}
	if wait > maxFetchBackoff {
		wait = maxFetchBackoff
	}
	if retryAfter > wait {
		wait = retryAfter
	}
	return wait
}
