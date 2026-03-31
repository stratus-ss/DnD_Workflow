package progress

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type ctxKey struct{}

// WithReporter stores the Reporter in the context for use by step implementations.
func WithReporter(ctx context.Context, r *Reporter) context.Context {
	return context.WithValue(ctx, ctxKey{}, r)
}

// FromContext retrieves the Reporter from context. Returns nil if absent.
func FromContext(ctx context.Context) *Reporter {
	r, _ := ctx.Value(ctxKey{}).(*Reporter)
	return r
}

// InputMetric describes the input size for a step, used for rate-based ETA estimation.
type InputMetric struct {
	Value float64 `json:"input_value"`
	Unit  string  `json:"input_unit"`
}

// ProgressState is the structure written to .progress.json.
type ProgressState struct {
	Step               string  `json:"step"`
	Status             string  `json:"status"`
	Health             string  `json:"health"`
	StartedAt          string  `json:"started_at,omitempty"`
	Heartbeat          string  `json:"heartbeat"`
	ProgressPct        float64 `json:"progress_pct"`
	EstimatedTotalSec  float64 `json:"estimated_total_sec"`
	EstimatedRemainSec float64 `json:"estimated_remaining_sec"`
}

// StepResult records the outcome of a single pipeline step for status.json.
type StepResult struct {
	Status     string  `json:"status"`
	ElapsedSec float64 `json:"elapsed_sec,omitempty"`
	Output     string  `json:"output,omitempty"`
	Error      string  `json:"error,omitempty"`
}

// PipelineStatus is the final status.json written on pipeline completion or failure.
type PipelineStatus struct {
	Status          string                 `json:"status"`
	Date            string                 `json:"date"`
	OutputDir       string                 `json:"output_dir"`
	TotalElapsedSec float64                `json:"total_elapsed_sec"`
	Steps           map[string]*StepResult `json:"steps"`
}

// BenchmarkStep records timing for one step in one run.
type BenchmarkStep struct {
	InputValue float64 `json:"input_value"`
	InputUnit  string  `json:"input_unit"`
	ElapsedSec float64 `json:"elapsed_sec"`
}

// BenchmarkRun records timing for all steps in one pipeline run.
type BenchmarkRun struct {
	Date  string                   `json:"date"`
	Steps map[string]BenchmarkStep `json:"steps"`
}

// BenchmarkHistory is the persistent benchmark data stored in .benchmarks.json.
type BenchmarkHistory struct {
	Runs []BenchmarkRun `json:"runs"`
}

const (
	progressFile      = ".progress.json"
	statusFile        = "status.json"
	benchmarksFile    = ".benchmarks.json"
	heartbeatInterval = 5 * time.Second
	stalledThreshold  = 60 * time.Second
)

// Reporter writes .progress.json for LLM monitoring and tracks benchmark history.
// All public methods are safe to call on a nil receiver (no-op).
type Reporter struct {
	mu            sync.Mutex
	sessionDir    string
	outputDir     string
	date          string
	seedRates     map[string]float64
	historyWindow int
	benchmarks    *BenchmarkHistory

	currentStep    string
	stepStart      time.Time
	estimatedSec   float64
	lastProgress   float64
	lastProgressAt time.Time

	heartbeatStop chan struct{}
	heartbeatDone chan struct{}

	stepResults   map[string]*StepResult
	pipelineStart time.Time
}

// New creates a Reporter. sessionDir is the per-date output directory for .progress.json
// and status.json. outputDir is the parent directory for .benchmarks.json.
func New(sessionDir, outputDir, date string, seedRates map[string]float64, historyWindow int) *Reporter {
	r := &Reporter{
		sessionDir:    sessionDir,
		outputDir:     outputDir,
		date:          date,
		seedRates:     seedRates,
		historyWindow: historyWindow,
		stepResults:   make(map[string]*StepResult),
		pipelineStart: time.Now(),
	}
	r.benchmarks = r.loadBenchmarks()
	return r
}

// EstimateStepDuration returns the estimated seconds for a step based on benchmark
// history (preferred) or seed rates from config (fallback).
func (r *Reporter) EstimateStepDuration(step string, metric InputMetric) float64 {
	if r == nil || metric.Value <= 0 {
		return 0
	}
	rate := r.benchmarks.averageRate(step, r.historyWindow)
	if rate <= 0 {
		rate = r.seedRates[step]
	}
	if rate <= 0 {
		return 0
	}
	return metric.Value * rate
}

