package scheduler

import (
	"math"
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

func TestIrregularForecastUsesElapsedTimeWeights(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var points []carbon.Point
	for minute := 0; minute < 5; minute++ {
		points = append(points, carbon.Point{Time: base.Add(time.Duration(minute) * time.Minute), GramsCO2PerKWh: 0})
	}
	points = append(points, carbon.Point{Time: base.Add(5 * time.Minute), GramsCO2PerKWh: 100}, carbon.Point{Time: base.Add(time.Hour), GramsCO2PerKWh: 50}, carbon.Point{Time: base.Add(2 * time.Hour), GramsCO2PerKWh: 50})
	pick, err := PickStart(points, time.Hour, base, base.Add(3*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if !pick.Start.Equal(base.Add(time.Hour)) || pick.WindowAvgGrams != 50 {
		t.Fatalf("dense short low samples biased selection: %+v", pick)
	}
}

func TestInvalidSchedulingWindowsAreRejected(t *testing.T) {
	now := time.Now()
	points := []carbon.Point{{Time: now, GramsCO2PerKWh: 10}}
	for _, duration := range []time.Duration{0, -time.Second} {
		if _, err := PickStart(points, duration, now, now.Add(time.Hour)); err == nil {
			t.Fatal("accepted invalid duration")
		}
	}
	if _, err := PickStart(points, time.Minute, now, now.Add(-time.Hour)); err == nil {
		t.Fatal("accepted reversed window")
	}
	for _, value := range []float64{math.NaN(), math.Inf(1), -1} {
		if _, err := PickStart([]carbon.Point{{Time: now, GramsCO2PerKWh: value}}, time.Minute, now, now.Add(time.Hour)); err == nil {
			t.Fatal("accepted invalid forecast intensity")
		}
	}
}
