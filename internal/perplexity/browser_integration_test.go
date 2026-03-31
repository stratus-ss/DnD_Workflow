package perplexity

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dnd-workflow/internal/config"
)

func TestGenerateNotesIntegration(t *testing.T) {
	if os.Getenv("PERPLEXITY_INTEGRATION") == "" {
		t.Skip("set PERPLEXITY_INTEGRATION=1 to run")
	}

	srtPath := "../../example_files/Mar_15_2026.srt.txt"
	if _, err := os.Stat(srtPath); err != nil {
		t.Fatalf("SRT file not found: %v", err)
	}
	absSRT, _ := filepath.Abs(srtPath)

	promptText, err := LoadPrompt("../../prompts/session_notes.txt", "Mar_15_2026")
	if err != nil {
		t.Fatalf("LoadPrompt: %v", err)
	}
	t.Logf("Prompt (%d chars): %.80s...", len(promptText), promptText)

	profileDir := os.ExpandEnv("$HOME/.config/dnd-workflow/chrome-profile")
	cfg := config.PerplexityConfig{
		SpaceURL:      "https://www.perplexity.ai/spaces/YOUR_SPACE_ID",
		ChromeProfile: profileDir,
	}

	browser := NewBrowser(cfg)
	if err := browser.Start(); err != nil {
		t.Fatalf("Start browser: %v", err)
	}
	defer browser.Close()

	t.Log("Navigating to Perplexity space...")
	if err := browser.NavigateToSpace(); err != nil {
		t.Fatalf("NavigateToSpace: %v", err)
	}

	browser.TakeScreenshot("/tmp/perplexity_01_space.png")
	t.Log("Screenshot saved: /tmp/perplexity_01_space.png")

	t.Log("Starting new thread...")
	if err := browser.StartNewThread(); err != nil {
		t.Logf("WARN: StartNewThread: %v (may already be fresh)", err)
	}

	browser.TakeScreenshot("/tmp/perplexity_02_thread.png")

	t.Logf("Uploading SRT file: %s", absSRT)
	if err := browser.UploadFile(absSRT); err != nil {
		t.Fatalf("UploadFile: %v", err)
	}

	browser.TakeScreenshot("/tmp/perplexity_03_upload.png")
	t.Log("Screenshot saved: /tmp/perplexity_03_upload.png")

	t.Log("Submitting prompt...")
	if err := browser.SubmitPrompt(promptText); err != nil {
		t.Fatalf("SubmitPrompt: %v", err)
	}

	browser.TakeScreenshot("/tmp/perplexity_04_submitted.png")

	t.Log("Waiting for response (up to 10min)...")
	if err := browser.WaitForResponse(10 * 60e9); err != nil {
		browser.TakeScreenshot("/tmp/perplexity_05_timeout.png")
		t.Fatalf("WaitForResponse: %v", err)
	}

	browser.TakeScreenshot("/tmp/perplexity_05_complete.png")
	t.Log("Response complete, extracting markdown...")

	markdown, err := browser.ExtractMarkdown()
	if err != nil {
		t.Logf("Markdown extraction failed: %v, falling back to plain text", err)
		markdown, err = browser.ExtractPlainText()
		if err != nil {
			t.Fatalf("ExtractPlainText: %v", err)
		}
	}

	outDir := "/tmp/dnd_perplexity_test"
	os.MkdirAll(outDir, 0o755)

	mdPath := filepath.Join(outDir, "session_notes.md")
	if err := os.WriteFile(mdPath, []byte(markdown), 0o644); err != nil {
		t.Fatalf("write markdown: %v", err)
	}
	t.Logf("Full markdown saved: %s (%d chars)", mdPath, len(markdown))

	narration := ParseSummary(markdown)
	if narration != "" {
		narPath := filepath.Join(outDir, "narration.md")
		os.WriteFile(narPath, []byte(narration), 0o644)
		words := len(strings.Fields(narration))
		t.Logf("Narration extracted: %d words, saved to %s", words, narPath)
	} else {
		t.Log("WARN: ParseSummary returned empty - full text may need different parsing")
	}

	t.Logf("Full output preview:\n%.500s\n...", markdown)
}
