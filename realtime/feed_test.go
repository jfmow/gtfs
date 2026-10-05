package realtime

import (
	"errors"
	"testing"
	"time"

	"github.com/jfmow/gtfs/realtime/proto"
)

type fakeUpstream struct {
	calls      int
	err        error
	retryAfter time.Duration
	entities   []*proto.FeedEntity
}

func (u *fakeUpstream) fetch(string, string, string) ([]*proto.FeedEntity, time.Duration, error) {
	u.calls++
	if u.err != nil {
		return nil, u.retryAfter, u.err
	}
	return u.entities, 0, nil
}

func testFetcher(u *fakeUpstream, period time.Duration) *feedFetcher {
	f := newFeedFetcher("https://example.com/feed", "", "", period)
	f.fetch = u.fetch
	return f
}

func TestFeedFetcherCachesForPeriod(t *testing.T) {
	u := &fakeUpstream{entities: []*proto.FeedEntity{{}}}
	f := testFetcher(u, 20*time.Second)
	t0 := time.Now()

	for i := 0; i < 5; i++ {
		if _, _, err := f.get(t0.Add(time.Duration(i) * time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	if u.calls != 1 {
		t.Fatalf("calls = %d within the period, want 1", u.calls)
	}
	f.get(t0.Add(20 * time.Second))
	if u.calls != 2 {
		t.Fatalf("calls = %d after the period, want 2", u.calls)
	}
}

func TestFeedFetcherCachesEmptyFeed(t *testing.T) {
	u := &fakeUpstream{} // no entities, e.g. no current alerts
	f := testFetcher(u, 20*time.Second)
	t0 := time.Now()

	for i := 0; i < 3; i++ {
		ents, _, err := f.get(t0.Add(time.Duration(i) * time.Second))
		if err != nil || len(ents) != 0 {
			t.Fatalf("get = %v, %v; want empty, nil", ents, err)
		}
	}
	if u.calls != 1 {
		t.Fatalf("calls = %d, want 1 - an empty feed must still be cached", u.calls)
	}
}

func TestFeedFetcherServesStaleAndBacksOff(t *testing.T) {
	u := &fakeUpstream{entities: []*proto.FeedEntity{{}}}
	f := testFetcher(u, 20*time.Second)
	t0 := time.Now()
	_, first, _ := f.get(t0)

	u.err = errors.New("unexpected status code: 403")

	// Fails at +20s: stale data, retry not before +40s.
	ents, at, err := f.get(t0.Add(20 * time.Second))
	if err != nil || len(ents) != 1 || !at.Equal(first) {
		t.Fatalf("want the stale feed, got %v, %v, %v", ents, at, err)
	}
	// Every request in the backoff window is answered without the upstream.
	for s := 21; s < 40; s++ {
		f.get(t0.Add(time.Duration(s) * time.Second))
	}
	if u.calls != 2 {
		t.Fatalf("calls = %d, want 2 - no requests during backoff", u.calls)
	}
	// Second failure at +40s doubles the wait: next try at +80s.
	f.get(t0.Add(40 * time.Second))
	f.get(t0.Add(79 * time.Second))
	if u.calls != 3 {
		t.Fatalf("calls = %d, want 3", u.calls)
	}

	// Past maxStaleAge the error comes through.
	if _, _, err := f.get(t0.Add(maxStaleAge + time.Second)); err == nil {
		t.Fatal("want the error once the cached feed is too old")
	}

	// Recovery resets the backoff.
	u.err = nil
	later := t0.Add(time.Hour)
	if _, at, err := f.get(later); err != nil || !at.Equal(later) {
		t.Fatalf("want a fresh fetch, got %v, %v", at, err)
	}
	if f.failures != 0 || !f.retryAt.IsZero() {
		t.Fatal("backoff not reset after a good fetch")
	}
}

func TestFeedFetcherErrorWithNoData(t *testing.T) {
	u := &fakeUpstream{err: errors.New("boom")}
	f := testFetcher(u, 20*time.Second)
	t0 := time.Now()

	if _, _, err := f.get(t0); err == nil {
		t.Fatal("want the error with nothing cached")
	}
	if _, _, err := f.get(t0.Add(time.Second)); err == nil || u.calls != 1 {
		t.Fatalf("want the last error without a new request, got %v, calls %d", err, u.calls)
	}
}

func TestBackoff(t *testing.T) {
	p := 20 * time.Second
	cases := []struct {
		failures   int
		retryAfter time.Duration
		want       time.Duration
	}{
		{1, 0, 20 * time.Second},
		{2, 0, 40 * time.Second},
		{4, 0, 160 * time.Second},
		{5, 0, maxFetchBackoff},
		{50, 0, maxFetchBackoff},
		{1, time.Hour, time.Hour}, // honour the upstream
	}
	for _, c := range cases {
		if got := backoff(p, c.failures, c.retryAfter); got != c.want {
			t.Errorf("backoff(%d, %v) = %v, want %v", c.failures, c.retryAfter, got, c.want)
		}
	}
}

func TestCombinedClientSplitsEntityTypes(t *testing.T) {
	tripID, alertID := "t1", "a1"
	u := &fakeUpstream{entities: []*proto.FeedEntity{
		{Id: &tripID, Vehicle: &proto.VehiclePosition{Trip: &proto.TripDescriptor{TripId: &tripID}}},
		{Id: &tripID, TripUpdate: &proto.TripUpdate{Trip: &proto.TripDescriptor{TripId: &tripID, StartDate: strPtr("20260101"), StartTime: strPtr("08:00:00")}}},
		{Id: &alertID, Alert: &proto.Alert{}},
	}}
	rt, err := NewCombinedClient("", "", 20*time.Second, "https://example.com/feed", *time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	rt.vehiclesFeed.fetch = u.fetch

	vehicles, err := rt.GetVehicles()
	if err != nil || len(vehicles) != 1 {
		t.Fatalf("vehicles = %v, %v", vehicles, err)
	}
	updates, err := rt.GetTripUpdates()
	if err != nil || len(updates) != 1 {
		t.Fatalf("trip updates = %v, %v", updates, err)
	}
	alerts, err := rt.GetAlerts()
	if err != nil || len(alerts) != 1 {
		t.Fatalf("alerts = %v, %v", alerts, err)
	}
	if u.calls != 1 {
		t.Fatalf("calls = %d, want 1 for all three types", u.calls)
	}
}

func strPtr(s string) *string { return &s }

func TestDisabledClient(t *testing.T) {
	v := NewDisabledClient(*time.UTC)
	if _, err := v.GetVehicles(); !errors.Is(err, ErrNoRealtime) {
		t.Fatalf("GetVehicles err = %v, want ErrNoRealtime", err)
	}
	if _, err := v.GetTripUpdates(); !errors.Is(err, ErrNoRealtime) {
		t.Fatalf("GetTripUpdates err = %v, want ErrNoRealtime", err)
	}
	if _, err := v.GetAlerts(); !errors.Is(err, ErrNoRealtime) {
		t.Fatalf("GetAlerts err = %v, want ErrNoRealtime", err)
	}
	v.EnableTripHistory()
	if len(v.GetTripHistory()) != 0 {
		t.Fatal("expected no trip history")
	}
}
