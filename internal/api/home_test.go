package api

import (
	"reflect"
	"slices"
	"testing"

	"github.com/hardware661/husic/internal/store"
)

func TestHomeLikesStableWithinRecommendationDay(t *testing.T) {
	likes := make([]store.Song, 32)
	for i := range likes {
		likes[i] = store.Song{ID: int64(i + 1), Title: "liked song"}
	}
	original := slices.Clone(likes)
	first := homeLikes(likes, "2026-10-23")
	for i := 0; i < 10; i++ {
		if !reflect.DeepEqual(first, homeLikes(likes, "2026-10-23")) {
			t.Fatal("returning home redraws liked covers on the same day")
		}
	}
	if !reflect.DeepEqual(likes, original) {
		t.Fatal("sampling changed library order")
	}
	slices.Reverse(likes)
	if !reflect.DeepEqual(first, homeLikes(likes, "2026-10-23")) {
		t.Fatal("database row order changed today's covers")
	}
	if reflect.DeepEqual(first, homeLikes(likes, "2026-10-24")) {
		t.Fatal("covers never renew with daily recommendations")
	}
	if len(first) != 8 || len(homeLikes(nil, "2026-10-23")) != 0 {
		t.Fatal("bad sample size")
	}
}
