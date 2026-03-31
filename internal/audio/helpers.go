package audio

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

func findShortGaps(silences []SilenceRange, minPauseMs int) []SilenceRange {
	targetSec := float64(minPauseMs) / 1000.0
	var shortGaps []SilenceRange

	for _, s := range silences {
		duration := s.EndSec - s.StartSec
		if duration < targetSec {
			shortGaps = append(shortGaps, s)
		}
	}

	return shortGaps
}

func estimateDuration(gaps []SilenceRange) float64 {
	if len(gaps) == 0 {
		return 0
	}
	return gaps[len(gaps)-1].EndSec + 60
}

func parseSilenceOutput(output string) []SilenceRange {
	startRe := regexp.MustCompile(`silence_start:\s*([\d.]+)`)
	endRe := regexp.MustCompile(`silence_end:\s*([\d.]+)`)

	starts := startRe.FindAllStringSubmatch(output, -1)
	ends := endRe.FindAllStringSubmatch(output, -1)

	count := len(starts)
	if len(ends) < count {
		count = len(ends)
	}

	silences := make([]SilenceRange, 0, count)
	for i := 0; i < count; i++ {
		startVal, _ := strconv.ParseFloat(starts[i][1], 64)
		endVal, _ := strconv.ParseFloat(ends[i][1], 64)
		silences = append(silences, SilenceRange{StartSec: startVal, EndSec: endVal})
	}

	return silences
}

func buildSegmentFilter(gaps []SilenceRange, targetSec, totalDuration float64) (string, int) {
	var parts []string
	var labels []string
	segIdx := 0
	cursor := 0.0

	for _, gap := range gaps {
		midpoint := (gap.StartSec + gap.EndSec) / 2.0
		padDur := targetSec - (gap.EndSec - gap.StartSec)
		if padDur < 0.01 {
			continue
		}

		if midpoint > cursor {
			aLabel := fmt.Sprintf("[a%d]", segIdx)
			trim := fmt.Sprintf("[0:a]atrim=%.4f:%.4f,asetpts=PTS-STARTPTS%s",
				cursor, midpoint, aLabel)
			parts = append(parts, trim)
			labels = append(labels, aLabel)
			segIdx++
		}

		padMs := int(math.Round(padDur * 1000))
		sLabel := fmt.Sprintf("[s%d]", segIdx)
		silGen := fmt.Sprintf("[1:a]atrim=0:%.4f,asetpts=PTS-STARTPTS%s",
			float64(padMs)/1000.0, sLabel)
		parts = append(parts, silGen)
		labels = append(labels, sLabel)
		segIdx++

		cursor = midpoint
	}

	if cursor < totalDuration {
		aLabel := fmt.Sprintf("[a%d]", segIdx)
		trim := fmt.Sprintf("[0:a]atrim=%.4f,asetpts=PTS-STARTPTS%s",
			cursor, aLabel)
		parts = append(parts, trim)
		labels = append(labels, aLabel)
		segIdx++
	}

	concatInput := strings.Join(labels, "")
	concat := fmt.Sprintf("%sconcat=n=%d:v=0:a=1[out]", concatInput, len(labels))
	parts = append(parts, concat)

	return strings.Join(parts, ";\n"), segIdx
}
