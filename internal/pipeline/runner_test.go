package pipeline

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"dnd-workflow/internal/config"
)

type mockTranscriber struct {
	called bool
	err    error
}

func (m *mockTranscriber) Transcribe(ctx context.Context, audioPath, outputPath string) error {
	m.called = true
	if m.err != nil {
		return m.err
	}
	return os.WriteFile(outputPath, []byte("1\n00:00:00,000 --> 00:00:01,000\ntest\n"), 0o644)
}

type mockNotesGen struct {
	started bool
	closed  bool
	err     error
}

func (m *mockNotesGen) Start() error {
	m.started = true
	return nil
}

func (m *mockNotesGen) Close() {
	m.closed = true
}

func (m *mockNotesGen) GenerateNotesInThread(srtPath, promptText, threadName string) (string, string, error) {
	if m.err != nil {
		return "", "", m.err
	}
	return "# Full Notes\n\nContent here.", "Narration text for TTS.", nil
}

type mockSpeaker struct {
	called bool
	err    error
}

func (m *mockSpeaker) ConvertTextToAudio(ctx context.Context, text, outputPath string, cfg interface{}) error {
	m.called = true
	if m.err != nil {
		return m.err
	}
	return os.WriteFile(outputPath, []byte("fake audio"), 0o644)
}

type mockAudioFixer struct {
	called bool
	err    error
}

func (m *mockAudioFixer) Process(ctx context.Context, inputPath, outputPath string) error {
	m.called = true
	if m.err != nil {
		return m.err
	}
	return os.WriteFile(outputPath, []byte("fixed audio"), 0o644)
}

type mockPublisher struct {
	createCalled bool
	checkCalled  bool
	pageExists   bool
	err          error
}

func (m *mockPublisher) CreatePage(ctx context.Context, title, path, content string, tags []string) (int, error) {
	m.createCalled = true
	if m.err != nil {
		return 0, m.err
	}
	return 42, nil
}

func (m *mockPublisher) CheckPageExists(ctx context.Context, path string) (bool, error) {
	m.checkCalled = true
	return m.pageExists, nil
}

func testConfig(dir string) *config.Config {
	t := true
	return &config.Config{
		Whisper: config.WhisperConfig{
			URL: "https://test.whisper",
		},
		Perplexity: config.PerplexityConfig{
			PromptFile: filepath.Join(dir, "prompt.txt"),
			ThreadName: "test-thread",
		},
		TTS: config.TTSConfig{
			URL: "https://test.tts",
		},
		Audio: config.AudioConfig{
			OutputFormat:      "mp3",
			ParagraphPauseSec: 1.5,
			SectionPauseSec:   3.0,
		},
		WikiJS: config.WikiJSConfig{
			URL:               "http://test.wiki",
			BasePath:          "test/path",
			Locale:            "en",
			Editor:            "markdown",
			IsPublished:       &t,
			Tags:              []string{"test"},
			PageTitleTemplate: "Test - {date}",
		},
		OutputDir: dir,
	}
}

func TestRunFromAll(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(dir)
	os.WriteFile(filepath.Join(dir, "prompt.txt"), []byte("test prompt {SESSION_DATE}"), 0o644)

	t.Setenv("WIKIJS_TOKEN", "fake-token")

	tr := &mockTranscriber{}
	ng := &mockNotesGen{}
	sp := &mockSpeaker{}
	af := &mockAudioFixer{}
	pub := &mockPublisher{}

	r := NewRunner(cfg, tr, ng, sp, af, pub)
	r.SetForce(true)

	if err := r.RunFrom(context.Background(), "/fake/audio.flac", "2026-03-15", "all", false); err != nil {
		t.Fatalf("RunFrom all: %v", err)
	}

	if !tr.called {
		t.Error("transcriber not called")
	}
	if !ng.started || !ng.closed {
		t.Error("notes generator not started/closed properly")
	}
	if !sp.called {
		t.Error("speaker not called")
	}
	if !af.called {
		t.Error("audio fixer not called")
	}
	if !pub.createCalled {
		t.Error("publisher not called")
	}
}

