// Package pipeline orchestrates the multi-step D&D session processing workflow.
package pipeline

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"dnd-workflow/internal/audio"
	"dnd-workflow/internal/config"
	"dnd-workflow/internal/perplexity"
	"dnd-workflow/internal/progress"
	"dnd-workflow/internal/tts"
)

var StepOrder = []string{"whisper", "perplexity", "perplexity-upload", "perplexity-scrape", "tts", "audio", "wiki", "distribute"}

const (
	stepWhisper          = 0
	stepPerplexity       = 1
	stepPerplexityUpload = 2
	stepPerplexityScrape = 3
	stepTTS              = 4
	stepAudio            = 5
	stepWiki             = 6
	stepDistribute       = 7
)

type Transcriber interface {
	Transcribe(ctx context.Context, audioPath, outputPath string) error
}

type NotesGenerator interface {
	Start() error
	Close()
	GenerateNotesInThread(srtPath, promptText, threadName string) (string, string, error)
	// UploadAndSubmit submits a prompt to Perplexity without waiting for response.
	// Browser remains open after call; caller must call Close().
	UploadAndSubmit(srtPath, promptText, threadName string) error
	// ScrapeExistingResponse extracts notes/narration from a page that already has a response.
	ScrapeExistingResponse() (string, string, error)
}

type Speaker interface {
	ConvertTextToAudio(ctx context.Context, text, outputPath string, cfg interface{}) error
}

type AudioFixer interface {
	Process(ctx context.Context, inputPath, outputPath string) error
}

type Publisher interface {
	CreatePage(ctx context.Context, title, path, content string, tags []string) (int, error)
	CheckPageExists(ctx context.Context, path string) (bool, error)
}

type FileDistributor interface {
	Distribute(ctx context.Context, transcriptSrc, audioSrc, date string) error
	MoveOriginalAudio(ctx context.Context, srcPath, date string) error
}

type Runner struct {
	cfg        *config.Config
	transcribe Transcriber
	notes      NotesGenerator
	speak      Speaker
	audioFix   AudioFixer
	publish    Publisher
	distribute FileDistributor
	force      bool
	reporter   *progress.Reporter
}

func NewRunner(cfg *config.Config, t Transcriber, n NotesGenerator, s Speaker, a AudioFixer, p Publisher, d FileDistributor) *Runner {
	return &Runner{
		cfg:        cfg,
		transcribe: t,
		notes:      n,
		speak:      s,
		audioFix:   a,
		publish:    p,
		distribute: d,
	}
}

func (r *Runner) SetForce(force bool) {
	r.force = force
}

func ValidStep(s string) bool {
	if s == "all" {
		return true
	}
	for _, v := range StepOrder {
		if v == s {
			return true
		}
	}
	return false
}

func stepIndex(s string) int {
	for i, v := range StepOrder {
		if v == s {
			return i
		}
	}
	return 0
}

