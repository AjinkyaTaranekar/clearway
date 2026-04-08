package model_test

import (
	"testing"

	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/capacity-service/internal/model"
)

func TestOccupancyLevel(t *testing.T) {
	cases := []struct {
		pct  float64
		want string
	}{
		{0, "low"},
		{24.9, "low"},
		{25, "moderate"},
		{49.9, "moderate"},
		{50, "high"},
		{74.9, "high"},
		{75, "critical"},
		{100, "critical"},
	}
	for _, c := range cases {
		got := model.OccupancyLevel(c.pct)
		if got != c.want {
			t.Errorf("OccupancyLevel(%.1f) = %q, want %q", c.pct, got, c.want)
		}
	}
}
