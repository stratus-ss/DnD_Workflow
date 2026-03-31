package wikijs

import (
	"context"
	"os"
	"testing"

	"dnd-workflow/internal/config"
)

func TestPublishRealNotes(t *testing.T) {
	token := os.Getenv("WIKIJS_TOKEN")
	if token == "" {
		t.Skip("set WIKIJS_TOKEN to run")
	}

	data, err := os.ReadFile("../../example_files/example_summary.txt")
	if err != nil {
		t.Fatalf("read example: %v", err)
	}

	cfg := config.WikiJSConfig{
		URL:      "http://wiki.example.com",
		Locale:   "en",
		Editor:   "markdown",
		BasePath: "D&D/YourCampaign/Session_Notes",
	}
	client := NewClient(cfg, token)

	title := "Session Notes - 2026-01-11"
	path := "D&D/YourCampaign/Session_Notes/2026/test-2026-01-11"
	tags := []string{"dnd", "session-notes", "your-campaign", "test"}

	pageID, err := client.CreatePage(context.Background(), title, path, string(data), tags)
	if err != nil {
		t.Fatalf("CreatePage: %v", err)
	}

	t.Logf("Published page ID: %d at %s (%d bytes)", pageID, path, len(data))
}
