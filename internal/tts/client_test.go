package tts

import (
	"context"
	"os"
	"testing"

	"dnd-workflow/internal/config"
)

func TestConvertTextToAudio(t *testing.T) {
	if os.Getenv("E2A_INTEGRATION") == "" {
		t.Skip("set E2A_INTEGRATION=1 to run")
	}

	cfg := testTTSConfig()
	cfg.Device = "cuda"
	cfg.TTSEngine = "xtts"
	cfg.Voice = "SampleVoice"
	cfg.FineTuned = "SampleVoice"
	cfg.OutputFormat = "m4a"
	cfg.Language = "eng"

	client := NewClient(cfg)
	text := "The party ventured deeper into the ancient ruins. Torches flickered against stone walls as they descended."
	outPath := "/tmp/dnd_tts_test.m4a"
	defer os.Remove(outPath)

	if err := client.ConvertTextToAudio(context.Background(), text, outPath); err != nil {
		t.Fatalf("ConvertTextToAudio: %v", err)
	}

	info, err := os.Stat(outPath)
	if err != nil {
		t.Fatalf("output file missing: %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("output file is empty")
	}
	t.Logf("Generated audio: %s (%d bytes)", outPath, info.Size())
}

func testTTSConfig() config.TTSConfig {
	return config.TTSConfig{
		URL:               "https://e2a.example.com",
		TLSSkipVerify:     true,
		Device:            "cpu",
		Language:          "eng",
		OutputFormat:      "mp3",
		Speed:             1.0,
		Temperature:       0.75,
		ConvertTimeoutMin: 60,
		HTTPTimeoutMin:    60,
		GradioAPIPrefix:   "/gradio_api",
		RepetitionPenalty: 1.0,
		NumBeams:          1,
		LengthPenalty:     3.0,
		OutputChannel: "mono",
		GradioAPINames: config.GradioAPINames{
			CreateSession: "/change_gr_restore_session",
			RestoreUI:     "/restore_interface",
			SetEbook:      "/change_gr_ebook_file",
			SubmitConvert: "/start_conversion",
			RefreshUI:     "/refresh_interface",
			AudiobookPlayer: "/update_gr_audiobook_player",
		},
	}
}
