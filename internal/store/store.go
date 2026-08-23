// Package store persists jobs to SQLite so the daemon survives restarts.
package store

import (
	"database/sql"
	_ "embed"
	"fmt"
	"time"

	"go-carbon-scheduler/internal/job"

	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schema string

const timeLayout = time.RFC3339Nano

type Store struct {
	db *sql.DB
}

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("store: open %s: %w", path, err)
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("store: apply schema: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

// Insert adds a new pending job and returns its ID.
func (s *Store) Insert(j job.Job) (int64, error) {
	res, err := s.db.Exec(
		`INSERT INTO jobs (command, zone, earliest, deadline, duration_seconds, status, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		j.Command, j.Zone, j.Earliest.Format(timeLayout), j.Deadline.Format(timeLayout),
		int64(j.Duration.Seconds()), job.StatusPending, time.Now().Format(timeLayout),
	)
	if err != nil {
		return 0, fmt.Errorf("store: insert job: %w", err)
	}
	return res.LastInsertId()
}

// Pending returns jobs that haven't been assigned a scheduled_at yet.
func (s *Store) Pending() ([]job.Job, error) {
	return s.query(`WHERE status = ?`, job.StatusPending)
}

// Due returns scheduled jobs whose scheduled_at has passed and are ready to run.
func (s *Store) Due(now time.Time) ([]job.Job, error) {
	return s.query(`WHERE status = ? AND scheduled_at <= ?`, job.StatusScheduled, now.Format(timeLayout))
}

// List returns every job, most recently created first.
func (s *Store) List() ([]job.Job, error) {
	return s.query(`ORDER BY created_at DESC`)
}

// MarkScheduled records the chosen start time and the intensity figures
// used to compute it.
func (s *Store) MarkScheduled(id int64, scheduledAt time.Time, baseline, actual float64) error {
	_, err := s.db.Exec(
		`UPDATE jobs SET status = ?, scheduled_at = ?, baseline_intensity = ?, actual_intensity = ? WHERE id = ?`,
		job.StatusScheduled, scheduledAt.Format(timeLayout), baseline, actual, id,
	)
	if err != nil {
		return fmt.Errorf("store: mark scheduled %d: %w", id, err)
	}
	return nil
}

// MarkRunning flips a due job to running just before exec starts.
func (s *Store) MarkRunning(id int64) error {
	_, err := s.db.Exec(`UPDATE jobs SET status = ? WHERE id = ?`, job.StatusRunning, id)
	if err != nil {
		return fmt.Errorf("store: mark running %d: %w", id, err)
	}
	return nil
}

// MarkFinished records the outcome of a completed run.
func (s *Store) MarkFinished(id int64, status job.Status, exitCode int, stdout, stderr string) error {
	_, err := s.db.Exec(
		`UPDATE jobs SET status = ?, executed_at = ?, exit_code = ?, stdout = ?, stderr = ? WHERE id = ?`,
		status, time.Now().Format(timeLayout), exitCode, stdout, stderr, id,
	)
	if err != nil {
		return fmt.Errorf("store: mark finished %d: %w", id, err)
	}
	return nil
}

func (s *Store) query(whereAndArgs ...any) ([]job.Job, error) {
	clause := ""
	var args []any
	if len(whereAndArgs) > 0 {
		clause = whereAndArgs[0].(string)
		args = whereAndArgs[1:]
	}

	rows, err := s.db.Query(`
		SELECT id, command, zone, earliest, deadline, duration_seconds, status,
		       scheduled_at, executed_at, exit_code, stdout, stderr,
		       baseline_intensity, actual_intensity, created_at
		FROM jobs `+clause, args...)
	if err != nil {
		return nil, fmt.Errorf("store: query: %w", err)
	}
	defer rows.Close()

	var jobs []job.Job
	for rows.Next() {
		var (
			j                                  job.Job
			earliest, deadline, createdAt      string
			durationSeconds                    int64
			scheduledAt, executedAt            sql.NullString
			exitCode                           sql.NullInt64
			stdout, stderr                     sql.NullString
			baselineIntensity, actualIntensity sql.NullFloat64
			status                             string
		)
		if err := rows.Scan(
			&j.ID, &j.Command, &j.Zone, &earliest, &deadline, &durationSeconds, &status,
			&scheduledAt, &executedAt, &exitCode, &stdout, &stderr,
			&baselineIntensity, &actualIntensity, &createdAt,
		); err != nil {
			return nil, fmt.Errorf("store: scan row: %w", err)
		}

		j.Status = job.Status(status)
		j.Duration = time.Duration(durationSeconds) * time.Second
		if j.Earliest, err = time.Parse(timeLayout, earliest); err != nil {
			return nil, fmt.Errorf("store: parse earliest: %w", err)
		}
		if j.Deadline, err = time.Parse(timeLayout, deadline); err != nil {
			return nil, fmt.Errorf("store: parse deadline: %w", err)
		}
		if j.CreatedAt, err = time.Parse(timeLayout, createdAt); err != nil {
			return nil, fmt.Errorf("store: parse created_at: %w", err)
		}
		if scheduledAt.Valid {
			t, err := time.Parse(timeLayout, scheduledAt.String)
			if err != nil {
				return nil, fmt.Errorf("store: parse scheduled_at: %w", err)
			}
			j.ScheduledAt = &t
		}
		if executedAt.Valid {
			t, err := time.Parse(timeLayout, executedAt.String)
			if err != nil {
				return nil, fmt.Errorf("store: parse executed_at: %w", err)
			}
			j.ExecutedAt = &t
		}
		if exitCode.Valid {
			v := int(exitCode.Int64)
			j.ExitCode = &v
		}
		if stdout.Valid {
			j.Stdout = stdout.String
		}
		if stderr.Valid {
			j.Stderr = stderr.String
		}
		if baselineIntensity.Valid {
			v := baselineIntensity.Float64
			j.BaselineIntensity = &v
		}
		if actualIntensity.Valid {
			v := actualIntensity.Float64
			j.ActualIntensity = &v
		}

		jobs = append(jobs, j)
	}
	return jobs, rows.Err()
}
