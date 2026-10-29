package api

import (
	"testing"
	"time"
)

func TestNetEaseDayRollsOverAtSixInBeijing(t *testing.T) {
	cst := time.FixedZone("CST", 8*3600)
	for _, c := range []struct {
		at   time.Time
		want string
	}{
		{time.Date(2026, 10, 23, 5, 59, 0, 0, cst), "2026-10-22"},
		{time.Date(2026, 10, 23, 6, 0, 0, 0, cst), "2026-10-23"},
		{time.Date(2026, 10, 23, 22, 30, 0, 0, cst), "2026-10-23"},
		{time.Date(2026, 10, 23, 23, 59, 0, 0, cst), "2026-10-23"},
	} {
		if got := netEaseDay(c.at); got != c.want {
			t.Errorf("%s: got %s, want %s", c.at.Format(time.RFC3339), got, c.want)
		}
	}
}
