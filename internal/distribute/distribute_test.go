// Package distribute tests file distribution logic.
package distribute

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"dnd-workflow/internal/config"
)

func TestDistribute_TranscriptCopy(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	src := filepath.Join(srcDir, "transcript_2026-03-15.srt.txt")
	if err := os.WriteFile(src, []byte("1\n00:00:00 --> 00:00:05\nHello world."), 0o644); err != nil {
		t.Fatalf("write src file: %v", err)
	}

	cfg := config.DistributeConfig{
		TranscriptDir: dstDir,
	}
	d := New(cfg)

	if err := d.Distribute(context.Background(), src, "", "2026-03-15"); err != nil {
		t.Fatalf("Distribute failed: %v", err)
	}

	got := filepath.Join(dstDir, "Mar_15_2026.srt.txt")
	data, err := os.ReadFile(got)
	if err != nil {
		t.Fatalf("read distributed file: %v", err)
	}
	if string(data) != "1\n00:00:00 --> 00:00:05\nHello world." {
		t.Errorf("content mismatch: %q", string(data))
	}
}

func TestDistribute_AudioCopy(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	src := filepath.Join(srcDir, "narration_raw_2026-03-15.mp3")
	if err := os.WriteFile(src, []byte("fake mp3 audio data"), 0o644); err != nil {
		t.Fatalf("write src file: %v", err)
	}

	cfg := config.DistributeConfig{
		AudioDir: dstDir,
	}
	d := New(cfg)

	if err := d.Distribute(context.Background(), "", src, "2026-03-15"); err != nil {
		t.Fatalf("Distribute failed: %v", err)
	}

	got := filepath.Join(dstDir, "session_recap_Mar_15_2026.mp3")
	data, err := os.ReadFile(got)
	if err != nil {
		t.Fatalf("read distributed file: %v", err)
	}
	if string(data) != "fake mp3 audio data" {
		t.Errorf("content mismatch: %q", string(data))
	}
}

func TestDistribute_SkipEmptyConfig(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	src := filepath.Join(srcDir, "transcript_2026-03-15.srt.txt")
	if err := os.WriteFile(src, []byte("test"), 0o644); err != nil {
		t.Fatalf("write src file: %v", err)
	}

	// Both dirs empty — should be a no-op (no error, no file created).
	cfg := config.DistributeConfig{}
	d := New(cfg)

	if err := d.Distribute(context.Background(), src, "", "2026-03-15"); err != nil {
		t.Fatalf("Distribute should not fail on empty config: %v", err)
	}

	// Nothing should be written anywhere.
	if entries, _ := os.ReadDir(dstDir); len(entries) > 0 {
		t.Errorf("expected no files written, got %v", entries)
	}
}

func TestMoveOriginalAudio(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	src := filepath.Join(srcDir, "session.m4a")
	if err := os.WriteFile(src, []byte("original audio bytes"), 0o644); err != nil {
		t.Fatalf("write src file: %v", err)
	}

	cfg := config.DistributeConfig{
		OriginalAudioDir: dstDir,
	}
	d := New(cfg)

	if err := d.MoveOriginalAudio(context.Background(), src, "2026-03-15"); err != nil {
		t.Fatalf("MoveOriginalAudio failed: %v", err)
	}

	got := filepath.Join(dstDir, "Mar_15_2026session.m4a")
	data, err := os.ReadFile(got)
	if err != nil {
		t.Fatalf("read moved file: %v", err)
	}
	if string(data) != "original audio bytes" {
		t.Errorf("content mismatch: %q", string(data))
	}

}

func TestDistribute_DateFormat(t *testing.T) {
	tests := []struct {
		date     string
		expected string
	}{
		{"2026-03-15", "Mar_15_2026"},
		{"2026-01-01", "Jan_01_2026"},
		{"2026-12-31", "Dec_31_2026"},
	}

	for _, tc := range tests {
		got, err := toDisplayDate(tc.date)
		if err != nil {
			t.Errorf("toDisplayDate(%q) returned error: %v", tc.date, err)
			continue
		}
		if got != tc.expected {
			t.Errorf("toDisplayDate(%q) = %q, want %q", tc.date, got, tc.expected)
		}
	}
}
