package scheduler

import (
	"testing"
	"time"

	"go-carbon-scheduler/internal/carbon"
)

func TestPickStartEmptyForecastErrors(t *testing.T) {
	_, err := PickStart(nil, time.Hour, time.Now(), time.Now().Add(time.Hour))
	if err == nil {
		t.Fatal("expected error for empty forecast")
	}
}

func TestPickStartChoosesKnownLowestWindow(t *testing.T) {
	base := time.Date(2026, 8, 24, 0, 0, 0, 0, time.UTC)
	// Synthetic day: high all day, a clear low trough at 03:00-04:00.
	var forecast []carbon.Point
	for h := 0; h < 24; h++ {
		intensity := 400.0
		if h == 3 {
			intensity = 100.0
		}
		forecast = append(forecast, carbon.Point{Time: base.Add(time.Duration(h) * time.Hour), GramsCO2PerKWh: intensity})
	}

	pick, err := PickStart(forecast, time.Hour, base, base.Add(24*time.Hour))
	if err != nil {
		t.Fatalf("PickStart: %v", err)
	}
	if !pick.Start.Equal(base.Add(3 * time.Hour)) {
		t.Errorf("Start = %v, want %v", pick.Start, base.Add(3*time.Hour))
	}
	if pick.WindowAvgGrams != 100.0 {
		t.Errorf("WindowAvgGrams = %v, want 100.0", pick.WindowAvgGrams)
	}
	if pick.PeakGrams != 400.0 {
		t.Errorf("PeakGrams = %v, want 400.0", pick.PeakGrams)
	}
	if pick.FellBackToLatest {
		t.Errorf("did not expect fallback")
	}
}

func TestPickStartAveragesMultiPointWindow(t *testing.T) {
	base := time.Date(2026, 8, 24, 0, 0, 0, 0, time.UTC)
	// 30-minute granularity, 3-hour job duration: the best 3h window should
	// average multiple points, not just pick a single lucky sample.
	var forecast []carbon.Point
	// Hours 0-2: noisy but high average (400ish). Hours 5-7: consistently low (150).
	highPattern := []float64{380, 420, 390, 410, 400, 415}
	lowPattern := []float64{150, 150, 150, 150, 150, 150}
	for i, v := range highPattern {
		forecast = append(forecast, carbon.Point{Time: base.Add(time.Duration(i) * 30 * time.Minute), GramsCO2PerKWh: v})
	}
	for i, v := range lowPattern {
		forecast = append(forecast, carbon.Point{Time: base.Add(10*time.Hour + time.Duration(i)*30*time.Minute), GramsCO2PerKWh: v})
	}

	pick, err := PickStart(forecast, 3*time.Hour, base, base.Add(24*time.Hour))
	if err != nil {
		t.Fatalf("PickStart: %v", err)
	}
	wantStart := base.Add(10 * time.Hour)
	if !pick.Start.Equal(wantStart) {
		t.Errorf("Start = %v, want %v", pick.Start, wantStart)
	}
	if pick.WindowAvgGrams != 150.0 {
		t.Errorf("WindowAvgGrams = %v, want 150.0", pick.WindowAvgGrams)
	}
}

func TestPickStartFallsBackWhenDurationDoesNotFit(t *testing.T) {
	base := time.Date(2026, 8, 24, 0, 0, 0, 0, time.UTC)
	forecast := []carbon.Point{
		{Time: base, GramsCO2PerKWh: 300},
		{Time: base.Add(30 * time.Minute), GramsCO2PerKWh: 250},
	}

	// Deadline is only 30 minutes out, but the job takes an hour — cannot
	// possibly finish inside the window, so we must fall back rather than
	// return an empty/zero result.
	deadline := base.Add(30 * time.Minute)
	pick, err := PickStart(forecast, time.Hour, base, deadline)
	if err != nil {
		t.Fatalf("PickStart: %v", err)
	}
	if !pick.FellBackToLatest {
		t.Errorf("expected FellBackToLatest = true when duration exceeds the window")
	}
	if pick.Start.Before(base) || pick.Start.After(deadline) {
		t.Errorf("fallback Start %v outside [earliest, deadline] = [%v, %v]", pick.Start, base, deadline)
	}
}

func TestPickStartHonorsEarliestBound(t *testing.T) {
	base := time.Date(2026, 8, 24, 0, 0, 0, 0, time.UTC)
	var forecast []carbon.Point
	for h := 0; h < 12; h++ {
		intensity := 400.0
		if h == 1 { // lowest point is BEFORE earliest and must be ignored
			intensity = 50.0
		}
		if h == 6 {
			intensity = 200.0 // lowest point that's actually eligible
		}
		forecast = append(forecast, carbon.Point{Time: base.Add(time.Duration(h) * time.Hour), GramsCO2PerKWh: intensity})
	}

	earliest := base.Add(4 * time.Hour)
	pick, err := PickStart(forecast, time.Hour, earliest, base.Add(12*time.Hour))
	if err != nil {
		t.Fatalf("PickStart: %v", err)
	}
	if !pick.Start.Equal(base.Add(6 * time.Hour)) {
		t.Errorf("Start = %v, want %v (earliest bound should exclude the hour-1 trough)", pick.Start, base.Add(6*time.Hour))
	}
}