// StartStep begins tracking a new step with optional ETA estimate.
// Spawns a background heartbeat goroutine that writes .progress.json every 5s.
func (r *Reporter) StartStep(name string, estimatedSec float64) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	r.currentStep = name
	r.stepStart = time.Now()
	r.estimatedSec = estimatedSec
	r.lastProgress = 0
	r.lastProgressAt = time.Time{}
	r.stepResults[name] = &StepResult{Status: "running"}
	r.writeProgressLocked()
	r.startHeartbeat()
}

// UpdateProgress sets the fractional completion (0.0–1.0) for the current step.
// Called from polling loops that receive server-side progress (e.g. whisper).
func (r *Reporter) UpdateProgress(pct float64) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	if pct != r.lastProgress {
		r.lastProgress = pct
		r.lastProgressAt = time.Now()
	}
	r.writeProgressLocked()
}

// Heartbeat writes a fresh .progress.json without changing progress_pct.
// Called from polling loops that have no fractional progress (e.g. TTS).
func (r *Reporter) Heartbeat() {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.writeProgressLocked()
}

// CompleteStep marks the current step as complete and records benchmark data.
func (r *Reporter) CompleteStep(metric InputMetric, outputFile string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	r.stopHeartbeatLocked()
	elapsed := time.Since(r.stepStart).Seconds()
	r.stepResults[r.currentStep] = &StepResult{
		Status:     "complete",
		ElapsedSec: elapsed,
		Output:     outputFile,
	}
	if metric.Value > 0 {
		r.recordBenchmark(r.currentStep, metric, elapsed)
	}
	r.lastProgress = 1.0
	r.writeProgressLocked()
}

// FailStep marks the current step as failed with the given error.
func (r *Reporter) FailStep(err error) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	r.stopHeartbeatLocked()
	elapsed := time.Since(r.stepStart).Seconds()
	r.stepResults[r.currentStep] = &StepResult{
		Status:     "failed",
		ElapsedSec: elapsed,
		Error:      err.Error(),
	}
	state := &ProgressState{
		Step:      r.currentStep,
		Status:    "failed",
		Health:    "failed",
		StartedAt: r.stepStart.UTC().Format(time.RFC3339),
		Heartbeat: time.Now().UTC().Format(time.RFC3339),
	}
	r.writeStateFile(state)
}

// SkipStep records that a step was skipped due to existing output (checkpoint).
func (r *Reporter) SkipStep(name string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	r.stepResults[name] = &StepResult{Status: "skipped"}
	state := &ProgressState{
		Step:      name,
		Status:    "skipped",
		Health:    "skipped",
		Heartbeat: time.Now().UTC().Format(time.RFC3339),
	}
	r.writeStateFile(state)
}

// WriteStatus writes the final status.json summarising the entire pipeline run.
func (r *Reporter) WriteStatus(pipelineErr error) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	status := "complete"
	if pipelineErr != nil {
		status = "failed"
	}
	ps := &PipelineStatus{
		Status:          status,
		Date:            r.date,
		OutputDir:       r.sessionDir,
		TotalElapsedSec: time.Since(r.pipelineStart).Seconds(),
		Steps:           r.stepResults,
	}
	data, err := json.MarshalIndent(ps, "", "  ")
	if err != nil {
		return
	}
	path := filepath.Join(r.sessionDir, statusFile)
	os.WriteFile(path, data, 0o644)
}

func (r *Reporter) startHeartbeat() {
	r.heartbeatStop = make(chan struct{})
	r.heartbeatDone = make(chan struct{})

	go func() {
		defer close(r.heartbeatDone)
		ticker := time.NewTicker(heartbeatInterval)
		defer ticker.Stop()

		for {
			select {
			case <-r.heartbeatStop:
				return
			case <-ticker.C:
				r.mu.Lock()
				r.writeProgressLocked()
				r.mu.Unlock()
			}
		}
	}()
}

func (r *Reporter) stopHeartbeatLocked() {
	if r.heartbeatStop == nil {
		return
	}
	close(r.heartbeatStop)
	// Release lock so the goroutine can finish its current write, then wait for exit.
	r.mu.Unlock()
	<-r.heartbeatDone
	r.mu.Lock()
	r.heartbeatStop = nil
	r.heartbeatDone = nil
}

