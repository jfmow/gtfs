package realtime

import (
	"testing"
	"time"
)

func TestParseTripStart(t *testing.T) {
	loc, err := time.LoadLocation("Pacific/Auckland")
	if err != nil {
		t.Skip("no tzdata:", err)
	}
	cases := []struct{ date, clock, want string }{
		{"20260920", "08:00:00", "2026-09-20 08:00 NZST"},
		{"20260927", "08:00:00", "2026-09-27 08:00 NZDT"}, // clocks forward
		{"20260920", "25:10:00", "2026-09-21 01:10 NZST"}, // past midnight
	}
	for _, c := range cases {
		got, err := parseTripStart(c.date, c.clock, loc)
		if err != nil {
			t.Fatalf("%s %s: %v", c.date, c.clock, err)
		}
		if s := got.In(loc).Format("2006-01-02 15:04 MST"); s != c.want {
			t.Errorf("%s %s = %s, want %s", c.date, c.clock, s, c.want)
		}
	}
}
