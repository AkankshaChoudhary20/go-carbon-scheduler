// Command carbon-cron submits deferrable jobs and runs the daemon that
// schedules them into the lowest-carbon window before their deadline.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"go-carbon-scheduler/internal/carbon"
	"go-carbon-scheduler/internal/job"
	"go-carbon-scheduler/internal/scheduler"
	"go-carbon-scheduler/internal/store"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	var err error
	switch os.Args[1] {
	case "submit":
		err = runSubmit(os.Args[2:])
	case "list":
		err = runList(os.Args[2:])
	case "report":
		err = runReport(os.Args[2:])
	case "daemon":
		err = runDaemon(os.Args[2:])
	case "-h", "--help", "help":
		usage()
		return
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n\n", os.Args[1])
		usage()
		os.Exit(1)
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `carbon-cron — carbon-aware job scheduler

Usage:
  carbon-cron submit --command "<shell command>" [--zone ZONE] [--deadline 24h] [--duration 5m] [--earliest 0s]
  carbon-cron list
  carbon-cron report
  carbon-cron daemon [--poll 30s]

Environment:
  CARBON_CRON_DB              path to the SQLite database (default: ./carbon-cron.db)
  CARBON_CRON_PROVIDER        "mock" (default) or "electricitymaps"
  ELECTRICITYMAPS_API_TOKEN   required when CARBON_CRON_PROVIDER=electricitymaps
`)
}

func dbPath() string {
	if p := os.Getenv("CARBON_CRON_DB"); p != "" {
		return p
	}
	return "carbon-cron.db"
}

func openStore() (*store.Store, error) {
	path := dbPath()
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create db dir: %w", err)
		}
	}
	return store.Open(path)
}

func runSubmit(args []string) error {
	fs := flag.NewFlagSet("submit", flag.ExitOnError)
	command := fs.String("command", "", "shell command to run (required)")
	zone := fs.String("zone", "US-CAL-CISO", "carbon-intensity zone code")
	deadline := fs.Duration("deadline", 24*time.Hour, "job must finish within this long from now")
	duration := fs.Duration("duration", 5*time.Minute, "estimated time the job takes to run")
	earliest := fs.Duration("earliest", 0, "don't start the job before this long from now")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *command == "" {
		return fmt.Errorf("--command is required")
	}

	s, err := openStore()
	if err != nil {
		return err
	}
	defer s.Close()

	now := time.Now()
	id, err := s.Insert(job.Job{
		Command:  *command,
		Zone:     *zone,
		Earliest: now.Add(*earliest),
		Deadline: now.Add(*deadline),
		Duration: *duration,
	})
	if err != nil {
		return err
	}

	fmt.Printf("submitted job %d — the daemon will pick the lowest-carbon time before %s\n",
		id, now.Add(*deadline).Format(time.RFC3339))
	return nil
}

func runList(args []string) error {
	fs := flag.NewFlagSet("list", flag.ExitOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}

	s, err := openStore()
	if err != nil {
		return err
	}
	defer s.Close()

	jobs, err := s.List()
	if err != nil {
		return err
	}
	if len(jobs) == 0 {
		fmt.Println("no jobs yet — run `carbon-cron submit` to add one")
		return nil
	}

	fmt.Printf("%-4s %-10s %-8s %-25s %s\n", "ID", "STATUS", "EXIT", "SCHEDULED", "COMMAND")
	for _, j := range jobs {
		scheduled := "-"
		if j.ScheduledAt != nil {
			scheduled = j.ScheduledAt.Format(time.RFC3339)
		}
		exit := "-"
		if j.ExitCode != nil {
			exit = fmt.Sprintf("%d", *j.ExitCode)
		}
		fmt.Printf("%-4d %-10s %-8s %-25s %s\n", j.ID, j.Status, exit, scheduled, truncate(j.Command, 40))
	}
	return nil
}

func runReport(args []string) error {
	fs := flag.NewFlagSet("report", flag.ExitOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}

	s, err := openStore()
	if err != nil {
		return err
	}
	defer s.Close()

	jobs, err := s.List()
	if err != nil {
		return err
	}

	var total float64
	var counted int
	fmt.Printf("%-4s %-10s %-16s %-16s %s\n", "ID", "STATUS", "PEAK gCO2/kWh", "RUN gCO2/kWh", "SAVED (delta)")
	for _, j := range jobs {
		delta, ok := j.IntensityDelta()
		if !ok {
			continue
		}
		counted++
		total += delta
		fmt.Printf("%-4d %-10s %-16.0f %-16.0f %+.0f\n", j.ID, j.Status, *j.BaselineIntensity, *j.ActualIntensity, delta)
	}
	if counted == 0 {
		fmt.Println("no scheduled jobs yet")
		return nil
	}
	fmt.Printf("\n%d job(s) scheduled — average carbon intensity was %.0f gCO2/kWh lower than the window's peak\n", counted, total/float64(counted))
	fmt.Println("(this is an intensity delta, not an absolute CO2 mass — multiply by each job's real power draw and duration for an actual gCO2e figure)")
	return nil
}

func runDaemon(args []string) error {
	fs := flag.NewFlagSet("daemon", flag.ExitOnError)
	poll := fs.Duration("poll", 30*time.Second, "how often to check for jobs to schedule/run")
	if err := fs.Parse(args); err != nil {
		return err
	}

	s, err := openStore()
	if err != nil {
		return err
	}
	defer s.Close()

	provider, err := buildProvider()
	if err != nil {
		return err
	}

	logger := log.New(os.Stdout, "", log.LstdFlags)
	d := &scheduler.Daemon{Store: s, Provider: provider, Poll: *poll, Logger: logger}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	logger.Printf("carbon-cron daemon starting (poll=%s, db=%s)", *poll, dbPath())
	err = d.Run(ctx)
	if err == context.Canceled {
		logger.Println("shutting down")
		return nil
	}
	return err
}

func buildProvider() (carbon.Provider, error) {
	switch os.Getenv("CARBON_CRON_PROVIDER") {
	case "electricitymaps":
		token := os.Getenv("ELECTRICITYMAPS_API_TOKEN")
		if token == "" {
			return nil, fmt.Errorf("ELECTRICITYMAPS_API_TOKEN must be set when CARBON_CRON_PROVIDER=electricitymaps")
		}
		return carbon.ElectricityMapsProvider{APIToken: token, HTTPClient: &http.Client{Timeout: 15 * time.Second}}, nil
	default:
		return carbon.MockProvider{}, nil
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