func (r *Reporter) writeProgressLocked() {
	r.writeStateFile(r.buildState())
}

func (r *Reporter) buildState() *ProgressState {
	now := time.Now()

	var elapsed float64
	var startedAt string
	if !r.stepStart.IsZero() {
		elapsed = now.Sub(r.stepStart).Seconds()
		startedAt = r.stepStart.UTC().Format(time.RFC3339)
	}

	estimatedTotal := r.estimatedSec
	if r.lastProgress >= 0.1 && r.lastProgress < 1.0 {
		estimatedTotal = elapsed / r.lastProgress
	}

	remaining := estimatedTotal - elapsed
	if remaining < 0 {
		remaining = 0
	}

	status := "running"
	if result, ok := r.stepResults[r.currentStep]; ok && result.Status != "running" {
		status = result.Status
	}

	return &ProgressState{
		Step:               r.currentStep,
		Status:             status,
		Health:             r.computeHealth(elapsed),
		StartedAt:          startedAt,
		Heartbeat:          now.UTC().Format(time.RFC3339),
		ProgressPct:        r.lastProgress,
		EstimatedTotalSec:  estimatedTotal,
		EstimatedRemainSec: remaining,
	}
}

func (r *Reporter) computeHealth(elapsedSec float64) string {
	if result, ok := r.stepResults[r.currentStep]; ok {
		switch result.Status {
		case "complete":
			return "completed"
		case "failed":
			return "failed"
		case "skipped":
			return "skipped"
		}
	}

	if r.lastProgress > 0 && !r.lastProgressAt.IsZero() {
		if time.Since(r.lastProgressAt) > stalledThreshold {
			return "stalled"
		}
	}

	if r.estimatedSec > 0 && r.lastProgress > 0 {
		expectedPct := elapsedSec / r.estimatedSec
		if expectedPct > 0.1 && r.lastProgress < expectedPct*0.6 {
			return "slow"
		}
	}

	if r.estimatedSec > 0 && r.lastProgress == 0 && elapsedSec > r.estimatedSec*1.2 {
		return "slow"
	}

	return "healthy"
}

func (r *Reporter) writeStateFile(state *ProgressState) {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return
	}
	path := filepath.Join(r.sessionDir, progressFile)
	os.WriteFile(path, data, 0o644)
}

// Benchmark persistence

func (r *Reporter) loadBenchmarks() *BenchmarkHistory {
	path := filepath.Join(r.outputDir, benchmarksFile)
	data, err := os.ReadFile(path)
	if err != nil {
		return &BenchmarkHistory{}
	}
	var h BenchmarkHistory
	if err := json.Unmarshal(data, &h); err != nil {
		return &BenchmarkHistory{}
	}
	return &h
}

func (r *Reporter) recordBenchmark(step string, metric InputMetric, elapsedSec float64) {
	var run *BenchmarkRun
	for i := range r.benchmarks.Runs {
		if r.benchmarks.Runs[i].Date == r.date {
			run = &r.benchmarks.Runs[i]
			break
		}
	}
	if run == nil {
		r.benchmarks.Runs = append(r.benchmarks.Runs, BenchmarkRun{
			Date:  r.date,
			Steps: make(map[string]BenchmarkStep),
		})
		run = &r.benchmarks.Runs[len(r.benchmarks.Runs)-1]
	}
	run.Steps[step] = BenchmarkStep{
		InputValue: metric.Value,
		InputUnit:  metric.Unit,
		ElapsedSec: elapsedSec,
	}
	r.saveBenchmarks()
}

func (r *Reporter) saveBenchmarks() {
	data, err := json.MarshalIndent(r.benchmarks, "", "  ")
	if err != nil {
		return
	}
	path := filepath.Join(r.outputDir, benchmarksFile)
	os.WriteFile(path, data, 0o644)
}

func (h *BenchmarkHistory) averageRate(step string, window int) float64 {
	var rates []float64
	for i := len(h.Runs) - 1; i >= 0 && len(rates) < window; i-- {
		s, ok := h.Runs[i].Steps[step]
		if !ok || s.InputValue <= 0 {
			continue
		}
		rates = append(rates, s.ElapsedSec/s.InputValue)
	}
	if len(rates) == 0 {
		return 0
	}
	sum := 0.0
	for _, r := range rates {
		sum += r
	}
	return sum / float64(len(rates))
}
