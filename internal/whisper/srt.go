package whisper

import (
	"fmt"
	"os"
	"strings"
)

func WriteSRT(segments []Segment, outputPath string) error {
	var sb strings.Builder
	idx := 0

	for _, seg := range segments {
		text := strings.TrimSpace(seg.Text)
		if text == "" {
			continue
		}
		idx++
		sb.WriteString(fmt.Sprintf("%d\n", idx))
		sb.WriteString(fmt.Sprintf("%s --> %s\n", formatSRTTime(seg.Start), formatSRTTime(seg.End)))
		sb.WriteString(text + "\n\n")
	}

	return os.WriteFile(outputPath, []byte(sb.String()), 0o644)
}

func formatSRTTime(seconds float64) string {
	hours := int(seconds) / 3600
	minutes := (int(seconds) % 3600) / 60
	secs := int(seconds) % 60
	millis := int((seconds - float64(int(seconds))) * 1000)
	return fmt.Sprintf("%02d:%02d:%02d,%03d", hours, minutes, secs, millis)
}