// RunFrom runs the pipeline starting at startStep. When continueSteps is true,
// all steps from startStep onward are executed; otherwise only the single
// requested step runs. Use "all" to run every step regardless of continueSteps.
func (r *Runner) RunFrom(ctx context.Context, audioPath, date, startStep string, continueSteps bool) (retErr error) {
	if err := config.ValidateDate(date); err != nil {
		return err
	}

	sessionDir, err := r.cfg.EnsureSessionDir(date)
	if err != nil {
		return err
	}

	seedRates := map[string]float64{
		"whisper": r.cfg.Benchmarks.WhisperRate,
		"tts":     r.cfg.Benchmarks.TTSRate,
		"audio":   r.cfg.Benchmarks.AudioRate,
	}
	r.reporter = progress.New(
		sessionDir, r.cfg.OutputDir, date,
		seedRates, r.cfg.Benchmarks.HistoryWindow,
	)
	ctx = progress.WithReporter(ctx, r.reporter)
	defer func() { r.reporter.WriteStatus(retErr) }()

	start := 0
	if startStep != "" && startStep != "all" {
		start = stepIndex(startStep)
	}

	end := len(StepOrder) - 1
	if startStep != "" && startStep != "all" && !continueSteps {
		end = start
	}

	ext := r.cfg.Audio.OutputFormat

	var srtPath, fullNotes, narration, rawAudioPath string

	// Step 0: whisper
	if start <= 0 && 0 <= end {
		srtPath, err = r.runTranscribe(ctx, audioPath, sessionDir, date)
		if err != nil {
			slog.Error("step failed", "step", "whisper", "error", err)
			return fmt.Errorf("step whisper: %w", err)
		}
	}

	// Step 1: perplexity — needs whisper output
	if start <= 1 && 1 <= end {
		if start > 0 {
			srtPath = filepath.Join(sessionDir, fmt.Sprintf("transcript_%s.srt.txt", date))
			if !fileExists(srtPath) {
				return fmt.Errorf("step whisper output not found: %s", srtPath)
			}
			slog.Info("using existing output", "step", "whisper", "path", srtPath)
		}
		fullNotes, narration, err = r.runNotes(ctx, srtPath, sessionDir, date)
		if err != nil {
			slog.Error("step failed", "step", "perplexity", "error", err)
			return fmt.Errorf("step perplexity: %w", err)
		}
	}

	// Step 2: perplexity-upload — upload and submit only, no wait
	if start <= stepPerplexityUpload && stepPerplexityUpload <= end {
		if start > stepPerplexity {
			srtPath = filepath.Join(sessionDir, fmt.Sprintf("transcript_%s.srt.txt", date))
			if !fileExists(srtPath) {
				return fmt.Errorf("step whisper output not found: %s", srtPath)
			}
			slog.Info("using existing output", "step", "whisper", "path", srtPath)
		}
		if err := r.runPerplexityUpload(ctx, srtPath, sessionDir, date); err != nil {
			slog.Error("step failed", "step", "perplexity-upload", "error", err)
			return fmt.Errorf("step perplexity-upload: %w", err)
		}
	}

	// Step 3: perplexity-scrape — scrape existing response
	if start <= stepPerplexityScrape && stepPerplexityScrape <= end {
		if start > stepPerplexityUpload {
			srtPath = filepath.Join(sessionDir, fmt.Sprintf("transcript_%s.srt.txt", date))
			if !fileExists(srtPath) {
				return fmt.Errorf("step whisper output not found: %s", srtPath)
			}
			slog.Info("using existing output", "step", "whisper", "path", srtPath)
		}
		fullNotes, narration, err = r.runPerplexityScrape(ctx, srtPath, sessionDir, date)
		if err != nil {
			slog.Error("step failed", "step", "perplexity-scrape", "error", err)
			return fmt.Errorf("step perplexity-scrape: %w", err)
		}
	}

	// Step 4: tts — needs perplexity narration output
	if start <= stepTTS && stepTTS <= end {
		if start > stepPerplexityScrape {
			// Check both custom recaps dir and session dir for existing narration
			recapsDir := sessionDir
			if r.cfg.Perplexity.SessionRecapsDir != "" {
				recapsDir = r.cfg.Perplexity.SessionRecapsDir
			}
			customNarrationPath := filepath.Join(recapsDir, fmt.Sprintf("narration_%s.md", date))
			sessionNarrationPath := filepath.Join(sessionDir, fmt.Sprintf("narration_%s.md", date))

			var narrationPath string
			if fileExists(customNarrationPath) {
				narrationPath = customNarrationPath
			} else if fileExists(sessionNarrationPath) {
				narrationPath = sessionNarrationPath
			} else {
				return fmt.Errorf("step perplexity output not found: narration_%s.md", date)
			}
			slog.Info("using existing output", "step", "perplexity (narration)", "path", narrationPath)
			narrationData, err := os.ReadFile(narrationPath)
			if err != nil {
				slog.Warn("failed to read existing narration", "path", narrationPath, "error", err)
			}
			narration = string(narrationData)
		}
		rawAudioPath, err = r.runTTS(ctx, narration, sessionDir, date, ext)
		if err != nil {
			slog.Error("step failed", "step", "tts", "error", err)
			return fmt.Errorf("step tts: %w", err)
		}
	}

	// Step 5: audio — needs tts output
	if start <= stepAudio && stepAudio <= end {
		if start > stepPerplexityUpload {
			rawAudioPath = filepath.Join(sessionDir, fmt.Sprintf("narration_raw_%s.%s", date, ext))
			if !fileExists(rawAudioPath) {
				return fmt.Errorf("step tts output not found: %s", rawAudioPath)
			}
			slog.Info("using existing output", "step", "tts", "path", rawAudioPath)
		}
		if err := r.runAudioFix(ctx, rawAudioPath, sessionDir, date, ext); err != nil {
			slog.Error("step failed", "step", "audio", "error", err)
			return fmt.Errorf("step audio: %w", err)
		}
	}

	// Step 6: wiki — needs perplexity notes output
	if start <= stepWiki && stepWiki <= end {
		if start > stepPerplexity && fullNotes == "" {
			// Check both custom recaps dir and session dir for existing notes
			recapsDir := sessionDir
			if r.cfg.Perplexity.SessionRecapsDir != "" {
				recapsDir = r.cfg.Perplexity.SessionRecapsDir
			}
			customNotesPath := filepath.Join(recapsDir, fmt.Sprintf("notes_%s.md", date))
			sessionNotesPath := filepath.Join(sessionDir, fmt.Sprintf("notes_%s.md", date))

			var notesPath string
			if fileExists(customNotesPath) {
				notesPath = customNotesPath
			} else if fileExists(sessionNotesPath) {
				notesPath = sessionNotesPath
			} else {
				return fmt.Errorf("step perplexity output not found: notes_%s.md", date)
			}
			slog.Info("using existing output", "step", "perplexity (notes)", "path", notesPath)
			notesData, err := os.ReadFile(notesPath)
			if err != nil {
				slog.Warn("failed to read existing notes", "path", notesPath, "error", err)
			}
			fullNotes = string(notesData)
		}
		if err := r.RunPublish(ctx, fullNotes, date); err != nil {
			slog.Error("step failed", "step", "wiki", "error", err)
			return fmt.Errorf("step wiki: %w", err)
		}
	}

	// Step 7: distribute — needs whisper transcript and audio output
	if start <= stepDistribute && stepDistribute <= end {
		if srtPath == "" {
			srtPath = filepath.Join(sessionDir, fmt.Sprintf("transcript_%s.srt.txt", date))
		}
		finalAudioPath := filepath.Join(sessionDir, fmt.Sprintf("narration_final_%s.%s", date, ext))
		if err := r.runDistribute(ctx, srtPath, finalAudioPath, date); err != nil {
			slog.Error("step failed", "step", "distribute", "error", err)
			return fmt.Errorf("step distribute: %w", err)
		}
	}

	slog.Info("pipeline complete", "output_dir", sessionDir)
	return nil
}

