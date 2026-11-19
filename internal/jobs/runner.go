// Package jobs runs the daily pipeline (spec §16.1): fetch market data,
// persist it, then run the evaluators that raise and resolve alerts. Every
// step is idempotent (upserts), a failing step raises data_fetch_failed
// without stopping the others, and each run is logged in job_runs.
package jobs

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/lucasgoveia/stock-portfolio/internal/alerts"
	"github.com/lucasgoveia/stock-portfolio/internal/store/db"
)

// JobDaily is the job name of the daily pipeline.
const JobDaily = "daily"

// AlertFetchFailed is raised per failing step (variant = step name).
const AlertFetchFailed = "data_fetch_failed"

// Step statuses.
const (
	StepOK      = "ok"
	StepFailed  = "failed"
	StepSkipped = "skipped"
)

// Run statuses.
const (
	RunOK      = "ok"
	RunPartial = "partial"
	RunFailed  = "failed"
)

// ErrSkipped marks a step that had nothing to do (missing config, no assets).
var ErrSkipped = errors.New("skipped")

// ErrRunning is returned when a run is already in progress.
var ErrRunning = errors.New("daily job already running")

// ErrNoRuns is returned by Last before the first run.
var ErrNoRuns = errors.New("no job runs yet")

// Step is one pipeline stage. Run returns a short note for the log.
type Step struct {
	Name string
	Run  func(ctx context.Context, day time.Time) (string, error)
}

// StepResult is the outcome of one step.
type StepResult struct {
	Name       string `json:"name"`
	Status     string `json:"status"`
	Note       string `json:"note,omitempty"`
	Error      string `json:"error,omitempty"`
	DurationMS int64  `json:"duration_ms"`
}

// RunResult is one logged execution.
type RunResult struct {
	ID         int64        `json:"id"`
	Job        string       `json:"job"`
	RunDate    string       `json:"run_date"`
	StartedAt  string       `json:"started_at"`
	FinishedAt string       `json:"finished_at,omitempty"`
	Status     string       `json:"status"`
	Steps      []StepResult `json:"steps"`
}

// Runner executes the steps in order, one run at a time.
type Runner struct {
	q      *db.Queries
	alerts *alerts.Service
	steps  []Step
	now    func() time.Time
	mu     sync.Mutex
}

// NewRunner builds a Runner. A nil clock uses time.Now.
func NewRunner(conn db.DBTX, al *alerts.Service, steps []Step, clock func() time.Time) *Runner {
	if clock == nil {
		clock = time.Now
	}
	return &Runner{q: db.New(conn), alerts: al, steps: steps, now: clock}
}

const tsLayout = "2006-01-02T15:04:05Z"

