package audio

import (
	"context"
	"os"
	"testing"

	"dnd-workflow/internal/config"
)

func TestProcessRealAudio(t *testing.T) {
	inputPath := "../../example_files/session_recapp_Mar_15_2026.m4a"
	if _, err := os.Stat(inputPath); err != nil {
		t.Skip("example m4a not found")
	}

	ctx := context.Background()
	proc := NewProcessor(testAudioConfig())

	silences, err := proc.DetectSilences(ctx, inputPath)
	if err != nil {
		t.Fatalf("DetectSilences: %v", err)
	}
	t.Logf("Detected %d silence regions", len(silences))

	if len(silences) > 0 {
		t.Logf("First silence: %.2fs - %.2fs (%.0fms)",
			silences[0].StartSec, silences[0].EndSec,
			(silences[0].EndSec-silences[0].StartSec)*1000)
		last := silences[len(silences)-1]
		t.Logf("Last silence: %.2fs - %.2fs (%.0fms)",
			last.StartSec, last.EndSec,
			(last.EndSec-last.StartSec)*1000)
	}

	shortGaps := findShortGaps(silences, 600)
	t.Logf("Short gaps (<600ms): %d out of %d total", len(shortGaps), len(silences))

	outputPath := "/tmp/dnd_audio_processed.m4a"
	defer os.Remove(outputPath)

	if err := proc.Process(ctx, inputPath, outputPath); err != nil {
		t.Fatalf("Process: %v", err)
	}

	outInfo, err := os.Stat(outputPath)
	if err != nil {
		t.Fatalf("output missing: %v", err)
	}

	inInfo, _ := os.Stat(inputPath)
	t.Logf("Input: %.1f MB, Output: %.1f MB",
		float64(inInfo.Size())/1024/1024,
		float64(outInfo.Size())/1024/1024)
}

func testAudioConfig() config.AudioConfig {
	return config.AudioConfig{
		MinPauseMs:       600,
		SilenceThreshDB:  -40,
		MinSilenceLenMs:  150,
		OutputBitrate:    "170k",
		OutputFormat:     "mp3",
		PadSampleRate:    44100,
		PadChannelLayout: "mono",
		TargetLoudness:   -16,
		FFmpegPath:       "ffmpeg",
		FFprobePath:      "ffprobe",
	}
}
