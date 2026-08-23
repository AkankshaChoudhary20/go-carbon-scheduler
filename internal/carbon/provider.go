// Package carbon defines a pluggable source of grid carbon-intensity forecasts.
package carbon

import (
	"context"
	"time"
)

// Point is one forecasted sample: carbon intensity of the electricity grid
// at Time, in grams of CO2 equivalent per kilowatt-hour.
type Point struct {
	Time           time.Time
	GramsCO2PerKWh float64
}

// Provider returns a carbon-intensity forecast for a zone over [from, to].
// Implementations may return points at whatever granularity they have
// (typically hourly); callers must not assume a fixed spacing.
type Provider interface {
	Forecast(ctx context.Context, zone string, from, to time.Time) ([]Point, error)
}
