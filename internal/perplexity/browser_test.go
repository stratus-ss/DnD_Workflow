package perplexity

import (
	"os"
	"strings"
	"testing"
)

func TestParseSummaryRealData(t *testing.T) {
	data, err := os.ReadFile("../../example_files/example_summary.txt")
	if err != nil {
		t.Skip("example_summary.txt not found")
	}

	narration := ParseSummary(string(data))
	if narration == "" {
		t.Fatal("ParseSummary returned empty string")
	}

	if strings.HasPrefix(narration, "Session Narration") {
		t.Errorf("narration should not start with heading, got: %.80s", narration)
	}

	if strings.Contains(narration, "DM Summary") {
		t.Error("narration should not contain 'DM Summary'")
	}

	if strings.Contains(narration, "Session Overview") {
		t.Error("narration should not contain DM summary sections")
	}

	words := len(strings.Fields(narration))
	t.Logf("Narration: %d words, %d chars", words, len(narration))

	if words < 500 || words > 2000 {
		t.Errorf("narration word count %d outside expected range (500-2000)", words)
	}
}
