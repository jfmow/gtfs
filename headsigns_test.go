package gtfs

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// Metlink's rail headsigns are station codes - "UPPE-All stops",
// "WAIK-Express" - in both stop_headsign and trip_headsign.
func TestRefreshTidiesStationCodeHeadsigns(t *testing.T) {
	db := newTestDatabase(t, "headsigns")
	zip := buildGTFSZip(t, map[string]string{
		"routes.txt":   "route_id,route_short_name,route_long_name,route_type\nHVL,HVL,Hutt Valley Line,2\n",
		"calendar.txt": "service_id,monday,tuesday,wednesday,thursday,friday,saturday,sunday,start_date,end_date\nWK,1,1,1,1,1,1,1,20260101,20261231\n",
		"trips.txt": "route_id,service_id,trip_id,trip_headsign\n" +
			"HVL,WK,T1,UPPE-All stops\n" +
			"HVL,WK,T2,Upper Hutt\n" +
			"HVL,WK,T3,ABCD-Express\n",
		"stops.txt": "stop_id,stop_name,stop_lat,stop_lon\n" +
			"WELL1,Wellington Station,-41.278,174.781\n" +
			"UPPE,Upper Hutt Station,-41.124,175.070\n" +
			"WAIK,Waikanae Station,-40.875,175.066\n",
		"stop_times.txt": "trip_id,arrival_time,departure_time,stop_id,stop_sequence,stop_headsign\n" +
			"T1,22:05:00,22:05:00,WELL1,1,UPPE-All stops\n" +
			"T2,22:10:00,22:10:00,WELL1,1,WAIK-Express\n" +
			"T3,22:15:00,22:15:00,WELL1,1,WELL-Non stop\n",
	})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(zip) }))
	defer srv.Close()
	db.url = srv.URL
	if err := db.refreshDatabaseData(); err != nil {
		t.Fatalf("refresh failed: %v", err)
	}

	stopHeadsigns := map[string]string{}
	rows, err := db.db.Query("SELECT trip_id, stop_headsign FROM stop_times")
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var trip, h string
		rows.Scan(&trip, &h)
		stopHeadsigns[trip] = h
	}
	rows.Close()
	want := map[string]string{"T1": "Upper Hutt", "T2": "Waikanae (Express)", "T3": "WELL-Non stop"}
	for trip, h := range want {
		// WELL isn't a stop in this feed, so its code is left alone.
		if stopHeadsigns[trip] != h {
			t.Errorf("stop_headsign for %s = %q, want %q", trip, stopHeadsigns[trip], h)
		}
	}

	var tripHeadsign string
	db.db.QueryRow("SELECT trip_headsign FROM trips WHERE trip_id = 'T1'").Scan(&tripHeadsign)
	if tripHeadsign != "Upper Hutt" {
		t.Errorf("trip_headsign for T1 = %q, want Upper Hutt", tripHeadsign)
	}
	db.db.QueryRow("SELECT trip_headsign FROM trips WHERE trip_id = 'T3'").Scan(&tripHeadsign)
	if tripHeadsign != "ABCD-Express" {
		t.Errorf("unknown code should be left alone, got %q", tripHeadsign)
	}
}
