//go:build !statigo

// Package audio provides FFmpeg-based audio post-processing for D&D session narrations.
package audio

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"

	"dnd-workflow/internal/config"
	"dnd-workflow/internal/fileutil"
)

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

	duration, err := p.probeDuration(ctx, inputPath)
	if err != nil {
		slog.Warn("ffprobe failed, estimating duration from silence ranges", "error", err)
		duration = estimateDuration(shortGaps)
	}

	return p.injectPauses(ctx, inputPath, outputPath, shortGaps, duration)
}

func (p *Processor) DetectSilences(ctx context.Context, audioPath string) ([]SilenceRange, error) {
	args := []string{
		"-i", audioPath,
		"-af", fmt.Sprintf("silencedetect=noise=%ddB:d=%f",
			p.cfg.SilenceThreshDB,
			float64(p.cfg.MinSilenceLenMs)/1000.0),
		"-f", "null", "-",
	}

	cmd := exec.CommandContext(ctx, p.cfg.FFmpegPath, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("ffmpeg silencedetect: %w\n%s", err, string(output))
	}

	return parseSilenceOutput(string(output)), nil
}

func (p *Processor) probeDuration(ctx context.Context, audioPath string) (float64, error) {
	return ProbeDuration(ctx, p.cfg.FFprobePath, audioPath)
}

func (p *Processor) injectPauses(ctx context.Context, inputPath, outputPath string, gaps []SilenceRange, totalDuration float64) error {
	targetSec := float64(p.cfg.MinPauseMs) / 1000.0
	segFilter, segCount := buildSegmentFilter(gaps, targetSec, totalDuration)

	nullSrc := fmt.Sprintf("anullsrc=r=%d:cl=%s", p.cfg.PadSampleRate, p.cfg.PadChannelLayout)
	args := []string{
		"-i", inputPath,
		"-f", "lavfi", "-i", nullSrc,
		"-filter_complex", segFilter,
		"-map", "[out]",
		"-b:a", p.cfg.OutputBitrate,
		"-y", outputPath,
	}

	slog.Info("processing audio segments", "count", segCount)
	cmd := exec.CommandContext(ctx, p.cfg.FFmpegPath, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg: %w\n%s", err, string(output))
	}

	return nil
}
