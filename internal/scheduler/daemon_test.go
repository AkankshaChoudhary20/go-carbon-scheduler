package scheduler

import (
	"context"
	"strings"
	"testing"
	"time"

	"go-carbon-scheduler/internal/carbon"
	"go-carbon-scheduler/internal/job"
	"go-carbon-scheduler/internal/store"
)

// fakeProvider returns a fixed forecast with one clearly-lowest point at
// "now", so tests can assert exactly which start time gets picked.
type fakeProvider struct {
	lowPointTime time.Time
}

func (f fakeProvider) Forecast(_ context.Context, _ string, from, to time.Time) ([]carbon.Point, error) {
	return []carbon.Point{
		{Time: from, GramsCO2PerKWh: 500},
		{Time: f.lowPointTime, GramsCO2PerKWh: 50},
		{Time: to, GramsCO2PerKWh: 500},
	}, nil
}

func TestDaemonTickSchedulesAndRunsJob(t *testing.T) {
	s, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	now := time.Now()
	id, err := s.Insert(job.Job{
		Command:  "echo hello-carbon",
		Zone:     "TEST",
		Earliest: now.Add(-time.Minute),
		Deadline: now.Add(time.Hour),
		Duration: time.Second,
	})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}

	d := &Daemon{
		Store:    s,
		Provider: fakeProvider{lowPointTime: now},
		Poll:     time.Hour, // irrelevant for a synchronous Tick
	}

	if err := d.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	all, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("expected 1 job, got %d", len(all))
	}
	got := all[0]
	if got.ID != id {
		t.Fatalf("wrong job returned")
	}
	if got.Status != job.StatusDone {
		t.Fatalf("Status = %v, want done (stderr=%q)", got.Status, got.Stderr)
	}
	if got.ExitCode == nil || *got.ExitCode != 0 {
		t.Errorf("ExitCode = %v, want 0", got.ExitCode)
	}
	if !strings.Contains(got.Stdout, "hello-carbon") {
		t.Errorf("Stdout = %q, want it to contain %q", got.Stdout, "hello-carbon")
	}
	if got.ActualIntensity == nil || *got.ActualIntensity != 50 {
		t.Errorf("ActualIntensity = %v, want 50", got.ActualIntensity)
	}
	if got.BaselineIntensity == nil || *got.BaselineIntensity != 500 {
		t.Errorf("BaselineIntensity = %v, want 500", got.BaselineIntensity)
	}
	if got.ScheduledAt == nil || !got.ScheduledAt.Equal(now) {
		t.Errorf("ScheduledAt = %v, want %v", got.ScheduledAt, now)
	}
}

func TestDaemonTickDoesNotRunJobsScheduledInTheFuture(t *testing.T) {
	s, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	now := time.Now()
	future := now.Add(2 * time.Hour)
	_, err = s.Insert(job.Job{
		Command:  "echo should-not-run-yet",
		Zone:     "TEST",
		Earliest: now,
		Deadline: now.Add(3 * time.Hour),
		Duration: time.Second,
	})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}

	d := &Daemon{Store: s, Provider: fakeProvider{lowPointTime: future}}
	if err := d.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	all, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("expected 1 job, got %d", len(all))
	}
	if all[0].Status != job.StatusScheduled {
		t.Errorf("Status = %v, want scheduled (job should not run before its scheduled_at)", all[0].Status)
	}
}
