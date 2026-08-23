// Package job defines the Job record shared by the store, scheduler, and CLI.
package job

import "time"

type Status string

const (
	StatusPending   Status = "pending"   // submitted, no scheduled_at yet
	StatusScheduled Status = "scheduled" // scheduled_at chosen, waiting for it
	StatusRunning   Status = "running"   // subprocess is executing
	StatusDone      Status = "done"      // exited 0
	StatusFailed    Status = "failed"    // exited non-zero or exec error
)

// Job is one unit of deferrable work: a shell command that must run somewhere
// between Earliest and Deadline, expected to take about Duration.
type Job struct {
	ID       int64
	Command  string
	Zone     string // carbon-intensity zone code, e.g. "US-CAL-CISO"
	Earliest time.Time
	Deadline time.Time
	Duration time.Duration

	Status      Status
	ScheduledAt *time.Time
	ExecutedAt  *time.Time
	ExitCode    *int
	Stdout      string
	Stderr      string

	// BaselineIntensity is the peak forecasted gCO2/kWh in [Earliest, Deadline] —
	// what running "immediately" in the worst slot would have cost.
	BaselineIntensity *float64
	// ActualIntensity is the forecasted intensity at the chosen ScheduledAt.
	ActualIntensity *float64

	CreatedAt time.Time
}

// SavedGrams estimates CO2 avoided by running at ActualIntensity instead of
// BaselineIntensity, per kWh consumed by the job. Without a real power-draw
// figure per job, this reports intensity delta (gCO2/kWh) rather than an
// absolute mass — the CLI labels it accordingly.
func (j Job) IntensityDelta() (float64, bool) {
	if j.BaselineIntensity == nil || j.ActualIntensity == nil {
		return 0, false
	}
	return *j.BaselineIntensity - *j.ActualIntensity, true
}
