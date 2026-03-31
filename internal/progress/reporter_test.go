package progress

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func newTestReporter(t *testing.T) (*Reporter, string) {
	t.Helper()
	dir := t.TempDir()
	sessionDir := filepath.Join(dir, "2026-03-15")
	os.MkdirAll(sessionDir, 0o755)

	seeds := map[string]float64{
		"whisper": 0.3,
		"tts":     0.06,
		"audio":   0.25,
	}
	return New(sessionDir, dir, "2026-03-15", seeds, 5), sessionDir
}

func readProgress(t *testing.T, sessionDir string) *ProgressState {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(sessionDir, progressFile))
	if err != nil {
		t.Fatalf("read progress: %v", err)
	}
	var state ProgressState
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatalf("unmarshal progress: %v", err)
	}
	return &state
}

func readStatus(t *testing.T, sessionDir string) *PipelineStatus {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(sessionDir, statusFile))
	if err != nil {
		t.Fatalf("read status: %v", err)
	}
	var ps PipelineStatus
	if err := json.Unmarshal(data, &ps); err != nil {
		t.Fatalf("unmarshal status: %v", err)
	}
	return &ps
}

func TestStartAndCompleteStep(t *testing.T) {
	rep, sessionDir := newTestReporter(t)

	metric := InputMetric{Value: 3600, Unit: "audio_sec"}
	rep.StartStep("whisper", 1080)
	rep.UpdateProgress(0.5)
	rep.CompleteStep(metric, "transcript.srt.txt")

	state := readProgress(t, sessionDir)
	if state.Step != "whisper" {
		t.Errorf("step = %q, want whisper", state.Step)
	}
	if state.Status != "complete" {
		t.Errorf("status = %q, want complete", state.Status)
	}
	if state.ProgressPct != 1.0 {
		t.Errorf("progress = %f, want 1.0", state.ProgressPct)
	}
	if state.Health != "completed" {
		t.Errorf("health = %q, want completed", state.Health)
	}
}

func TestSkipStep(t *testing.T) {
	rep, sessionDir := newTestReporter(t)

	rep.SkipStep("whisper")

	state := readProgress(t, sessionDir)
	if state.Status != "skipped" {
		t.Errorf("status = %q, want skipped", state.Status)
	}
	if state.Health != "skipped" {
		t.Errorf("health = %q, want skipped", state.Health)
	}
}

func TestFailStep(t *testing.T) {
	rep, sessionDir := newTestReporter(t)

	rep.StartStep("tts", 60)
	rep.FailStep(errors.New("connection refused"))

	state := readProgress(t, sessionDir)
	if state.Status != "failed" {
		t.Errorf("status = %q, want failed", state.Status)
	}
	if state.Health != "failed" {
		t.Errorf("health = %q, want failed", state.Health)
	}
}

func TestWriteStatus(t *testing.T) {
	rep, sessionDir := newTestReporter(t)

	rep.StartStep("whisper", 100)
	rep.CompleteStep(InputMetric{}, "transcript.srt.txt")
	rep.WriteStatus(nil)

	ps := readStatus(t, sessionDir)
	if ps.Status != "complete" {
		t.Errorf("pipeline status = %q, want complete", ps.Status)
	}
	if ps.Date != "2026-03-15" {
		t.Errorf("date = %q, want 2026-03-15", ps.Date)
	}
	if result, ok := ps.Steps["whisper"]; !ok {
		t.Error("whisper step missing from status")
	} else if result.Status != "complete" {
		t.Errorf("whisper status = %q, want complete", result.Status)
	}
}

func TestWriteStatusOnFailure(t *testing.T) {
	rep, sessionDir := newTestReporter(t)

	rep.StartStep("whisper", 100)
	rep.FailStep(errors.New("upload failed"))
	rep.WriteStatus(errors.New("step whisper: upload failed"))

	ps := readStatus(t, sessionDir)
	if ps.Status != "failed" {
		t.Errorf("pipeline status = %q, want failed", ps.Status)
	}
	result := ps.Steps["whisper"]
	if result.Error != "upload failed" {
		t.Errorf("whisper error = %q, want 'upload failed'", result.Error)
	}
}

