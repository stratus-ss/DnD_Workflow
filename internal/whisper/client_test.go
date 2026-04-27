// Package whisper tests the HTTP client behavior.
package whisper

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"dnd-workflow/internal/config"
)

func TestTranscribe_UploadSuccess(t *testing.T) {
	// Mock server that returns task ID on upload, then completed SRT on poll.
	mux := http.NewServeMux()
	var taskID string

	mux.HandleFunc("/v1/transcription/", func(w http.ResponseWriter, r *http.Request) {
		taskID = "task-abc123"
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"identifier": taskID, "message": "queued"})
	})

	mux.HandleFunc("/v1/task/task-abc123", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":   "completed",
			"progress": 1.0,
			"result": []struct {
				Start float64 `json:"start"`
				End   float64 `json:"end"`
				Text  string  `json:"text"`
			}{
				{Start: 0.0, End: 5.0, Text: "Hello world."},
			},
		})
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	yes := true
	cfg := config.WhisperConfig{
		URL:              srv.URL + "/v1",
		Model:            "base",
		UploadTimeoutMin: 5,
		PollIntervalSec:  1,
		MaxPollErrors:    3,
		WordTimestamps:   &yes,
		ConditionOnPrev:  &yes,
	}
	client := NewClient(cfg)

	tmpDir := t.TempDir()
	audioPath := tmpDir + "/test.m4a"
	if err := os.WriteFile(audioPath, []byte("fake audio data"), 0o644); err != nil {
		t.Fatalf("create temp audio: %v", err)
	}
	outputPath := tmpDir + "/output.srt.txt"

	err := client.Transcribe(context.Background(), audioPath, outputPath)
	if err != nil {
		t.Fatalf("Transcribe failed: %v", err)
	}

	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read output file: %v", err)
	}
	if !strings.Contains(string(data), "Hello world.") {
		t.Errorf("output missing expected text: %s", string(data))
	}
}

func TestTranscribe_UploadHTTPError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/transcription/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal server error"))
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	yes := true
	cfg := config.WhisperConfig{
		URL:              srv.URL + "/v1",
		UploadTimeoutMin: 5,
		WordTimestamps:   &yes,
		ConditionOnPrev:  &yes,
	}
	client := NewClient(cfg)

	tmpDir := t.TempDir()
	audioPath := tmpDir + "/test.m4a"
	if err := os.WriteFile(audioPath, []byte("fake audio data"), 0o644); err != nil {
		t.Fatalf("create temp audio: %v", err)
	}
	err := client.Transcribe(context.Background(), audioPath, tmpDir+"/out.srt.txt")
	if err == nil {
		t.Fatal("expected error on 500 response, got nil")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error should mention status code: %v", err)
	}
}

func TestTranscribe_PollTimeout(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/transcription/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"identifier": "task-abc123", "message": "queued"})
	})

	// Always return "pending".
	mux.HandleFunc("/v1/task/task-abc123", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":   "pending",
			"progress": 0.1,
			"result":   nil,
		})
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	yes := true
	cfg := config.WhisperConfig{
		URL:              srv.URL + "/v1",
		UploadTimeoutMin: 5,
		PollIntervalSec:  1,
		MaxPollErrors:    3,
		WordTimestamps:   &yes,
		ConditionOnPrev:  &yes,
	}
	client := NewClient(cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	tmpDir := t.TempDir()
	audioPath := tmpDir + "/test.m4a"
	if err := os.WriteFile(audioPath, []byte("fake audio data"), 0o644); err != nil {
		t.Fatalf("create temp audio: %v", err)
	}
	err := client.Transcribe(ctx, audioPath, tmpDir+"/out.srt.txt")
	if err == nil {
		t.Fatal("expected context deadline exceeded, got nil")
	}
	if !strings.Contains(err.Error(), "context") && !strings.Contains(err.Error(), "deadline") {
		t.Errorf("error should be about context: %v", err)
	}
}
