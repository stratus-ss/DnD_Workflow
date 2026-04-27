// Package whisper provides an HTTP client for the Whisper-WebUI transcription service.
package whisper

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"dnd-workflow/internal/config"
	"dnd-workflow/internal/progress"
)

type Client struct {
	cfg          config.WhisperConfig
	pollInterval time.Duration
	uploadClient *http.Client
	pollClient   *http.Client
}

type Segment struct {
	Start float64 `json:"start"`
	End   float64 `json:"end"`
	Text  string  `json:"text"`
}

type uploadResponse struct {
	Identifier string `json:"identifier"`
	Message    string `json:"message"`
}

type taskResponse struct {
	Status   string          `json:"status"`
	Progress float64         `json:"progress"`
	Result   json.RawMessage `json:"result"`
	Error    string          `json:"error"`
}

func NewClient(cfg config.WhisperConfig) *Client {
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: cfg.TLSSkipVerify},
	}
	return &Client{
		cfg:          cfg,
		pollInterval: time.Duration(cfg.PollIntervalSec) * time.Second,
		uploadClient: &http.Client{
			Timeout:   time.Duration(cfg.UploadTimeoutMin) * time.Minute,
			Transport: transport,
		},
		pollClient: &http.Client{
			Timeout:   30 * time.Second,
			Transport: transport,
		},
	}
}

func (c *Client) Transcribe(ctx context.Context, audioPath, outputPath string) error {
	taskID, err := c.upload(ctx, audioPath)
	if err != nil {
		return fmt.Errorf("upload: %w", err)
	}
	slog.Info("upload complete", "task_id", taskID)

	segments, err := c.pollTask(ctx, taskID)
	if err != nil {
		return fmt.Errorf("poll: %w", err)
	}

	if err := WriteSRT(segments, outputPath); err != nil {
		return fmt.Errorf("write SRT: %w", err)
	}

	return nil
}

func (c *Client) upload(ctx context.Context, audioPath string) (string, error) {
	body, contentType, err := buildFileBody(audioPath)
	if err != nil {
		return "", err
	}

	reqURL := c.buildTranscriptionURL()
	slog.Info("uploading audio", "file", filepath.Base(audioPath), "url", reqURL)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, body)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", contentType)

	resp, err := c.uploadClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("post: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("upload returned %d: %s", resp.StatusCode, string(respBody))
	}

	var result uploadResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}
	if result.Identifier == "" {
		return "", fmt.Errorf("no task identifier in response")
	}

	return result.Identifier, nil
}

func (c *Client) buildTranscriptionURL() string {
	q := url.Values{}
	q.Set("model_size", c.cfg.Model)
	if c.cfg.Language != "" {
		q.Set("lang", c.cfg.Language)
	}
	q.Set("is_translate", "false")
	q.Set("compute_type", c.cfg.ComputeType)
	q.Set("beam_size", strconv.Itoa(c.cfg.BeamSize))
	q.Set("best_of", strconv.Itoa(c.cfg.BestOf))
	q.Set("batch_size", strconv.Itoa(c.cfg.BatchSize))
	q.Set("vad_filter", strconv.FormatBool(c.cfg.VAD))
	q.Set("is_diarize", strconv.FormatBool(c.cfg.Diarize))
	q.Set("no_speech_threshold", c.cfg.NoSpeechThreshold)
	q.Set("log_prob_threshold", c.cfg.LogProbThreshold)
	q.Set("temperature", c.cfg.Temperature)
	q.Set("word_timestamps", strconv.FormatBool(*c.cfg.WordTimestamps))
	q.Set("condition_on_previous_text", strconv.FormatBool(*c.cfg.ConditionOnPrev))
	return c.cfg.URL + "/transcription/?" + q.Encode()
}

func buildFileBody(audioPath string) (*bytes.Buffer, string, error) {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	file, err := os.Open(audioPath)
	if err != nil {
		return nil, "", fmt.Errorf("open audio: %w", err)
	}
	defer file.Close()

	part, err := writer.CreateFormFile("file", filepath.Base(audioPath))
	if err != nil {
		return nil, "", fmt.Errorf("create form file: %w", err)
	}
	if _, err := io.Copy(part, file); err != nil {
		return nil, "", fmt.Errorf("copy file: %w", err)
	}
	writer.Close()

	return &buf, writer.FormDataContentType(), nil
}

func (c *Client) pollTask(ctx context.Context, taskID string) ([]Segment, error) {
	lastProgress := -1.0
	start := time.Now()
	taskURL := c.cfg.URL + "/task/" + taskID
	consecutiveErrors := 0

	for {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("cancelled: %w", ctx.Err())
		default:
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, taskURL, nil)
		if err != nil {
			return nil, fmt.Errorf("create poll request: %w", err)
		}

		resp, err := c.pollClient.Do(req)
		if err != nil {
			consecutiveErrors++
			slog.Warn("poll error", "error", err, "elapsed", time.Since(start).Truncate(time.Second), "consecutive_errors", consecutiveErrors)
			if consecutiveErrors >= c.cfg.MaxPollErrors {
				return nil, fmt.Errorf("poll abandoned after %d consecutive errors, last: %w", consecutiveErrors, err)
			}
			time.Sleep(c.pollInterval)
			continue
		}

		var task taskResponse
		if err := json.NewDecoder(resp.Body).Decode(&task); err != nil {
			resp.Body.Close()
			consecutiveErrors++
			slog.Warn("poll decode error", "error", err, "consecutive_errors", consecutiveErrors)
			if consecutiveErrors >= c.cfg.MaxPollErrors {
				return nil, fmt.Errorf("poll abandoned after %d decode errors, last: %w", consecutiveErrors, err)
			}
			time.Sleep(c.pollInterval)
			continue
		}
		resp.Body.Close()
		consecutiveErrors = 0

		segments, done, err := handleTaskStatus(task, lastProgress, start)
		if err != nil {
			return nil, err
		}
		if done {
			return segments, nil
		}

		if task.Progress != lastProgress {
			if rep := progress.FromContext(ctx); rep != nil {
				rep.UpdateProgress(task.Progress)
			}
			lastProgress = task.Progress
		}

		time.Sleep(c.pollInterval)
	}
}

func handleTaskStatus(task taskResponse, lastProgress float64, start time.Time) ([]Segment, bool, error) {
	elapsed := time.Since(start).Truncate(time.Second)

	switch task.Status {
	case "completed":
		slog.Info("transcription completed", "elapsed", elapsed)
		var segments []Segment
		if err := json.Unmarshal(task.Result, &segments); err != nil {
			return nil, true, fmt.Errorf("decode completed result: %w", err)
		}
		return segments, true, nil
	case "failed", "error":
		return nil, true, fmt.Errorf("task failed after %s: %s", elapsed, task.Error)
	default:
		if task.Progress != lastProgress {
			slog.Info("transcription progress", "status", task.Status, "progress_pct", task.Progress*100, "elapsed", elapsed)
		}
		return nil, false, nil
	}
}
