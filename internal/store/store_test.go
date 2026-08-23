package store

import (
	"testing"
	"time"

	"go-carbon-scheduler/internal/job"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestInsertAndPending(t *testing.T) {
	s := openTestStore(t)
	now := time.Now().Truncate(time.Second)

	id, err := s.Insert(job.Job{
		Command:  "echo hi",
		Zone:     "US-CAL-CISO",
		Earliest: now,
		Deadline: now.Add(time.Hour),
		Duration: 5 * time.Minute,
	})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if id == 0 {
		t.Fatal("expected non-zero id")
	}

	pending, err := s.Pending()
	if err != nil {
		t.Fatalf("Pending: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("expected 1 pending job, got %d", len(pending))
	}
	got := pending[0]
	if got.ID != id || got.Command != "echo hi" || got.Status != job.StatusPending {
		t.Errorf("unexpected job: %+v", got)
	}
	if got.Duration != 5*time.Minute {
		t.Errorf("Duration = %v, want 5m", got.Duration)
	}
}

func TestScheduleAndDueTransition(t *testing.T) {
	s := openTestStore(t)
	now := time.Now().Truncate(time.Second)

	id, err := s.Insert(job.Job{
		Command: "echo hi", Zone: "GB", Earliest: now, Deadline: now.Add(time.Hour), Duration: time.Minute,
	})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}

	future := now.Add(10 * time.Minute)
	if err := s.MarkScheduled(id, future, 400.0, 150.0); err != nil {
		t.Fatalf("MarkScheduled: %v", err)
	}

	if pending, _ := s.Pending(); len(pending) != 0 {
		t.Errorf("expected 0 pending after scheduling, got %d", len(pending))
	}

	// Not due yet — "now" is before the scheduled time.
	due, err := s.Due(now)
	if err != nil {
		t.Fatalf("Due: %v", err)
	}
	if len(due) != 0 {
		t.Errorf("expected 0 due jobs before scheduled_at, got %d", len(due))
	}

	// Due once we ask as-of the scheduled time.
	due, err = s.Due(future)
	if err != nil {
		t.Fatalf("Due: %v", err)
	}
	if len(due) != 1 {
		t.Fatalf("expected 1 due job, got %d", len(due))
	}
	got := due[0]
	if got.ScheduledAt == nil || !got.ScheduledAt.Equal(future) {
		t.Errorf("ScheduledAt = %v, want %v", got.ScheduledAt, future)
	}
	if got.BaselineIntensity == nil || *got.BaselineIntensity != 400.0 {
		t.Errorf("BaselineIntensity = %v, want 400.0", got.BaselineIntensity)
	}
	if got.ActualIntensity == nil || *got.ActualIntensity != 150.0 {
		t.Errorf("ActualIntensity = %v, want 150.0", got.ActualIntensity)
	}
}

func TestRunningAndFinishedLifecycle(t *testing.T) {
	s := openTestStore(t)
	now := time.Now().Truncate(time.Second)

	id, _ := s.Insert(job.Job{Command: "echo hi", Zone: "GB", Earliest: now, Deadline: now.Add(time.Hour), Duration: time.Minute})
	_ = s.MarkScheduled(id, now, 300, 200)

	if err := s.MarkRunning(id); err != nil {
		t.Fatalf("MarkRunning: %v", err)
	}
	if err := s.MarkFinished(id, job.StatusDone, 0, "hi\n", ""); err != nil {
		t.Fatalf("MarkFinished: %v", err)
	}

	all, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("expected 1 job, got %d", len(all))
	}
	got := all[0]
	if got.Status != job.StatusDone {
		t.Errorf("Status = %v, want done", got.Status)
	}
	if got.ExitCode == nil || *got.ExitCode != 0 {
		t.Errorf("ExitCode = %v, want 0", got.ExitCode)
	}
	if got.Stdout != "hi\n" {
		t.Errorf("Stdout = %q, want %q", got.Stdout, "hi\n")
	}
	if got.ExecutedAt == nil {
		t.Error("ExecutedAt should be set after MarkFinished")
	}
}
