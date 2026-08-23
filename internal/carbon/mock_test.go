package carbon

import (
	"context"
	"math"
	"testing"
	"time"
)

func TestMockProviderDeterministic(t *testing.T) {
	m := MockProvider{}
	from := time.Date(2026, 8, 24, 0, 0, 0, 0, time.UTC)
	to := from.Add(24 * time.Hour)

	a, err := m.Forecast(context.Background(), "US-CAL-CISO", from, to)
	if err != nil {
		t.Fatalf("Forecast: %v", err)
	}
	b, err := m.Forecast(context.Background(), "US-CAL-CISO", from, to)
	if err != nil {
		t.Fatalf("Forecast: %v", err)
	}

	if len(a) != len(b) {
		t.Fatalf("non-deterministic length: %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("non-deterministic point at %d: %+v vs %+v", i, a[i], b[i])
		}
	}
	if len(a) != 25 { // 0:00 through 24:00 inclusive at hourly step
		t.Fatalf("expected 25 hourly points, got %d", len(a))
	}
}

func TestMockProviderDiurnalShape(t *testing.T) {
	m := MockProvider{}
	from := time.Date(2026, 8, 24, 0, 0, 0, 0, time.UTC)
	to := from.Add(24 * time.Hour)

	points, err := m.Forecast(context.Background(), "US-CAL-CISO", from, to)
	if err != nil {
		t.Fatalf("Forecast: %v", err)
	}

	_, troughHour := zoneParams("US-CAL-CISO")

	minIdx, maxIdx := 0, 0
	for i, p := range points {
		if p.GramsCO2PerKWh < points[minIdx].GramsCO2PerKWh {
			minIdx = i
		}
		if p.GramsCO2PerKWh > points[maxIdx].GramsCO2PerKWh {
			maxIdx = i
		}
	}

	gotTrough := float64(points[minIdx].Time.Hour())
	gotPeak := float64(points[maxIdx].Time.Hour())

	if diffHours(gotTrough, troughHour) > 1 {
		t.Errorf("trough at hour %v, want near %v", gotTrough, troughHour)
	}
	wantPeak := math.Mod(troughHour+12, 24)
	if diffHours(gotPeak, wantPeak) > 1 {
		t.Errorf("peak at hour %v, want near %v", gotPeak, wantPeak)
	}
}

func diffHours(a, b float64) float64 {
	d := a - b
	if d < 0 {
		d = -d
	}
	if d > 12 {
		d = 24 - d
	}
	return d
}
