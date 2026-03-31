package whisper

import (
	"os"
	"strings"
	"testing"
)

func TestFormatSRTTime(t *testing.T) {
	tests := []struct {
		seconds  float64
		expected string
	}{
		{0.0, "00:00:00,000"},
		{1.5, "00:00:01,500"},
		{61.125, "00:01:01,125"},
		{3661.25, "01:01:01,250"},
		{7200.0, "02:00:00,000"},
	}

	for _, tc := range tests {
		got := formatSRTTime(tc.seconds)
		if got != tc.expected {
			t.Errorf("formatSRTTime(%f) = %q, want %q", tc.seconds, got, tc.expected)
		}
	}
}

func TestWriteSRT(t *testing.T) {
	segments := []Segment{
		{Start: 0.0, End: 1.5, Text: "Hello world"},
		{Start: 1.5, End: 3.0, Text: ""},
		{Start: 3.0, End: 5.0, Text: "  "},
		{Start: 5.0, End: 7.5, Text: "Second line"},
	}

	tmpFile := t.TempDir() + "/test.srt"
	if err := WriteSRT(segments, tmpFile); err != nil {
		t.Fatalf("WriteSRT: %v", err)
	}

	data, err := os.ReadFile(tmpFile)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	content := string(data)

	if !strings.Contains(content, "1\n00:00:00,000 --> 00:00:01,500\nHello world") {
		t.Error("first segment not found or incorrect")
	}
	if !strings.Contains(content, "2\n00:00:05,000 --> 00:00:07,500\nSecond line") {
		t.Errorf("second segment should be index 2 (sequential), got:\n%s", content)
	}
	if strings.Contains(content, "3\n") {
		t.Error("should only have 2 entries (empty segments skipped)")
	}
}

func TestWriteSRTEmpty(t *testing.T) {
	tmpFile := t.TempDir() + "/empty.srt"
	if err := WriteSRT(nil, tmpFile); err != nil {
		t.Fatalf("WriteSRT(nil): %v", err)
	}

	data, err := os.ReadFile(tmpFile)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(data) != 0 {
		t.Errorf("expected empty file, got %d bytes", len(data))
	}
}
