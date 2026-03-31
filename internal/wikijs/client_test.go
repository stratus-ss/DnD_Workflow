package wikijs

import (
	"context"
	"os"
	"testing"

	"dnd-workflow/internal/config"
)

func TestCreatePage(t *testing.T) {
	token := os.Getenv("WIKIJS_TOKEN")
	if token == "" {
		t.Skip("set WIKIJS_TOKEN to run")
	}

	cfg := config.WikiJSConfig{
		URL:      "http://wiki.example.com",
		Locale:   "en",
		Editor:   "markdown",
		BasePath: "D&D/YourCampaign/Session_Notes",
	}
	client := NewClient(cfg, token)

	content := "# Test Page\n\nThis is an automated test page from dnd-workflow.\n\nIt can be safely deleted."
	title := "Integration Test"
	path := "D&D/YourCampaign/Session_Notes/test-page"
	tags := []string{"test", "automated"}

	pageID, err := client.CreatePage(context.Background(), title, path, content, tags)
	if err != nil {
		t.Fatalf("CreatePage: %v", err)
	}

	t.Logf("Created page ID: %d at path: %s", pageID, path)
}