func TestRunFromPerplexity(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(dir)
	os.WriteFile(filepath.Join(dir, "prompt.txt"), []byte("test prompt {SESSION_DATE}"), 0o644)
	t.Setenv("WIKIJS_TOKEN", "fake-token")

	sessionDir := filepath.Join(dir, "2026-03-15")
	os.MkdirAll(sessionDir, 0o755)
	os.WriteFile(filepath.Join(sessionDir, "transcript_2026-03-15.srt.txt"), []byte("existing srt"), 0o644)

	tr := &mockTranscriber{}
	ng := &mockNotesGen{}
	sp := &mockSpeaker{}
	af := &mockAudioFixer{}
	pub := &mockPublisher{}

	r := NewRunner(cfg, tr, ng, sp, af, pub)
	r.SetForce(true)

	if err := r.RunFrom(context.Background(), "", "2026-03-15", "perplexity", true); err != nil {
		t.Fatalf("RunFrom perplexity: %v", err)
	}

	if tr.called {
		t.Error("transcriber should NOT be called when starting from perplexity")
	}
	if !ng.started {
		t.Error("notes generator should be called")
	}
	if !sp.called {
		t.Error("speaker should be called")
	}
}

func TestRunFromWiki(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(dir)
	t.Setenv("WIKIJS_TOKEN", "fake-token")

	sessionDir := filepath.Join(dir, "2026-03-15")
	os.MkdirAll(sessionDir, 0o755)
	os.WriteFile(filepath.Join(sessionDir, "transcript_2026-03-15.srt.txt"), []byte("srt"), 0o644)
	os.WriteFile(filepath.Join(sessionDir, "notes_2026-03-15.md"), []byte("# Notes"), 0o644)
	os.WriteFile(filepath.Join(sessionDir, "narration_2026-03-15.md"), []byte("narration"), 0o644)
	os.WriteFile(filepath.Join(sessionDir, "narration_raw_2026-03-15.mp3"), []byte("raw"), 0o644)

	tr := &mockTranscriber{}
	ng := &mockNotesGen{}
	sp := &mockSpeaker{}
	af := &mockAudioFixer{}
	pub := &mockPublisher{}

	r := NewRunner(cfg, tr, ng, sp, af, pub)
	r.SetForce(true)

	if err := r.RunFrom(context.Background(), "", "2026-03-15", "wiki", false); err != nil {
		t.Fatalf("RunFrom wiki: %v", err)
	}

	if tr.called || ng.started || sp.called || af.called {
		t.Error("only wiki step should run")
	}
	if !pub.createCalled {
		t.Error("publisher should be called")
	}
}

func TestRunFromMissingPriorOutput(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(dir)

	sessionDir := filepath.Join(dir, "2026-03-15")
	os.MkdirAll(sessionDir, 0o755)

	r := NewRunner(cfg, nil, nil, nil, nil, nil)
	err := r.RunFrom(context.Background(), "", "2026-03-15", "perplexity", false)
	if err == nil {
		t.Fatal("expected error for missing whisper output")
	}
}

func TestCheckpointSkipsExistingFiles(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(dir)
	t.Setenv("WIKIJS_TOKEN", "fake-token")
	os.WriteFile(filepath.Join(dir, "prompt.txt"), []byte("test {SESSION_DATE}"), 0o644)

	sessionDir := filepath.Join(dir, "2026-03-15")
	os.MkdirAll(sessionDir, 0o755)
	os.WriteFile(filepath.Join(sessionDir, "transcript_2026-03-15.srt.txt"), []byte("existing"), 0o644)

	tr := &mockTranscriber{}
	r := NewRunner(cfg, tr, &mockNotesGen{}, &mockSpeaker{}, &mockAudioFixer{}, &mockPublisher{})

	if err := r.RunFrom(context.Background(), "/fake/audio.flac", "2026-03-15", "all", false); err != nil {
		t.Fatalf("RunFrom: %v", err)
	}
	if tr.called {
		t.Error("transcriber should have been skipped (checkpoint)")
	}
}