func (r *Runner) runTranscribe(ctx context.Context, audioPath, sessionDir, date string) (srtPath string, retErr error) {
	srtPath = filepath.Join(sessionDir, fmt.Sprintf("transcript_%s.srt.txt", date))

	if !r.force && fileExists(srtPath) {
		slog.Info("transcript exists, skipping", "path", srtPath)
		r.reporter.SkipStep("whisper")
		return srtPath, nil
	}

	var metric progress.InputMetric
	if audioPath != "" {
		dur, probeErr := audio.ProbeDuration(ctx, r.cfg.Audio.FFprobePath, audioPath)
		if probeErr == nil {
			metric = progress.InputMetric{Value: dur, Unit: "audio_sec"}
		}
	}
	est := r.reporter.EstimateStepDuration("whisper", metric)
	r.reporter.StartStep("whisper", est)
	defer func() {
		if retErr != nil {
			r.reporter.FailStep(retErr)
		} else {
			r.reporter.CompleteStep(metric, filepath.Base(srtPath))
		}
	}()

	slog.Info("starting step", "step", "whisper")
	if err := r.transcribe.Transcribe(ctx, audioPath, srtPath); err != nil {
		return "", err
	}

	slog.Info("transcript saved", "path", srtPath)

	if r.cfg.Distribute.OriginalAudioDir != "" && audioPath != "" {
		if err := r.distribute.MoveOriginalAudio(ctx, audioPath, date); err != nil {
			slog.Warn("failed to move original audio (continuing pipeline)", "error", err)
		}
	}

	return srtPath, nil
}

