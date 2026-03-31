package tts

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	doubleNewlineRe = regexp.MustCompile(`\n\s*\n`)
	headingPrefixRe = regexp.MustCompile(`^#{1,6}\s+`)
)

// InsertPauseTags adds SML pause tags inline between paragraphs.
// Headings get sectionPause seconds; regular paragraphs get paragraphPause.
// Markdown heading markers (#) are stripped so they aren't spoken aloud.
// Tags are space-delimited with no double newlines to avoid conflicting
// with ebook2audiobook's internal text preprocessing.
func InsertPauseTags(text string, paragraphPause, sectionPause float64) string {
	paragraphs := splitParagraphs(text)

	var result strings.Builder
	for i, para := range paragraphs {
		trimmed := strings.TrimSpace(para)
		isHeading := strings.HasPrefix(trimmed, "#")

		if isHeading {
			para = stripHeadingMarkers(para)
		}

		if i > 0 {
			pause := paragraphPause
			if isHeading {
				pause = sectionPause
			}
			result.WriteString(fmt.Sprintf(" [pause:%.1f] ", pause))
		}
		result.WriteString(strings.TrimSpace(para))
	}

	return result.String()
}

func stripHeadingMarkers(s string) string {
	return headingPrefixRe.ReplaceAllString(strings.TrimSpace(s), "")
}

func splitParagraphs(text string) []string {
	parts := doubleNewlineRe.Split(text, -1)

	var result []string
	for _, p := range parts {
		if strings.TrimSpace(p) != "" {
			result = append(result, p)
		}
	}
	return result
}
