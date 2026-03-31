package tts

import (
	"context"
	"os"
	"strings"
	"testing"

	"dnd-workflow/internal/perplexity"
)

func TestConvertRealNarration(t *testing.T) {
	if os.Getenv("E2A_INTEGRATION") == "" {
		t.Skip("set E2A_INTEGRATION=1 to run")
	}

	data, err := os.ReadFile("../../example_files/example_summary.txt")
	if err != nil {
		t.Fatalf("read example: %v", err)
	}

	narration := perplexity.ParseSummary(string(data))
	if narration == "" {
		t.Fatal("ParseSummary returned empty")
	}

	words := len(strings.Fields(narration))
	t.Logf("Narration: %d words", words)

	cfg := testTTSConfig()
	cfg.Device = "cuda"
	cfg.TTSEngine = "xtts"
	cfg.Voice = "SampleVoice"
	cfg.FineTuned = "SampleVoice"
	cfg.OutputFormat = "m4a"
	cfg.Language = "eng"

	client := NewClient(cfg)
	outPath := "/tmp/dnd_real_narration.m4a"
	defer os.Remove(outPath)

	if err := client.ConvertTextToAudio(context.Background(), narration, outPath); err != nil {
		t.Fatalf("ConvertTextToAudio: %v", err)
	}

	info, err := os.Stat(outPath)
	if err != nil {
		t.Fatalf("output missing: %v", err)
	}

	t.Logf("Generated audio: %s (%.1f MB)", outPath, float64(info.Size())/1024/1024)

	exampleInfo, err := os.Stat("../../example_files/session_recapp_Mar_15_2026.m4a")
	if err == nil {
		t.Logf("Example audio: %.1f MB", float64(exampleInfo.Size())/1024/1024)
		ratio := float64(info.Size()) / float64(exampleInfo.Size())
		t.Logf("Size ratio (generated/example): %.2f", ratio)
	}
}