func TestBenchmarkRecordAndRate(t *testing.T) {
	rep, sessionDir := newTestReporter(t)
	outputDir := filepath.Dir(sessionDir)

	metric := InputMetric{Value: 3600, Unit: "audio_sec"}
	rep.StartStep("whisper", 1080)
	rep.CompleteStep(metric, "transcript.srt.txt")

	data, err := os.ReadFile(filepath.Join(outputDir, benchmarksFile))
	if err != nil {
		t.Fatalf("read benchmarks: %v", err)
	}
	var history BenchmarkHistory
	if err := json.Unmarshal(data, &history); err != nil {
		t.Fatalf("unmarshal benchmarks: %v", err)
	}

	if len(history.Runs) != 1 {
		t.Fatalf("runs = %d, want 1", len(history.Runs))
	}
	step := history.Runs[0].Steps["whisper"]
	if step.InputValue != 3600 {
		t.Errorf("input_value = %f, want 3600", step.InputValue)
	}
	if step.InputUnit != "audio_sec" {
		t.Errorf("input_unit = %q, want audio_sec", step.InputUnit)
	}
}

func TestEstimateWithSeedRate(t *testing.T) {
	rep, _ := newTestReporter(t)

	metric := InputMetric{Value: 7200, Unit: "audio_sec"}
	est := rep.EstimateStepDuration("whisper", metric)

	expected := 7200 * 0.3
	if est != expected {
		t.Errorf("estimate = %f, want %f", est, expected)
	}
}

func TestEstimateWithBenchmarkHistory(t *testing.T) {
	dir := t.TempDir()
	sessionDir := filepath.Join(dir, "2026-03-22")
	os.MkdirAll(sessionDir, 0o755)

	history := BenchmarkHistory{
		Runs: []BenchmarkRun{
			{
				Date: "2026-03-15",
				Steps: map[string]BenchmarkStep{
					"whisper": {InputValue: 3600, InputUnit: "audio_sec", ElapsedSec: 720},
				},
			},
			{
				Date: "2026-03-08",
				Steps: map[string]BenchmarkStep{
					"whisper": {InputValue: 7200, InputUnit: "audio_sec", ElapsedSec: 1800},
				},
			},
		},
	}
	data, _ := json.Marshal(history)
	os.WriteFile(filepath.Join(dir, benchmarksFile), data, 0o644)

	seeds := map[string]float64{"whisper": 0.3}
	rep := New(sessionDir, dir, "2026-03-22", seeds, 5)

	metric := InputMetric{Value: 5400, Unit: "audio_sec"}
	est := rep.EstimateStepDuration("whisper", metric)

	rate1 := 720.0 / 3600.0
	rate2 := 1800.0 / 7200.0
	avgRate := (rate1 + rate2) / 2.0
	expected := 5400 * avgRate

	if est != expected {
		t.Errorf("estimate = %f, want %f (avgRate=%f)", est, expected, avgRate)
	}
}

func TestEstimateZeroMetric(t *testing.T) {
	rep, _ := newTestReporter(t)

	est := rep.EstimateStepDuration("whisper", InputMetric{})
	if est != 0 {
		t.Errorf("estimate = %f, want 0 for zero metric", est)
	}
}

func TestNilReporterSafety(t *testing.T) {
	var rep *Reporter

	rep.StartStep("whisper", 100)
	rep.UpdateProgress(0.5)
	rep.Heartbeat()
	rep.CompleteStep(InputMetric{}, "out.txt")
	rep.FailStep(errors.New("err"))
	rep.SkipStep("tts")
	rep.WriteStatus(nil)

	est := rep.EstimateStepDuration("whisper", InputMetric{Value: 100, Unit: "s"})
	if est != 0 {
		t.Errorf("nil estimate = %f, want 0", est)
	}
}

func TestAverageRateWindowLimit(t *testing.T) {
	history := &BenchmarkHistory{
		Runs: []BenchmarkRun{
			{Date: "d1", Steps: map[string]BenchmarkStep{"w": {InputValue: 100, ElapsedSec: 30}}},
			{Date: "d2", Steps: map[string]BenchmarkStep{"w": {InputValue: 100, ElapsedSec: 40}}},
			{Date: "d3", Steps: map[string]BenchmarkStep{"w": {InputValue: 100, ElapsedSec: 50}}},
		},
	}

	rate := history.averageRate("w", 2)
	expected := (50.0/100.0 + 40.0/100.0) / 2.0
	if rate != expected {
		t.Errorf("rate = %f, want %f (window=2 should use last 2 runs)", rate, expected)
	}
}

