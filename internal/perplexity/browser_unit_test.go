package perplexity

import (
	"testing"
)

func TestParseSummaryNormal(t *testing.T) {
	input := `# Session Narration

The party entered the dungeon and found a treasure chest.
They fought a dragon and won.

# DM Summary

## Session Overview
The party cleared the dungeon.`

	narration := ParseSummary(input)
	if narration == "" {
		t.Fatal("ParseSummary returned empty")
	}
	if narration != "The party entered the dungeon and found a treasure chest.\nThey fought a dragon and won." {
		t.Errorf("unexpected narration: %q", narration)
	}
}

func TestParseSummaryEmpty(t *testing.T) {
	narration := ParseSummary("")
	if narration != "" {
		t.Errorf("expected empty, got %q", narration)
	}
}

func TestParseSummaryNoHeading(t *testing.T) {
	narration := ParseSummary("Just some random text without headings")
	if narration != "" {
		t.Errorf("expected empty for no heading, got %q", narration)
	}
}

func TestParseSummaryNoDMSummary(t *testing.T) {
	input := `## Session Narration

All the narration text goes here until the end of the document.
Multiple lines of narration content.`

	narration := ParseSummary(input)
	if narration == "" {
		t.Fatal("ParseSummary returned empty for narration-only content")
	}
	if narration != "All the narration text goes here until the end of the document.\nMultiple lines of narration content." {
		t.Errorf("unexpected: %q", narration)
	}
}

func TestParseSummaryEmojiHeadings(t *testing.T) {
	input := "# 🎙️ Session Narration — April 26, 2026\n\n" +
		"The party explored the dungeon.\n\n" +
		"***\n***\n\n" +
		"# 📋 DM Summary — April 26, 2026\n\n" +
		"## Session Overview\nThey cleared it."
	narration := ParseSummary(input)
	if narration == "" {
		t.Fatal("ParseSummary returned empty for emoji headings")
	}
}

func TestJsStringLiteral(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"hello", "`hello`"},
		{"has `backtick`", "`has \\`backtick\\``"},
		{"has $dollar", "`has \\$dollar`"},
		{`has \backslash`, "`has \\\\backslash`"},
	}

	for _, tc := range tests {
		got := jsStringLiteral(tc.input)
		if got != tc.expected {
			t.Errorf("jsStringLiteral(%q) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}

func TestLoadPrompt(t *testing.T) {
	text, err := LoadPrompt("../../prompts/session_notes.txt", "2026-03-15")
	if err != nil {
		t.Skip("prompt file not found")
	}
	if text == "" {
		t.Fatal("LoadPrompt returned empty")
	}
	if !contains(text, "2026-03-15") {
		t.Error("session date not substituted")
	}
	if contains(text, "{SESSION_DATE}") {
		t.Error("placeholder not replaced")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstr(s, substr))
}

func containsSubstr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
