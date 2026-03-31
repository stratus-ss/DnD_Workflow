package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDefaults(t *testing.T) {
	content := `whisper:
  url: "https://whisper.test"
perplexity:
  prompt_file: "prompts/test.txt"
tts:
  url: "https://tts.test"
wikijs:
  url: "http://wiki.test"
  base_path: "test"
`
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")
	os.WriteFile(cfgPath, []byte(content), 0o644)

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.Whisper.PollIntervalSec != 5 {
		t.Errorf("PollIntervalSec = %d, want 5", cfg.Whisper.PollIntervalSec)
	}
	if cfg.Whisper.BeamSize != 5 {
		t.Errorf("BeamSize = %d, want 5", cfg.Whisper.BeamSize)
	}
	if cfg.Audio.MinPauseMs != 600 {
		t.Errorf("MinPauseMs = %d, want 600", cfg.Audio.MinPauseMs)
	}
	if cfg.Audio.SilenceThreshDB != -40 {
		t.Errorf("SilenceThreshDB = %d, want -40", cfg.Audio.SilenceThreshDB)
	}
	if cfg.Audio.MinSilenceLenMs != 150 {
		t.Errorf("MinSilenceLenMs = %d, want 150", cfg.Audio.MinSilenceLenMs)
	}
	if cfg.OutputDir != "./output" {
		t.Errorf("OutputDir = %q, want %q", cfg.OutputDir, "./output")
	}
}

func TestLoadValidationMissingWhisperURL(t *testing.T) {
	content := `tts:
  url: "https://tts.test"
wikijs:
  url: "http://wiki.test"
perplexity:
  prompt_file: "test.txt"
`
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")
	os.WriteFile(cfgPath, []byte(content), 0o644)

	_, err := Load(cfgPath)
	if err == nil {
		t.Fatal("expected validation error for missing whisper.url")
	}
}

func TestLoadValidationMissingTTSURL(t *testing.T) {
	content := `whisper:
  url: "https://whisper.test"
wikijs:
  url: "http://wiki.test"
perplexity:
  prompt_file: "test.txt"
`
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")
	os.WriteFile(cfgPath, []byte(content), 0o644)

	_, err := Load(cfgPath)
	if err == nil {
		t.Fatal("expected validation error for missing tts.url")
	}
}

func TestLoadValidationPositiveSilenceThresh(t *testing.T) {
	content := `whisper:
  url: "https://whisper.test"
tts:
  url: "https://tts.test"
wikijs:
  url: "http://wiki.test"
perplexity:
  prompt_file: "test.txt"
audio:
  silence_thresh_db: 10
`
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")
	os.WriteFile(cfgPath, []byte(content), 0o644)

	_, err := Load(cfgPath)
	if err == nil {
		t.Fatal("expected validation error for positive silence_thresh_db")
	}
}

func TestExpandHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("cannot determine home dir")
	}

	got := expandHome("~/test/path")
	expected := filepath.Join(home, "test/path")
	if got != expected {
		t.Errorf("expandHome(~/test/path) = %q, want %q", got, expected)
	}

	got = expandHome("/absolute/path")
	if got != "/absolute/path" {
		t.Errorf("expandHome(/absolute/path) = %q, want %q", got, "/absolute/path")
	}
}

func TestSessionOutputDir(t *testing.T) {
	cfg := &Config{OutputDir: "/tmp/output"}
	got := cfg.SessionOutputDir("2026-03-15")
	if got != "/tmp/output/2026-03-15" {
		t.Errorf("SessionOutputDir = %q, want %q", got, "/tmp/output/2026-03-15")
	}
}

func TestEnsureSessionDir(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := &Config{OutputDir: tmpDir}

	dir, err := cfg.EnsureSessionDir("test-date")
	if err != nil {
		t.Fatalf("EnsureSessionDir: %v", err)
	}

	expected := filepath.Join(tmpDir, "test-date")
	if dir != expected {
		t.Errorf("dir = %q, want %q", dir, expected)
	}

	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("directory not created: %v", err)
	}
	if !info.IsDir() {
		t.Error("expected directory")
	}
}

func TestEnvOverride(t *testing.T) {
	content := `whisper:
  url: "https://whisper.test"
tts:
  url: "https://tts.test"
wikijs:
  url: "http://wiki.test"
perplexity:
  prompt_file: "test.txt"
output_dir: "./default"
`
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")
	os.WriteFile(cfgPath, []byte(content), 0o644)

	t.Setenv("DND_OUTPUT_DIR", "/custom/output")

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.OutputDir != "/custom/output" {
		t.Errorf("OutputDir = %q, want %q", cfg.OutputDir, "/custom/output")
	}
}

func TestWikiJSToken(t *testing.T) {
	t.Setenv("WIKIJS_TOKEN", "test-token-123")
	if got := WikiJSToken(); got != "test-token-123" {
		t.Errorf("WikiJSToken() = %q, want %q", got, "test-token-123")
	}
}
