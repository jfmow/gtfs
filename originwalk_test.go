package gtfs

import (
	"testing"
	"time"
)

func TestDropNegligibleOriginWalk(t *testing.T) {
	t0 := time.Date(2026, 10, 15, 9, 53, 0, 0, time.UTC)
	walk := func(km float64, from, to *Stop) JourneyLeg {
		return JourneyLeg{Mode: "walk", FromStop: from, ToStop: to, DistanceKm: km, DepartureTime: t0, ArrivalTime: t0.Add(30 * time.Second)}
	}
	ride := JourneyLeg{Mode: "transit", DepartureTime: t0.Add(2 * time.Minute)}
	busStop := &Stop{StopId: "A"}
	plat := func(p string) *Stop { return &Stop{StopId: "P" + p, PlatformNumber: p} }

	cases := []struct {
		name string
		legs []JourneyLeg
		drop bool
	}{
		{"20m to a bus stop", []JourneyLeg{walk(0.02, nil, busStop), ride}, true},
		{"real walk", []JourneyLeg{walk(0.3, nil, busStop), ride}, false},
		{"same platform", []JourneyLeg{walk(0.01, plat("2"), plat("2")), ride}, true},
		{"different platform", []JourneyLeg{walk(0.02, plat("1"), plat("2")), ride}, false},
		{"platform, unknown start", []JourneyLeg{walk(0.02, nil, plat("2")), ride}, false},
		{"walk only", []JourneyLeg{walk(0.02, nil, nil)}, false},
	}
	for _, c := range cases {
		got := dropNegligibleOriginWalk(c.legs)
		if dropped := len(got) < len(c.legs); dropped != c.drop {
			t.Errorf("%s: dropped=%v, want %v", c.name, dropped, c.drop)
		}
	}
}
