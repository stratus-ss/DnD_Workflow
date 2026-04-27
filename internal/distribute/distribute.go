package distribute

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"dnd-workflow/internal/config"
)

type Distributor struct {
	cfg config.DistributeConfig
}

func New(cfg config.DistributeConfig) *Distributor {
	return &Distributor{cfg: cfg}
}

// Distribute copies the transcript and final audio to their configured
// destinations. The date string must be in YYYY-MM-DD format. If a destination
// directory is unconfigured (empty), that copy is skipped.
func (d *Distributor) Distribute(_ context.Context, transcriptSrc, audioSrc, date string) error {
	displayDate, err := toDisplayDate(date)
	if err != nil {
		return fmt.Errorf("parse date %q: %w", date, err)
	}

	if transcriptSrc != "" {
		if err := d.distributeTranscript(transcriptSrc, displayDate); err != nil {
			return fmt.Errorf("distribute transcript: %w", err)
		}
	}

	if audioSrc != "" {
		if err := d.distributeAudio(audioSrc, displayDate); err != nil {
			return fmt.Errorf("distribute audio: %w", err)
		}
	}

	return nil
}

func (d *Distributor) distributeTranscript(src, displayDate string) error {
	if d.cfg.TranscriptDir == "" {
		slog.Info("distribute: transcript_dir not configured, skipping transcript copy")
		return nil
	}

	dst := filepath.Join(d.cfg.TranscriptDir, displayDate+".srt.txt")
	if err := os.MkdirAll(d.cfg.TranscriptDir, 0o755); err != nil {
		return fmt.Errorf("ensure transcript dir: %w", err)
	}

	if err := copyFile(src, dst); err != nil {
		return err
	}
	slog.Info("transcript distributed", "dst", dst)
	return nil
}

func (d *Distributor) distributeAudio(src, displayDate string) error {
	if d.cfg.AudioDir == "" {
		slog.Info("distribute: audio_dir not configured, skipping audio copy")
		return nil
	}

	if err := d.rotateExistingRecap(); err != nil {
		return fmt.Errorf("rotate existing recap: %w", err)
	}

	ext := filepath.Ext(src)
	dst := filepath.Join(d.cfg.AudioDir, "session_recap_"+displayDate+ext)
	if err := os.MkdirAll(d.cfg.AudioDir, 0o755); err != nil {
		return fmt.Errorf("ensure audio dir: %w", err)
	}

	if err := copyFile(src, dst); err != nil {
		return err
	}
	slog.Info("audio distributed", "dst", dst)
	return nil
}

func (d *Distributor) rotateExistingRecap() error {
	matches, err := filepath.Glob(filepath.Join(d.cfg.AudioDir, "session_recap_*"))
	if err != nil {
		return err
	}
	if len(matches) == 0 {
		return nil
	}

	completedDir := d.cfg.AudioCompletedDir
	if completedDir == "" {
		completedDir = filepath.Join(d.cfg.AudioDir, "Completed")
	}
	if err := os.MkdirAll(completedDir, 0o755); err != nil {
		return fmt.Errorf("ensure completed dir: %w", err)
	}

	for _, src := range matches {
		dst := filepath.Join(completedDir, filepath.Base(src))
		if err := os.Rename(src, dst); err != nil {
			return fmt.Errorf("move %s to %s: %w", src, dst, err)
		}
		slog.Info("moved previous recap to completed", "src", src, "dst", dst)
	}
	return nil
}

// MoveOriginalAudio moves the original audio file to the configured directory
// after successful transcription. Errors are logged but do not fail the pipeline.
func (d *Distributor) MoveOriginalAudio(_ context.Context, srcPath, date string) error {
	if d.cfg.OriginalAudioDir == "" {
		return nil
	}

	displayDate, err := toDisplayDate(date)
	if err != nil {
		return fmt.Errorf("parse date %q: %w", date, err)
	}

	if err := os.MkdirAll(d.cfg.OriginalAudioDir, 0o755); err != nil {
		return fmt.Errorf("ensure original audio dir: %w", err)
	}

	filename := filepath.Base(srcPath)
	dst := filepath.Join(d.cfg.OriginalAudioDir, displayDate+filename)

	if err := copyFile(srcPath, dst); err != nil {
		return fmt.Errorf("copy original audio: %w", err)
	}

	slog.Info("moved original audio file", "dst", dst)
	return nil
}

// toDisplayDate converts "2026-03-29" to "Mar_29_2026".
func toDisplayDate(isoDate string) (string, error) {
	t, err := time.Parse("2006-01-02", isoDate)
	if err != nil {
		return "", err
	}
	return t.Format("Jan_02_2006"), nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open %s: %w", src, err)
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("create %s: %w", dst, err)
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("copy %s -> %s: %w", src, dst, err)
	}
	return out.Close()
}
