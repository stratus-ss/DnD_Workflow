package tts

import (
	"strings"
	"testing"
)

func TestInsertPauseTagsParagraphs(t *testing.T) {
	text := "First paragraph.\n\nSecond paragraph.\n\nThird paragraph."
	result := InsertPauseTags(text, 1.5, 3.0)

	if !strings.Contains(result, "[pause:1.5]") {
		t.Errorf("expected paragraph pause tag, got:\n%s", result)
	}
	if strings.Count(result, "[pause:1.5]") != 2 {
		t.Errorf("expected 2 paragraph pauses, got %d", strings.Count(result, "[pause:1.5]"))
	}
	if strings.Contains(result, "\n\n") {
		t.Errorf("should not contain double newlines, got:\n%s", result)
	}
}

func TestInsertPauseTagsHeading(t *testing.T) {
	text := "Intro paragraph.\n\n## Chapter 2\n\nContent here."
	result := InsertPauseTags(text, 1.5, 3.0)

	if !strings.Contains(result, "[pause:3.0]") {
		t.Errorf("expected section pause before heading, got:\n%s", result)
	}
	if !strings.Contains(result, "[pause:1.5]") {
		t.Errorf("expected paragraph pause after heading, got:\n%s", result)
	}
	if strings.Contains(result, "##") {
		t.Errorf("heading markers should be stripped, got:\n%s", result)
	}
}

func TestInsertPauseTagsHeadingStripped(t *testing.T) {
	text := "# Title\n\n## Subtitle\n\n### Deep heading\n\nContent."
	result := InsertPauseTags(text, 1.5, 3.0)

	if strings.Contains(result, "#") {
		t.Errorf("all heading markers should be stripped, got:\n%s", result)
	}
	if !strings.Contains(result, "Title") {
		t.Errorf("heading text should be preserved, got:\n%s", result)
	}
	if strings.Count(result, "[pause:3.0]") != 2 {
		t.Errorf("expected 2 section pauses, got %d in:\n%s",
			strings.Count(result, "[pause:3.0]"), result)
	}
	if strings.Count(result, "[pause:1.5]") != 1 {
		t.Errorf("expected 1 paragraph pause, got %d in:\n%s",
			strings.Count(result, "[pause:1.5]"), result)
	}
}

func TestInsertPauseTagsInlineFormat(t *testing.T) {
	text := "Para one.\n\nPara two."
	result := InsertPauseTags(text, 1.5, 3.0)

	expected := "Para one. [pause:1.5] Para two."
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestInsertPauseTagsSingleParagraph(t *testing.T) {
	text := "Just one paragraph with no breaks."
	result := InsertPauseTags(text, 1.5, 3.0)

	if strings.Contains(result, "[pause:") {
		t.Errorf("single paragraph should have no pauses, got:\n%s", result)
	}
	if result != text {
		t.Errorf("expected unchanged text, got:\n%s", result)
	}
}

func TestInsertPauseTagsEmpty(t *testing.T) {
	result := InsertPauseTags("", 1.5, 3.0)
	if result != "" {
		t.Errorf("expected empty string, got %q", result)
	}
}

func TestSplitParagraphs(t *testing.T) {
	text := "para1\n\npara2\n\n\n\npara3"
	parts := splitParagraphs(text)
	if len(parts) != 3 {
		t.Fatalf("expected 3 paragraphs, got %d: %v", len(parts), parts)
	}
}
