package audio

import (
	"testing"
)

func TestParseSilenceOutput(t *testing.T) {
	output := `[silencedetect @ 0x1234] silence_start: 1.504
[silencedetect @ 0x1234] silence_end: 1.784 | silence_duration: 0.280
[silencedetect @ 0x1234] silence_start: 5.123
[silencedetect @ 0x1234] silence_end: 5.500 | silence_duration: 0.377`

	silences := parseSilenceOutput(output)
	if len(silences) != 2 {
		t.Fatalf("expected 2 silences, got %d", len(silences))
	}

	if silences[0].StartSec != 1.504 || silences[0].EndSec != 1.784 {
		t.Errorf("silence[0] = {%.3f, %.3f}, want {1.504, 1.784}", silences[0].StartSec, silences[0].EndSec)
	}
	if silences[1].StartSec != 5.123 || silences[1].EndSec != 5.500 {
		t.Errorf("silence[1] = {%.3f, %.3f}, want {5.123, 5.500}", silences[1].StartSec, silences[1].EndSec)
	}
}

func TestParseSilenceOutputEmpty(t *testing.T) {
	silences := parseSilenceOutput("no silence detected in this output")
	if len(silences) != 0 {
		t.Errorf("expected 0 silences, got %d", len(silences))
	}
}

func TestParseSilenceOutputMismatchedPairs(t *testing.T) {
	output := `[silencedetect] silence_start: 1.0
[silencedetect] silence_end: 2.0
[silencedetect] silence_start: 3.0`

	silences := parseSilenceOutput(output)
	if len(silences) != 1 {
		t.Fatalf("expected 1 silence (mismatched pair trimmed), got %d", len(silences))
	}
}

func TestFindShortGaps(t *testing.T) {
	silences := []SilenceRange{
		{StartSec: 0.0, EndSec: 0.3},
		{StartSec: 1.0, EndSec: 1.8},
		{StartSec: 3.0, EndSec: 3.4},
	}

	short := findShortGaps(silences, 600)
	if len(short) != 2 {
		t.Fatalf("expected 2 short gaps (<600ms), got %d", len(short))
	}
	if short[0].StartSec != 0.0 {
		t.Errorf("first short gap should start at 0.0, got %.1f", short[0].StartSec)
	}
	if short[1].StartSec != 3.0 {
		t.Errorf("second short gap should start at 3.0, got %.1f", short[1].StartSec)
	}
}

func TestBuildSegmentFilter(t *testing.T) {
	gaps := []SilenceRange{
		{StartSec: 2.0, EndSec: 2.3},
	}

	filter, segCount := buildSegmentFilter(gaps, 0.6, 10.0)
	if segCount == 0 {
		t.Fatal("expected segments > 0")
	}
	if filter == "" {
		t.Fatal("expected non-empty filter string")
	}
	if segCount < 3 {
		t.Errorf("expected at least 3 segments (before, silence, after), got %d", segCount)
	}
}

func TestBuildSegmentFilterNoGaps(t *testing.T) {
	filter, segCount := buildSegmentFilter(nil, 0.6, 10.0)
	if segCount != 1 {
		t.Errorf("expected 1 segment (full audio passthrough), got %d", segCount)
	}
	_ = filter
}

func TestEstimateDuration(t *testing.T) {
	gaps := []SilenceRange{
		{StartSec: 5.0, EndSec: 10.0},
	}
	d := estimateDuration(gaps)
	if d != 70.0 {
		t.Errorf("expected 70.0 (10.0 + 60), got %.1f", d)
	}

	d = estimateDuration(nil)
	if d != 0 {
		t.Errorf("expected 0 for nil, got %.1f", d)
	}
}
