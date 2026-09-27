package gtfs

import (
	"testing"
	"time"
)

func TestServiceDayStartDST(t *testing.T) {
	loc, err := time.LoadLocation("Pacific/Auckland")
	if err != nil {
		t.Skip("no tzdata:", err)
	}
	cases := []struct {
		name string
		at   time.Time
		want string // wall clock of GTFS 08:00:00 on that service day
	}{
		{"normal day", time.Date(2026, 9, 20, 9, 0, 0, 0, loc), "2026-09-20 08:00 NZST"},
		{"clocks forward", time.Date(2026, 9, 27, 9, 0, 0, 0, loc), "2026-09-27 08:00 NZDT"},
		{"clocks forward, before 2am", time.Date(2026, 9, 27, 0, 30, 0, 0, loc), "2026-09-27 08:00 NZDT"},
		{"clocks back", time.Date(2026, 4, 5, 9, 0, 0, 0, loc), "2026-04-05 08:00 NZST"},
	}
	for _, c := range cases {
		start := serviceDayStart(c.at, loc)
		got := start.Add(8 * time.Hour).In(loc).Format("2006-01-02 15:04 MST")
		if got != c.want {
			t.Errorf("%s: GTFS 08:00 = %s, want %s", c.name, got, c.want)
		}
		if d := serviceDayNoon(start).Format("20060102"); d != c.at.Format("20060102") {
			t.Errorf("%s: service date %s, want %s", c.name, d, c.at.Format("20060102"))
		}
	}
}