func TestForceOverridesCheckpoint(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(dir)
	t.Setenv("WIKIJS_TOKEN", "fake-token")
	os.WriteFile(filepath.Join(dir, "prompt.txt"), []byte("test {SESSION_DATE}"), 0o644)

	sessionDir := filepath.Join(dir, "2026-03-15")
	os.MkdirAll(sessionDir, 0o755)
	os.WriteFile(filepath.Join(sessionDir, "transcript_2026-03-15.srt.txt"), []byte("existing"), 0o644)

	tr := &mockTranscriber{}
	r := NewRunner(cfg, tr, &mockNotesGen{}, &mockSpeaker{}, &mockAudioFixer{}, &mockPublisher{})
	r.SetForce(true)

	if err := r.RunFrom(context.Background(), "/fake/audio.flac", "2026-03-15", "all", false); err != nil {
		t.Fatalf("RunFrom: %v", err)
	}
	if !tr.called {
		t.Error("transcriber should have been called with --force")
	}
}

func TestWikiDedupSkipsExistingPage(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(dir)
	t.Setenv("WIKIJS_TOKEN", "fake-token")

	pub := &mockPublisher{pageExists: true}
	r := NewRunner(cfg, nil, nil, nil, nil, pub)

	if err := r.RunPublish(context.Background(), "# notes", "2026-03-15"); err != nil {
		t.Fatalf("RunPublish: %v", err)
	}
	if !pub.checkCalled {
		t.Error("CheckPageExists not called")
	}
	if pub.createCalled {
		t.Error("CreatePage should not be called when page exists")
	}
}

func TestRunPublishMissingToken(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(dir)
	t.Setenv("WIKIJS_TOKEN", "")

	r := NewRunner(cfg, nil, nil, nil, nil, nil)
	err := r.RunPublish(context.Background(), "content", "2026-03-15")
	if err == nil {
		t.Fatal("expected error for missing token")
	}
}

func TestValidStep(t *testing.T) {
	for _, s := range []string{"all", "whisper", "perplexity", "tts", "audio", "wiki"} {
		if !ValidStep(s) {
			t.Errorf("ValidStep(%q) = false, want true", s)
		}
	}
	if ValidStep("invalid") {
		t.Error("ValidStep(invalid) = true, want false")
	}
}

func TestRunSingleStep(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(dir)
	os.WriteFile(filepath.Join(dir, "prompt.txt"), []byte("test prompt {SESSION_DATE}"), 0o644)

	sessionDir := filepath.Join(dir, "2026-03-15")
	os.MkdirAll(sessionDir, 0o755)
	os.WriteFile(filepath.Join(sessionDir, "transcript_2026-03-15.srt.txt"), []byte("existing srt"), 0o644)

	tr := &mockTranscriber{}
	ng := &mockNotesGen{}
	sp := &mockSpeaker{}
	af := &mockAudioFixer{}
	pub := &mockPublisher{}

	r := NewRunner(cfg, tr, ng, sp, af, pub)
	r.SetForce(true)

	if err := r.RunFrom(context.Background(), "", "2026-03-15", "perplexity", false); err != nil {
		t.Fatalf("RunFrom single step: %v", err)
	}

	if tr.called {
		t.Error("transcriber should NOT run for single perplexity step")
	}
	if !ng.started {
		t.Error("notes generator should be called")
	}
	if sp.called {
		t.Error("speaker should NOT run without --continue")
	}
	if af.called {
		t.Error("audio fixer should NOT run without --continue")
	}
	if pub.createCalled {
		t.Error("publisher should NOT run without --continue")
	}
}

func TestPageTitleUsesTemplate(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(dir)
	title := cfg.WikiPageTitle("2026-03-15")
	if title != "Test - 2026-03-15" {
		t.Errorf("WikiPageTitle = %q, want %q", title, "Test - 2026-03-15")
	}
}