func (r *Runner) runNotes(ctx context.Context, srtPath, sessionDir, date string) (fullNotes string, narration string, retErr error) {
	recapsDir := sessionDir
	if r.cfg.Perplexity.SessionRecapsDir != "" {
		recapsDir = r.cfg.Perplexity.SessionRecapsDir
		slog.Info("using perplexity session recaps directory", "path", recapsDir)
		if err := os.MkdirAll(recapsDir, 0o755); err != nil {
			return "", "", fmt.Errorf("ensure recaps dir: %w", err)
		}
	}
	notesPath := filepath.Join(recapsDir, fmt.Sprintf("notes_%s.md", date))
	narrationPath := filepath.Join(recapsDir, fmt.Sprintf("narration_%s.md", date))

	// Also prepare paths in sessionDir for pipeline continuity
	sessionNotesPath := filepath.Join(sessionDir, fmt.Sprintf("notes_%s.md", date))
	sessionNarrationPath := filepath.Join(sessionDir, fmt.Sprintf("narration_%s.md", date))

	// Check if files exist in either location (must have non-zero size)
	skipStep := !r.force && ((nonZeroFileExists(notesPath) && nonZeroFileExists(narrationPath)) || (nonZeroFileExists(sessionNotesPath) && nonZeroFileExists(sessionNarrationPath)))

	if skipStep {
		slog.Info("notes exist, skipping")
		r.reporter.SkipStep("perplexity")
		// Prefer reading from custom dir if it exists and non-empty, otherwise fall back to session dir
		var notes, narrationData []byte
		if nonZeroFileExists(notesPath) && nonZeroFileExists(narrationPath) {
			notes, _ = os.ReadFile(notesPath)
			narrationData, _ = os.ReadFile(narrationPath)
		} else {
			notes, _ = os.ReadFile(sessionNotesPath)
			narrationData, _ = os.ReadFile(sessionNarrationPath)
		}
		return string(notes), string(narrationData), nil
	}

	r.reporter.StartStep("perplexity", 0)
	defer func() {
		if retErr != nil {
			r.reporter.FailStep(retErr)
		} else {
			r.reporter.CompleteStep(progress.InputMetric{}, filepath.Base(notesPath))
		}
	}()

	slog.Info("starting step", "step", "perplexity")

	promptText, err := perplexity.LoadPrompt(r.cfg.Perplexity.PromptFile, date)
	if err != nil {
		return "", "", err
	}

	if err := r.notes.Start(); err != nil {
		return "", "", fmt.Errorf("start browser: %w", err)
	}
	defer r.notes.Close()

	fullNotes, narration, err = r.notes.GenerateNotesInThread(srtPath, promptText, r.cfg.Perplexity.ThreadName)
	if err != nil {
		return "", "", err
	}

	if err := os.WriteFile(notesPath, []byte(fullNotes), 0o644); err != nil {
		return "", "", fmt.Errorf("write notes: %w", err)
	}

	if err := os.WriteFile(narrationPath, []byte(narration), 0o644); err != nil {
		return "", "", fmt.Errorf("write narration: %w", err)
	}

	// Also save to sessionDir to ensure pipeline continuity
	if err := os.WriteFile(sessionNotesPath, []byte(fullNotes), 0o644); err != nil {
		return "", "", fmt.Errorf("write session notes: %w", err)
	}

	if err := os.WriteFile(sessionNarrationPath, []byte(narration), 0o644); err != nil {
		return "", "", fmt.Errorf("write session narration: %w", err)
	}

	slog.Info("notes saved", "path", notesPath)
	if recapsDir != sessionDir {
		slog.Info("notes also saved to session directory", "path", sessionNotesPath)
	}
	return fullNotes, narration, nil
}

