CREATE TABLE IF NOT EXISTS jobs (
  id                  INTEGER PRIMARY KEY AUTOINCREMENT,
  command             TEXT NOT NULL,
  zone                TEXT NOT NULL,
  earliest            TEXT NOT NULL,
  deadline            TEXT NOT NULL,
  duration_seconds    INTEGER NOT NULL,
  status              TEXT NOT NULL DEFAULT 'pending',
  scheduled_at        TEXT,
  executed_at         TEXT,
  exit_code           INTEGER,
  stdout              TEXT,
  stderr              TEXT,
  baseline_intensity  REAL,
  actual_intensity    REAL,
  created_at          TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_jobs_status ON jobs(status);
