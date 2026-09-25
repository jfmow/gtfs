package gtfs

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// Metlink marks a trip's first stop pickup-only (drop_off_type=1 - nobody
// gets off where it starts) and its last stop drop-off-only
// (pickup_type=1). A departures board must still list the first stop, and
// a trip's stop list must still start there.
func newPickupDropoffTestDatabase(t *testing.T) Database {
	t.Helper()
	db := newTestDatabase(t, "pickupdropoff")
	zip := buildGTFSZip(t, map[string]string{
		"routes.txt":   "route_id,route_short_name,route_long_name,route_type\nHVL,HVL,Hutt Valley Line,2\n",
		"calendar.txt": "service_id,monday,tuesday,wednesday,thursday,friday,saturday,sunday,start_date,end_date\nWK,1,1,1,1,1,1,1,20260101,20261231\n",
		"trips.txt":    "route_id,service_id,trip_id,trip_headsign\nHVL,WK,T1,Upper Hutt\n",
		"stops.txt": "stop_id,stop_name,stop_lat,stop_lon,location_type,parent_station\n" +
			"WELL,Wellington Station,-41.278,174.781,1,\n" +
			"WELL1,Wellington Station,-41.278,174.781,0,WELL\n" +
			"PETO,Petone Station,-41.221,174.870,0,\n" +
			"UPPE,Upper Hutt Station,-41.124,175.070,0,\n",
		"stop_times.txt": "trip_id,arrival_time,departure_time,stop_id,stop_sequence,pickup_type,drop_off_type\n" +
			"T1,22:05:00,22:05:00,WELL1,1,0,1\n" +
			"T1,22:17:00,22:17:00,PETO,2,0,0\n" +
			"T1,22:50:00,22:50:00,UPPE,3,1,0\n",
	})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(zip) }))
	t.Cleanup(srv.Close)
	db.url = srv.URL
	if err := db.refreshDatabaseData(); err != nil {
		t.Fatalf("refresh failed: %v", err)
	}
	return db
}

func TestGetActiveTripsIncludesPickupOnlyOrigin(t *testing.T) {
	db := newPickupDropoffTestDatabase(t)
	date := time.Date(2026, 9, 25, 21, 50, 0, 0, time.UTC)

	origin, err := db.GetActiveTrips("WELL1", "21:50:00", date, 10)
	if err != nil || len(origin) != 1 || origin[0].TripID != "T1" {
		t.Fatalf("expected T1 departing its pickup-only origin, got %+v (err %v)", origin, err)
	}

	// The drop-off-only terminus is not a departure.
	terminus, _ := db.GetActiveTrips("UPPE", "21:50:00", date, 10)
	if len(terminus) != 0 {
		t.Fatalf("expected no departures from a drop-off-only terminus, got %+v", terminus)
	}
}

func TestTripStopListsKeepPickupOnlyOrigin(t *testing.T) {
	db := newPickupDropoffTestDatabase(t)

	stops, _, err := db.GetStopsForTripID("T1")
	if err != nil || len(stops) != 3 || stops[0].StopId != "WELL1" {
		t.Fatalf("expected 3 stops starting at WELL1, got %+v (err %v)", stops, err)
	}

	times, err := db.GetStopTimesForTripID("T1")
	if err != nil || len(times) != 3 {
		t.Fatalf("expected 3 stop times, got %d (err %v)", len(times), err)
	}

	service, err := db.GetServiceByTripAndStop("T1", "WELL1", "")
	if err != nil || service.TripID != "T1" {
		t.Fatalf("expected T1 at its pickup-only origin, got %+v (err %v)", service, err)
	}
}