func (r *Runner) runPerplexityUpload(ctx context.Context, srtPath, sessionDir, date string) error {
	promptText, err := perplexity.LoadPrompt(r.cfg.Perplexity.PromptFile, date)
	if err != nil {
		return err
	}

	if err := r.notes.Start(); err != nil {
		return fmt.Errorf("start browser: %w", err)
	}
	defer r.notes.Close()

	r.reporter.StartStep("perplexity-upload", 0)
	slog.Info("starting step", "step", "perplexity-upload")

	if err := r.notes.UploadAndSubmit(srtPath, promptText, r.cfg.Perplexity.ThreadName); err != nil {
		r.reporter.FailStep(err)
		return fmt.Errorf("upload and submit: %w", err)
	}

	r.reporter.CompleteStep(progress.InputMetric{}, "perplexity-upload")
	slog.Info("perplexity-upload complete, response pending in browser")
	return nil
}

func (r *Runner) runPerplexityScrape(ctx context.Context, srtPath, sessionDir, date string) (string, string, error) {
	if err := r.notes.Start(); err != nil {
		return "", "", fmt.Errorf("start browser: %w", err)
	}
	defer r.notes.Close()

	r.reporter.StartStep("perplexity-scrape", 0)
	slog.Info("starting step", "step", "perplexity-scrape")

	fullNotes, narration, err := r.notes.ScrapeExistingResponse()
	if err != nil {
		r.reporter.FailStep(err)
		return "", "", fmt.Errorf("scrape response: %w", err)
	}

	recapsDir := sessionDir
	if r.cfg.Perplexity.SessionRecapsDir != "" {
		recapsDir = r.cfg.Perplexity.SessionRecapsDir
	}

	notesPath := filepath.Join(recapsDir, fmt.Sprintf("notes_%s.md", date))
	narrationPath := filepath.Join(recapsDir, fmt.Sprintf("narration_%s.md", date))
	sessionNotesPath := filepath.Join(sessionDir, fmt.Sprintf("notes_%s.md", date))
	sessionNarrationPath := filepath.Join(sessionDir, fmt.Sprintf("narration_%s.md", date))

	if err := os.WriteFile(notesPath, []byte(fullNotes), 0o644); err != nil {
		return "", "", fmt.Errorf("write notes: %w", err)
	}
	if err := os.WriteFile(narrationPath, []byte(narration), 0o644); err != nil {
		return "", "", fmt.Errorf("write narration: %w", err)
	}
	if err := os.WriteFile(sessionNotesPath, []byte(fullNotes), 0o644); err != nil {
		return "", "", fmt.Errorf("write session notes: %w", err)
	}
	if err := os.WriteFile(sessionNarrationPath, []byte(narration), 0o644); err != nil {
		return "", "", fmt.Errorf("write session narration: %w", err)
	}

	slog.Info("notes saved", "path", notesPath)
	if recapsDir != sessionDir {
		slog.Info("notes also saved to session directory", "path", sessionNotesPath)
	}

	r.reporter.CompleteStep(progress.InputMetric{}, filepath.Base(notesPath))
	return fullNotes, narration, nil
}

func (r *Runner) runTTS(ctx context.Context, narrationText, sessionDir, date, ext string) (rawPath string, retErr error) {
	rawPath = filepath.Join(sessionDir, fmt.Sprintf("narration_raw_%s.%s", date, ext))

	if !r.force && fileExists(rawPath) {
		slog.Info("TTS output exists, skipping", "path", rawPath)
		r.reporter.SkipStep("tts")
		return rawPath, nil
	}

	narrationText = tts.InsertPauseTags(
		narrationText,
		r.cfg.Audio.ParagraphPauseSec,
		r.cfg.Audio.SectionPauseSec,
	)

	metric := progress.InputMetric{Value: float64(len(narrationText)), Unit: "chars"}
	est := r.reporter.EstimateStepDuration("tts", metric)
	r.reporter.StartStep("tts", est)
	defer func() {
		if retErr != nil {
			r.reporter.FailStep(retErr)
		} else {
			r.reporter.CompleteStep(metric, filepath.Base(rawPath))
		}
	}()

	slog.Info("starting step", "step", "tts")

	if r.cfg.Audio.SavePauseText {
		pausePath := filepath.Join(sessionDir, fmt.Sprintf("narration_paused_%s.txt", date))
		if err := os.WriteFile(pausePath, []byte(narrationText), 0o644); err != nil {
			return "", fmt.Errorf("write pause text: %w", err)
		}
		slog.Info("pause-tagged text saved", "path", pausePath)
	}

	if err := r.speak.ConvertTextToAudio(ctx, narrationText, rawPath, nil); err != nil {
		return "", err
	}

	return rawPath, nil
}

