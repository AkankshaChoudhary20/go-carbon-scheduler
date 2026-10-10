# go-carbon-scheduler

`carbon-cron` — a cron-like scheduler for jobs that don't need to run *right
now*, only *before some deadline*. It shifts them into the lowest-carbon
window it can find within that deadline, using real (or synthetic) grid
carbon-intensity forecasts.

## Why this exists

Grid carbon intensity swings 2–4x over a typical day depending on how much
wind/solar is on the grid at that moment. A lot of real workloads — CI jobs,
batch ETL, backups, report generation, model training — don't care exactly
when they run, only that they finish in time. Shifting them into the
cleanest window inside their deadline cuts real emissions for free. This is
the same idea behind Google's and Microsoft's internal carbon-aware
scheduling and the Green Software Foundation's Carbon Aware SDK, packaged
here as a small Go binary you can point at your own jobs today.

## How it works

```
cmd/carbon-cron/        CLI: submit / list / report / daemon
internal/carbon/        Pluggable carbon-intensity forecast providers
  provider.go              Provider interface
  mock.go                  Deterministic synthetic diurnal curve (default — no API key needed)
  electricitymaps.go       Real provider: ElectricityMaps API
internal/scheduler/     The scheduling algorithm + daemon loop
  window.go                Picks the start time whose [start, start+duration)
                            window has the lowest *average* forecasted
                            intensity — not just the single lowest point,
                            which matters once a job's duration spans
                            several forecast samples
  daemon.go                Poll loop: schedules pending jobs, executes due ones
internal/store/          SQLite-backed job persistence (survives restarts)
internal/job/             Job type and status lifecycle
```

**Job lifecycle:** `pending` → (daemon fetches a forecast and picks a start
time) → `scheduled` → (start time arrives) → `running` → `done` / `failed`.

**Baseline comparison:** for every scheduled job, the daemon also records the
*peak* forecasted intensity anywhere in the job's `[earliest, deadline]`
window — the "if you'd just run it immediately at the worst moment"
baseline — so `report` can show how much lower the chosen window actually
was.

## Quick start

```bash
go build -o carbon-cron ./cmd/carbon-cron

# Submit a job that must finish within 24h and takes ~30 minutes.
# With no provider configured, this uses the built-in mock forecast —
# no API key needed to try the whole thing end to end.
./carbon-cron submit --command "python train.py" --deadline 24h --duration 30m

./carbon-cron list

# In another terminal (or the background): the daemon schedules pending
# jobs and executes ones whose time has come.
./carbon-cron daemon
```

Real run against the mock provider, 24h deadline, 30-minute job:

```
job 1: scheduled for 2026-08-24T02:43:21+05:30 (window avg 195 gCO2/kWh vs peak 495 gCO2/kWh)
```

— picked 2:43am (the mock's synthetic overnight trough) over an average day
that peaks at 495 gCO2/kWh, a ~60% intensity reduction for a job that didn't
have to run at any particular time anyway.

```bash
./carbon-cron report
```

```
ID   STATUS     PEAK gCO2/kWh    RUN gCO2/kWh     SAVED (delta)
1    done       495              195              +300

1 job(s) scheduled — average carbon intensity was 300 gCO2/kWh lower than the window's peak
```

## Using real carbon data

The mock provider is the default so the whole tool works out of the box.
For real grid data, get a free API token from
[ElectricityMaps](https://app.electricitymaps.com/api) and set:

```bash
export CARBON_CRON_PROVIDER=electricitymaps
export ELECTRICITYMAPS_API_TOKEN=your-token-here
./carbon-cron submit --command "..." --zone US-CAL-CISO --deadline 24h --duration 30m
./carbon-cron daemon
```

Zone codes (`US-CAL-CISO`, `DE`, `GB`, ...) come from ElectricityMaps'
[zone list](https://app.electricitymaps.com/map). Note their API has moved
through several versions (v3 → v4) — if `daemon` logs a forecast parsing
error, check the current response shape at
[app.electricitymaps.com/api/docs/reference](https://app.electricitymaps.com/api/docs/reference)
against `internal/carbon/electricitymaps.go`'s `electricityMapsResponse`
struct; the field names may have shifted since this was written.

## CLI reference

| Command | Flags | Description |
| --- | --- | --- |
| `submit` | `--command` (required), `--zone`, `--deadline`, `--duration`, `--earliest` | Add a job. Deadline/duration/earliest are Go duration strings (`24h`, `30m`) relative to now. |
| `list` | — | Show all jobs and their status. |
| `report` | — | Show per-job and average carbon-intensity savings for scheduled/done jobs. |
| `daemon` | `--poll` (default `30s`) | Run the scheduler loop. Schedules pending jobs, executes due ones. |

## Environment variables

| Var | Default | Purpose |
| --- | --- | --- |
| `CARBON_CRON_DB` | `./carbon-cron.db` | SQLite database path |
| `CARBON_CRON_PROVIDER` | `mock` | `mock` or `electricitymaps` |
| `ELECTRICITYMAPS_API_TOKEN` | — | Required when provider is `electricitymaps` |

## What "saved" means here

The report shows a **carbon-intensity delta** (gCO2/kWh), not an absolute
CO2 mass — this tool doesn't know each job's actual power draw. To get real
gCO2e saved, multiply the delta by the job's measured energy consumption
(kWh) for its duration. The delta figure is still directly meaningful on its
own: it's exactly how much cleaner the electricity was at the chosen time
versus the worst moment in the job's deadline window.

## Testing

```bash
go test ./...
```

Covers: mock-provider determinism and diurnal shape, ElectricityMaps
response parsing against a fixture (via `httptest`, no live API calls), the
scheduling algorithm against synthetic forecasts with known-correct answers
(including multi-point window averaging and deadline-too-tight fallback),
full SQLite store round-trips, and an end-to-end daemon test that submits a
job, ticks the scheduler, and asserts it actually executed a real
subprocess.

## Possible extensions

- **Kubernetes-native mode**: a `CarbonAwareCronJob` CRD + controller
  wrapping standard `CronJob`s, so carbon-aware scheduling applies to
  existing k8s batch workloads without changing how they're defined.
- **Interruptible jobs**: allow a job to be preempted and rescheduled if a
  much cleaner window opens up before its original scheduled time.
- **Multi-region placement**: for jobs that can run in more than one region,
  pick both the region and the time that minimizes carbon intensity.

The mock forecast rejects ranges whose end precedes their start and honors context cancellation. Equal start/end times produce one sample.
