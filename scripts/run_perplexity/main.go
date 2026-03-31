package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"dnd-workflow/internal/config"
	"dnd-workflow/internal/perplexity"
)

func main() {
	srtPath, _ := filepath.Abs("example_files/Mar_15_2026.srt.txt")
	if _, err := os.Stat(srtPath); err != nil {
		fmt.Printf("SRT file not found: %v\n", err)
		os.Exit(1)
	}

	promptText, err := perplexity.LoadPrompt("prompts/session_notes.txt", "Mar_15_2026")
	if err != nil {
		fmt.Printf("LoadPrompt: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Prompt (%d chars)\n", len(promptText))

	profileDir := os.ExpandEnv("$HOME/.config/dnd-workflow/chrome-profile")
	cfg := config.PerplexityConfig{
		SpaceURL:      "https://www.perplexity.ai/spaces/YOUR_SPACE_ID",
		ChromeProfile: profileDir,
	}

	browser := perplexity.NewBrowser(cfg)
	if err := browser.Start(); err != nil {
		fmt.Printf("Start: %v\n", err)
		os.Exit(1)
	}
	defer browser.Close()

	threadName := ""
	start := time.Now()
	fullMD, narration, err := browser.GenerateNotesInThread(srtPath, promptText, threadName)
	if err != nil {
		browser.TakeScreenshot("/tmp/pplx_error.png")
		fmt.Printf("GenerateNotes failed: %v\n", err)
		fmt.Println("Screenshot: /tmp/pplx_error.png")
		os.Exit(1)
	}
	fmt.Printf("Done in %v\n", time.Since(start).Round(time.Second))

	outDir := "/tmp/dnd_perplexity_test"
	os.MkdirAll(outDir, 0o755)

	mdPath := filepath.Join(outDir, "session_notes.md")
	os.WriteFile(mdPath, []byte(fullMD), 0o644)
	fmt.Printf("Full markdown: %s (%d chars)\n", mdPath, len(fullMD))

	if narration != "" {
		narPath := filepath.Join(outDir, "narration.md")
		os.WriteFile(narPath, []byte(narration), 0o644)
		fmt.Printf("Narration: %d words -> %s\n", len(strings.Fields(narration)), narPath)
	} else {
		fmt.Println("WARN: ParseSummary returned empty")
	}

	fmt.Printf("\n--- Preview ---\n%.500s\n...\n", fullMD)
}