func (r *Runner) runAudioFix(ctx context.Context, inputPath, sessionDir, date, ext string) (retErr error) {
	outputPath := filepath.Join(sessionDir, fmt.Sprintf("narration_final_%s.%s", date, ext))

	if !r.force && fileExists(outputPath) {
		slog.Info("audio output exists, skipping", "path", outputPath)
		r.reporter.SkipStep("audio")
		return nil
	}

	var metric progress.InputMetric
	dur, probeErr := audio.ProbeDuration(ctx, r.cfg.Audio.FFprobePath, inputPath)
	if probeErr == nil {
		metric = progress.InputMetric{Value: dur, Unit: "audio_sec"}
	}
	est := r.reporter.EstimateStepDuration("audio", metric)
	r.reporter.StartStep("audio", est)
	defer func() {
		if retErr != nil {
			r.reporter.FailStep(retErr)
		} else {
			r.reporter.CompleteStep(metric, filepath.Base(outputPath))
		}
	}()

	slog.Info("starting step", "step", "audio")
	return r.audioFix.Process(ctx, inputPath, outputPath)
}

func (r *Runner) RunPublish(ctx context.Context, content, date string) error {
	if err := config.ValidateDate(date); err != nil {
		return err
	}
	r.reporter.StartStep("wiki", 0)
	slog.Info("starting step", "step", "wiki")

	token := config.WikiJSToken()
	if token == "" {
		err := fmt.Errorf("WIKIJS_TOKEN environment variable not set")
		r.reporter.FailStep(err)
		return err
	}

	year := date[:4]
	path := fmt.Sprintf("%s/%s/%s", r.cfg.WikiJS.BasePath, year, date)
	title := r.cfg.WikiPageTitle(date)
	tags := r.cfg.WikiJS.Tags

	exists, err := r.publish.CheckPageExists(ctx, path)
	if err != nil {
		slog.Warn("could not check if page exists, proceeding", "error", err)
	}
	if exists {
		slog.Info("wiki page already exists, skipping", "path", path)
		r.reporter.CompleteStep(progress.InputMetric{}, "")
		return nil
	}

	pageID, err := r.publish.CreatePage(ctx, title, path, content, tags)
	if err != nil {
		r.reporter.FailStep(err)
		return err
	}

	slog.Info("published to wiki", "page_id", pageID, "path", path)
	r.reporter.CompleteStep(progress.InputMetric{}, "")
	return nil
}

func (r *Runner) runDistribute(ctx context.Context, transcriptPath, audioPath, date string) (retErr error) {
	r.reporter.StartStep("distribute", 0)
	defer func() {
		if retErr != nil {
			r.reporter.FailStep(retErr)
		} else {
			r.reporter.CompleteStep(progress.InputMetric{}, "")
		}
	}()

	slog.Info("starting step", "step", "distribute")

	if !fileExists(transcriptPath) {
		slog.Warn("transcript not found, skipping transcript distribution", "path", transcriptPath)
		transcriptPath = ""
	}
	if !fileExists(audioPath) {
		slog.Warn("final audio not found, skipping audio distribution", "path", audioPath)
		audioPath = ""
	}

	if transcriptPath == "" && audioPath == "" {
		slog.Info("no files to distribute, skipping")
		return nil
	}

	return r.distribute.Distribute(ctx, transcriptPath, audioPath, date)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// nonZeroFileExists returns true if the file exists and has non-zero size.
func nonZeroFileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Size() > 0
}
