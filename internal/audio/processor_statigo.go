//go:build statigo

package audio

import (
	"context"
	"fmt"
	"log/slog"

	"dnd-workflow/internal/config"
	"dnd-workflow/internal/fileutil"
)

// Processor uses ffmpeg-statigo C bindings for audio processing,
// eliminating the need for ffmpeg/ffprobe on PATH at runtime.
type Processor struct {
	cfg config.AudioConfig
}

func NewProcessor(cfg config.AudioConfig) *Processor {
	return &Processor{cfg: cfg}
}

func (p *Processor) Process(ctx context.Context, inputPath, outputPath string) error {
	silences, err := p.DetectSilences(ctx, inputPath)
	if err != nil {
		return fmt.Errorf("detect silences: %w", err)
	}

	slog.Info("silence detection complete", "regions", len(silences))

	shortGaps := findShortGaps(silences, p.cfg.MinPauseMs)
	if len(shortGaps) == 0 {
		slog.Info("no short gaps to fix, copying file as-is")
		return fileutil.CopyFile(inputPath, outputPath)
	}

	slog.Info("injecting pauses for short gaps", "count", len(shortGaps), "min_pause_ms", p.cfg.MinPauseMs)

	return p.injectPauses(inputPath, outputPath, shortGaps)
}

func (p *Processor) DetectSilences(ctx context.Context, audioPath string) ([]SilenceRange, error) {
	input, err := openAudioDecoder(audioPath)
	if err != nil {
		return nil, err
	}
	defer input.close()

	sf, err := buildSilenceFilterGraph(input.decCtx, p.cfg.SilenceThreshDB, p.cfg.MinSilenceLenMs)
	if err != nil {
		return nil, err
	}
	defer sf.close()

	return drainSilences(input, sf)
}

func (p *Processor) injectPauses(inputPath, outputPath string, gaps []SilenceRange) error {
	tc, err := newTranscoder(inputPath, outputPath)
	if err != nil {
		return fmt.Errorf("open transcoder: %w", err)
	}
	defer tc.close()

	if err := tc.processSegments(gaps, p.cfg.MinPauseMs); err != nil {
		return fmt.Errorf("process segments: %w", err)
	}

	return nil
}
