// Package scheduler picks the lowest-carbon start time for a job within its
// deadline window, and runs the daemon loop that executes jobs once due.
package scheduler

import (
	"fmt"
	"sort"
	"time"

	"go-carbon-scheduler/internal/carbon"
)

// Pick is the result of choosing a start time for a job.
type Pick struct {
	Start            time.Time
	WindowAvgGrams   float64 // forecasted intensity of the chosen window
	PeakGrams        float64 // worst forecasted intensity anywhere in [earliest, deadline] — the "ran immediately at the worst moment" baseline
	FellBackToLatest bool    // true if no window fit before the deadline and we just picked the latest feasible start
}

// PickStart chooses the start time in [earliest, deadline-duration] whose
// [start, start+duration) window has the lowest average forecasted
// intensity. Forecast points need not be evenly spaced or pre-sorted.
func PickStart(forecast []carbon.Point, duration time.Duration, earliest, deadline time.Time) (Pick, error) {
	if len(forecast) == 0 {
		return Pick{}, fmt.Errorf("scheduler: empty forecast")
	}

	points := make([]carbon.Point, len(forecast))
	copy(points, forecast)
	sort.Slice(points, func(i, j int) bool { return points[i].Time.Before(points[j].Time) })

	peak := points[0].GramsCO2PerKWh
	for _, p := range points {
		if p.GramsCO2PerKWh > peak {
			peak = p.GramsCO2PerKWh
		}
	}

	latestStart := deadline.Add(-duration)
	if latestStart.Before(earliest) {
		// Duration doesn't fit inside the window at all — just meet the
		// deadline as best we can, using the intensity nearest that time.
		start := earliest
		return Pick{
			Start:            start,
			WindowAvgGrams:   nearestIntensity(points, start),
			PeakGrams:        peak,
			FellBackToLatest: true,
		}, nil
	}

	bestFound := false
	var best Pick
	for _, candidate := range points {
		start := candidate.Time
		if start.Before(earliest) || start.After(latestStart) {
			continue
		}
		avg, n := windowAverage(points, start, start.Add(duration))
		if n == 0 {
			avg = nearestIntensity(points, start)
		}
		if !bestFound || avg < best.WindowAvgGrams {
			bestFound = true
			best = Pick{Start: start, WindowAvgGrams: avg, PeakGrams: peak}
		}
	}

	if !bestFound {
		// No forecast point falls inside [earliest, latestStart]; run as
		// late as still safely meets the deadline.
		start := latestStart
		if start.Before(earliest) {
			start = earliest
		}
		return Pick{
			Start:            start,
			WindowAvgGrams:   nearestIntensity(points, start),
			PeakGrams:        peak,
			FellBackToLatest: true,
		}, nil
	}

	return best, nil
}

// windowAverage returns the mean intensity of points in [from, to) and how
// many contributed.
func windowAverage(points []carbon.Point, from, to time.Time) (float64, int) {
	sum, n := 0.0, 0
	for _, p := range points {
		if !p.Time.Before(from) && p.Time.Before(to) {
			sum += p.GramsCO2PerKWh
			n++
		}
	}
	if n == 0 {
		return 0, 0
	}
	return sum / float64(n), n
}

// nearestIntensity returns the intensity of the forecast point closest to t.
// points must be sorted ascending by Time.
func nearestIntensity(points []carbon.Point, t time.Time) float64 {
	best := points[0]
	bestDiff := absDuration(best.Time.Sub(t))
	for _, p := range points[1:] {
		if d := absDuration(p.Time.Sub(t)); d < bestDiff {
			best, bestDiff = p, d
		}
	}
	return best.GramsCO2PerKWh
}

func absDuration(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}
