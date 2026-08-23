package carbon

import (
	"context"
	"hash/fnv"
	"math"
	"time"
)

// MockProvider synthesizes a deterministic diurnal carbon-intensity curve —
// low overnight (wind/base load dominant, low demand), high in the evening
// (demand peak, solar drop-off) — so the whole tool is runnable and
// testable without any API key. The curve is a pure function of zone and
// calendar time, so repeated calls for the same inputs always agree.
type MockProvider struct {
	// StepMinutes controls forecast granularity; defaults to 60 if zero.
	StepMinutes int
}

func (m MockProvider) Forecast(_ context.Context, zone string, from, to time.Time) ([]Point, error) {
	step := time.Duration(m.StepMinutes) * time.Minute
	if step <= 0 {
		step = time.Hour
	}

	mean, troughHour := zoneParams(zone)
	const amplitude = 150.0

	eval := func(t time.Time) Point {
		hour := float64(t.Hour()) + float64(t.Minute())/60
		phase := 2 * math.Pi * (hour - troughHour) / 24
		return Point{Time: t, GramsCO2PerKWh: mean - amplitude*math.Cos(phase)}
	}

	// Start exactly at `from` (not the nearest step boundary) so a window
	// narrower than one step — e.g. a job with a 2-minute deadline — still
	// gets at least one point instead of an empty forecast.
	var points []Point
	for t := from; !t.After(to); t = t.Add(step) {
		points = append(points, eval(t))
	}
	if len(points) == 0 || points[len(points)-1].Time.Before(to) {
		points = append(points, eval(to))
	}
	return points, nil
}

// zoneParams derives a stable (but zone-varying) mean intensity and trough
// hour so different zones don't all look identical in a demo.
func zoneParams(zone string) (mean, troughHour float64) {
	h := fnv.New32a()
	_, _ = h.Write([]byte(zone))
	sum := h.Sum32()
	mean = 300 + float64(sum%150) // 300-450 gCO2/kWh baseline
	troughHour = float64(sum % 6) // spread the trough between 0-5am
	return mean, troughHour
}