// Run executes every step for today's date in America/Sao_Paulo.
func (r *Runner) Run(ctx context.Context) (RunResult, error) {
	if !r.mu.TryLock() {
		return RunResult{}, ErrRunning
	}
	defer r.mu.Unlock()

	start := r.now()
	day := civilDay(start.In(SaoPaulo()))
	row, err := r.q.CreateJobRun(ctx, db.CreateJobRunParams{
		Job: JobDaily, RunDate: day.Format(dateLayout), StartedAt: start.UTC().Format(tsLayout),
	})
	if err != nil {
		return RunResult{}, fmt.Errorf("log job run: %w", err)
	}
	res := RunResult{ID: row.ID, Job: row.Job, RunDate: row.RunDate, StartedAt: row.StartedAt}
	var failedKeys []string
	failed := 0
	for _, st := range r.steps {
		t0 := r.now()
		note, err := st.Run(ctx, day)
		sr := StepResult{Name: st.Name, Status: StepOK, Note: note, DurationMS: r.now().Sub(t0).Milliseconds()}
		switch {
		case errors.Is(err, ErrSkipped):
			sr.Status = StepSkipped
		case err != nil:
			sr.Status, sr.Error = StepFailed, summarize(err)
			failed++
			slog.Warn("daily job step failed", "step", st.Name, "err", err)
			if r.alerts != nil {
				if _, aerr := r.alerts.Upsert(ctx, AlertFetchFailed, nil, st.Name, map[string]any{
					"step": st.Name, "error": summarize(err), "run_id": row.ID, "run_date": row.RunDate,
				}); aerr != nil {
					return res, aerr
				}
				failedKeys = append(failedKeys, alerts.Key(AlertFetchFailed, nil, st.Name))
			}
		}
		res.Steps = append(res.Steps, sr)
	}
	if r.alerts != nil {
		if _, err := r.alerts.ResolveMissing(ctx, AlertFetchFailed, failedKeys); err != nil {
			return res, err
		}
	}
	switch {
	case failed == 0:
		res.Status = RunOK
	case failed == len(r.steps):
		res.Status = RunFailed
	default:
		res.Status = RunPartial
	}
	res.FinishedAt = r.now().UTC().Format(tsLayout)
	steps, err := json.Marshal(res.Steps)
	if err != nil {
		return res, err
	}
	if err := r.q.FinishJobRun(ctx, db.FinishJobRunParams{
		FinishedAt: sql.NullString{String: res.FinishedAt, Valid: true}, Status: res.Status,
		Steps: string(steps), ID: row.ID,
	}); err != nil {
		return res, fmt.Errorf("finish job run: %w", err)
	}
	return res, nil
}

// Last returns the most recent run.
func (r *Runner) Last(ctx context.Context) (RunResult, error) {
	row, err := r.q.LastJobRun(ctx, JobDaily)
	if errors.Is(err, sql.ErrNoRows) {
		return RunResult{}, ErrNoRuns
	}
	if err != nil {
		return RunResult{}, fmt.Errorf("last job run: %w", err)
	}
	res := RunResult{ID: row.ID, Job: row.Job, RunDate: row.RunDate, StartedAt: row.StartedAt,
		FinishedAt: row.FinishedAt.String, Status: row.Status, Steps: []StepResult{}}
	_ = json.Unmarshal([]byte(row.Steps), &res.Steps)
	return res, nil
}

// Schedule runs the pipeline every day at "HH:MM" America/Sao_Paulo until
// ctx is done.
func (r *Runner) Schedule(ctx context.Context, at string) error {
	hh, mm, err := parseClock(at)
	if err != nil {
		return err
	}
	go func() {
		for {
			wait := NextRun(r.now(), hh, mm).Sub(r.now())
			t := time.NewTimer(wait)
			select {
			case <-ctx.Done():
				t.Stop()
				return
			case <-t.C:
			}
			if res, err := r.Run(ctx); err != nil {
				slog.Error("daily job", "err", err)
			} else {
				slog.Info("daily job", "status", res.Status, "run_id", res.ID)
			}
		}
	}()
	return nil
}

func parseClock(at string) (int, int, error) {
	t, err := time.Parse("15:04", strings.TrimSpace(at))
	if err != nil {
		return 0, 0, fmt.Errorf("daily_job_at must be HH:MM: %w", err)
	}
	return t.Hour(), t.Minute(), nil
}

// NextRun returns the next hh:mm in America/Sao_Paulo strictly after now.
func NextRun(now time.Time, hh, mm int) time.Time {
	loc := SaoPaulo()
	n := now.In(loc)
	next := time.Date(n.Year(), n.Month(), n.Day(), hh, mm, 0, 0, loc)
	if !next.After(n) {
		next = next.AddDate(0, 0, 1)
	}
	return next
}

// maxStepError caps a stored step error: a readable summary, never a body.
const maxStepError = 300

func summarize(err error) string {
	s := strings.Join(strings.Fields(err.Error()), " ")
	if r := []rune(s); len(r) > maxStepError {
		return string(r[:maxStepError-1]) + "…"
	}
	return s
}
