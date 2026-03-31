package perplexity

import (
	"strings"

	htmltomarkdown "github.com/JohannesKaufmann/html-to-markdown/v2"
)

func convertHTMLToMarkdown(html string) (string, error) {
	md, err := htmltomarkdown.ConvertString(html)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(md), nil
}
