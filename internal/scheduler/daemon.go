package scheduler

import (
	"bytes"
	"context"
	"log"
	"os/exec"
	"time"

	"go-carbon-scheduler/internal/carbon"
	"go-carbon-scheduler/internal/job"
	"go-carbon-scheduler/internal/store"
)

// Daemon periodically assigns start times to pending jobs and executes ones
// whose scheduled time has arrived.
type Daemon struct {
	Store    *store.Store
	Provider carbon.Provider
	Poll     time.Duration
	Logger   *log.Logger
}

func (d *Daemon) logf(format string, args ...any) {
	if d.Logger != nil {
		d.Logger.Printf(format, args...)
	}
}

// Run blocks, ticking every d.Poll until ctx is cancelled.
func (d *Daemon) Run(ctx context.Context) error {
	if err := d.Tick(ctx); err != nil {
		d.logf("tick error: %v", err)
	}

	ticker := time.NewTicker(d.Poll)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := d.Tick(ctx); err != nil {
				d.logf("tick error: %v", err)
			}
		}
	}
}

// Tick runs one scheduling+execution pass. Exported so tests and the CLI
// can drive it synchronously instead of waiting on the poll interval.
func (d *Daemon) Tick(ctx context.Context) error {
	if err := d.schedulePending(ctx); err != nil {
		return err
	}
	return d.runDue(ctx)
}

func (d *Daemon) schedulePending(ctx context.Context) error {
	pending, err := d.Store.Pending()
	if err != nil {
		return err
	}
	for _, j := range pending {
		forecast, err := d.Provider.Forecast(ctx, j.Zone, j.Earliest, j.Deadline)
		if err != nil {
			d.logf("job %d: forecast error, skipping this tick: %v", j.ID, err)
			continue
		}
		pick, err := PickStart(forecast, j.Duration, j.Earliest, j.Deadline)
		if err != nil {
			d.logf("job %d: pick error, skipping this tick: %v", j.ID, err)
			continue
		}
		if err := d.Store.MarkScheduled(j.ID, pick.Start, pick.PeakGrams, pick.WindowAvgGrams); err != nil {
			return err
		}
		d.logf("job %d: scheduled for %s (window avg %.0f gCO2/kWh vs peak %.0f)",
			j.ID, pick.Start.Format(time.RFC3339), pick.WindowAvgGrams, pick.PeakGrams)
	}
	return nil
}

func (d *Daemon) runDue(ctx context.Context) error {
	due, err := d.Store.Due(time.Now())
	if err != nil {
		return err
	}
	for _, j := range due {
		if err := d.Store.MarkRunning(j.ID); err != nil {
			return err
		}
		d.logf("job %d: running %q", j.ID, j.Command)

		status, exitCode, stdout, stderr := runCommand(ctx, j.Command)
		if err := d.Store.MarkFinished(j.ID, status, exitCode, stdout, stderr); err != nil {
			return err
		}
		d.logf("job %d: finished with status=%s exit=%d", j.ID, status, exitCode)
	}
	return nil
}

func runCommand(ctx context.Context, command string) (status job.Status, exitCode int, stdout, stderr string) {
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	err := cmd.Run()
	stdout, stderr = outBuf.String(), errBuf.String()

	if err == nil {
		return job.StatusDone, 0, stdout, stderr
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		return job.StatusFailed, exitErr.ExitCode(), stdout, stderr
	}
	// Command couldn't even start (bad shell, permissions, etc.)
	return job.StatusFailed, -1, stdout, stderr + "\n" + err.Error()
}