func TestHealthStalled(t *testing.T) {
	rep, _ := newTestReporter(t)

	rep.mu.Lock()
	rep.currentStep = "whisper"
	rep.stepStart = time.Now().Add(-120 * time.Second)
	rep.estimatedSec = 300
	rep.lastProgress = 0.3
	rep.lastProgressAt = time.Now().Add(-90 * time.Second)
	rep.stepResults = map[string]*StepResult{"whisper": {Status: "running"}}
	health := rep.computeHealth(120)
	rep.mu.Unlock()

	if health != "stalled" {
		t.Errorf("health = %q, want stalled", health)
	}
}

func TestHealthSlow(t *testing.T) {
	rep, _ := newTestReporter(t)

	rep.mu.Lock()
	rep.currentStep = "whisper"
	rep.stepStart = time.Now().Add(-200 * time.Second)
	rep.estimatedSec = 300
	rep.lastProgress = 0.2
	rep.lastProgressAt = time.Now()
	rep.stepResults = map[string]*StepResult{"whisper": {Status: "running"}}
	health := rep.computeHealth(200)
	rep.mu.Unlock()

	if health != "slow" {
		t.Errorf("health = %q, want slow (expected ~0.67, actual 0.2)", health)
	}
}

func TestHealthSlowNoProgress(t *testing.T) {
	rep, _ := newTestReporter(t)

	rep.mu.Lock()
	rep.currentStep = "audio"
	rep.stepStart = time.Now().Add(-150 * time.Second)
	rep.estimatedSec = 100
	rep.lastProgress = 0
	rep.stepResults = map[string]*StepResult{"audio": {Status: "running"}}
	health := rep.computeHealth(150)
	rep.mu.Unlock()

	if health != "slow" {
		t.Errorf("health = %q, want slow (elapsed 150 > estimated 100 * 1.2)", health)
	}
}

func TestHealthHealthy(t *testing.T) {
	rep, _ := newTestReporter(t)

	rep.mu.Lock()
	rep.currentStep = "whisper"
	rep.stepStart = time.Now().Add(-50 * time.Second)
	rep.estimatedSec = 300
	rep.lastProgress = 0.2
	rep.lastProgressAt = time.Now()
	rep.stepResults = map[string]*StepResult{"whisper": {Status: "running"}}
	health := rep.computeHealth(50)
	rep.mu.Unlock()

	if health != "healthy" {
		t.Errorf("health = %q, want healthy", health)
	}
}

func TestHeartbeatWritesFile(t *testing.T) {
	rep, sessionDir := newTestReporter(t)

	rep.StartStep("audio", 60)

	time.Sleep(200 * time.Millisecond)

	state := readProgress(t, sessionDir)
	if state.Step != "audio" {
		t.Errorf("step = %q, want audio", state.Step)
	}
	if state.Status != "running" {
		t.Errorf("status = %q, want running", state.Status)
	}

	rep.CompleteStep(InputMetric{}, "final.mp3")
}

func TestMultipleStepsAccumulate(t *testing.T) {
	rep, sessionDir := newTestReporter(t)

	rep.SkipStep("whisper")
	rep.StartStep("perplexity", 0)
	rep.CompleteStep(InputMetric{}, "notes.md")
	rep.StartStep("tts", 60)
	rep.CompleteStep(InputMetric{Value: 5000, Unit: "chars"}, "raw.mp3")
	rep.WriteStatus(nil)

	ps := readStatus(t, sessionDir)
	if ps.Status != "complete" {
		t.Errorf("pipeline status = %q, want complete", ps.Status)
	}
	if ps.Steps["whisper"].Status != "skipped" {
		t.Errorf("whisper = %q, want skipped", ps.Steps["whisper"].Status)
	}
	if ps.Steps["perplexity"].Status != "complete" {
		t.Errorf("perplexity = %q, want complete", ps.Steps["perplexity"].Status)
	}
	if ps.Steps["tts"].Status != "complete" {
		t.Errorf("tts = %q, want complete", ps.Steps["tts"].Status)
	}
}
