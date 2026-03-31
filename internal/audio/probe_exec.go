//go:build !statigo

package audio

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// ProbeDuration returns the duration in seconds of an audio file using ffprobe.
// It first tries format-level duration metadata; if unavailable (e.g. raw FLAC),
// it falls back to reading the last packet timestamp.
func ProbeDuration(ctx context.Context, ffprobePath, audioPath string) (float64, error) {
	if ffprobePath == "" {
		ffprobePath = "ffprobe"
	}
	if d, err := probeFormatDuration(ctx, ffprobePath, audioPath); err == nil {
		return d, nil
	}
	return probeLastPacket(ctx, ffprobePath, audioPath)
}

func probeFormatDuration(ctx context.Context, bin, path string) (float64, error) {
	cmd := exec.CommandContext(ctx, bin,
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1",
		path,
	)
	out, err := cmd.Output()
	if err != nil {
		return 0, err
	}
	return strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
}

func probeLastPacket(ctx context.Context, bin, path string) (float64, error) {
	cmd := exec.CommandContext(ctx, bin,
		"-v", "error",
		"-select_streams", "a:0",
		"-show_entries", "packet=pts_time",
		"-of", "default=noprint_wrappers=1:nokey=1",
		"-read_intervals", "99999%+#1",
		path,
	)
	out, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("ffprobe last-packet fallback: %w", err)
	}
	return strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
}
