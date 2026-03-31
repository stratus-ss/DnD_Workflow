package tts

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"dnd-workflow/internal/config"
	"dnd-workflow/internal/progress"
)

type Client struct {
	cfg        config.TTSConfig
	httpClient *http.Client
	fnMap      map[string]int
}

func NewClient(cfg config.TTSConfig) *Client {
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: cfg.TLSSkipVerify},
	}
	return &Client{
		cfg:        cfg,
		httpClient: &http.Client{Timeout: time.Duration(cfg.HTTPTimeoutMin) * time.Minute, Transport: transport},
	}
}

func (c *Client) resolveFnIndex(apiName string) (int, error) {
	if c.fnMap == nil {
		if err := c.discoverFnIndices(); err != nil {
			return 0, fmt.Errorf("discover endpoints: %w", err)
		}
	}
	name := strings.TrimPrefix(apiName, "/")
	idx, ok := c.fnMap[name]
	if !ok {
		return 0, fmt.Errorf("api endpoint %q not found on server", apiName)
	}
	return idx, nil
}

func (c *Client) discoverFnIndices() error {
	configURL := fmt.Sprintf("%s/config", c.cfg.URL)
	resp, err := c.httpClient.Get(configURL)
	if err != nil {
		return fmt.Errorf("fetch config: %w", err)
	}
	defer resp.Body.Close()

	var cfg struct {
		Dependencies []struct {
			APIName string `json:"api_name"`
		} `json:"dependencies"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&cfg); err != nil {
		return fmt.Errorf("decode config: %w", err)
	}

	c.fnMap = make(map[string]int, len(cfg.Dependencies))
	for i, dep := range cfg.Dependencies {
		if dep.APIName != "" {
			c.fnMap[dep.APIName] = i
		}
	}
	slog.Info("discovered Gradio endpoints", "count", len(c.fnMap))
	return nil
}

func (c *Client) ConvertTextToAudio(ctx context.Context, text, outputPath string) error {
	sessionHash := fmt.Sprintf("dnd_%d", time.Now().UnixNano())

	sessionID, err := c.initSession(ctx, sessionHash)
	if err != nil {
		return fmt.Errorf("init session: %w", err)
	}
	slog.Info("session created", "session_id", sessionID)

	voicePath, fineTuned, err := c.restoreAndResolve(ctx, sessionHash, sessionID)
	if err != nil {
		return fmt.Errorf("restore: %w", err)
	}

	tmpFile, err := writeTempText(text)
	if err != nil {
		return fmt.Errorf("temp file: %w", err)
	}
	defer os.Remove(tmpFile)

	uploadedPath, err := c.uploadFile(ctx, tmpFile)
	if err != nil {
		return fmt.Errorf("upload: %w", err)
	}
	slog.Info("file uploaded", "path", uploadedPath)

	if err := c.setEbookFile(ctx, sessionHash, sessionID, uploadedPath); err != nil {
		return fmt.Errorf("set ebook: %w", err)
	}

	if err := c.submitConversion(ctx, sessionHash, sessionID, uploadedPath, voicePath, fineTuned); err != nil {
		return fmt.Errorf("convert: %w", err)
	}

	audioPath, err := c.getAudiobookPath(ctx, sessionHash, sessionID)
	if err != nil {
		return fmt.Errorf("get audiobook: %w", err)
	}

	audioURL, err := c.getAudiobookURL(ctx, sessionHash, sessionID, audioPath)
	if err != nil {
		return fmt.Errorf("get URL: %w", err)
	}

	return c.downloadFile(ctx, audioURL, outputPath)
}

func (c *Client) initSession(ctx context.Context, sessionHash string) (string, error) {
	api := c.cfg.GradioAPINames
	emptyData := make(map[string]interface{})
	initialState := map[string]interface{}{"hash": nil}
	result, err := c.queueCall(ctx, sessionHash, api.CreateSession, []interface{}{emptyData, initialState}, 30*time.Second)
	if err != nil {
		return "", err
	}

	data, ok := result["data"].([]interface{})
	if !ok || len(data) < 3 {
		return "", fmt.Errorf("unexpected create session output: %d elements", len(data))
	}

	for i := len(data) - 1; i >= 0; i-- {
		m, ok := data[i].(map[string]interface{})
		if !ok {
			continue
		}
		if sid, ok := m["value"].(string); ok && sid != "" {
			return sid, nil
		}
	}

	return "", fmt.Errorf("no session ID in create session response")
}

func (c *Client) restoreAndResolve(ctx context.Context, sessionHash, sessionID string) (string, string, error) {
	api := c.cfg.GradioAPINames
	result, err := c.queueCall(ctx, sessionHash, api.RestoreUI, []interface{}{sessionID}, 30*time.Second)
	if err != nil {
		return "", "", err
	}

	data, _ := result["data"].([]interface{})

	voicePath := resolveVoice(data, c.cfg.Voice)
	fineTuned := resolveFineTuned(data, c.cfg.FineTuned)
	slog.Info("resolved voice/model", "voice", voicePath, "fine_tuned", fineTuned)

	return voicePath, fineTuned, nil
}

func resolveVoice(outputs []interface{}, wanted string) string {
	for _, item := range outputs {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		choices, ok := m["choices"].([]interface{})
		if !ok || len(choices) == 0 {
			continue
		}
		for _, ch := range choices {
			pair, ok := ch.([]interface{})
			if !ok || len(pair) < 2 {
				continue
			}
			label, _ := pair[0].(string)
			value, _ := pair[1].(string)
			if strings.Contains(value, "/voices/") && label == wanted {
				return value
			}
		}
	}
	return wanted
}

func resolveFineTuned(outputs []interface{}, wanted string) string {
	for _, item := range outputs {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		choices, ok := m["choices"].([]interface{})
		if !ok || len(choices) == 0 {
			continue
		}
		hasVoicePaths := false
		for _, ch := range choices {
			pair, ok := ch.([]interface{})
			if ok && len(pair) >= 2 {
				val, _ := pair[1].(string)
				if strings.Contains(val, "/voices/") {
					hasVoicePaths = true
					break
				}
			}
		}
		if hasVoicePaths {
			continue
		}
		for _, ch := range choices {
			pair, ok := ch.([]interface{})
			if !ok || len(pair) < 2 {
				continue
			}
			label, _ := pair[0].(string)
			if label == wanted {
				value, _ := pair[1].(string)
				return value
			}
		}
	}
	if wanted == "" {
		return "internal"
	}
	return wanted
}

func (c *Client) setEbookFile(ctx context.Context, sessionHash, sessionID, uploadedPath string) error {
	api := c.cfg.GradioAPINames
	fileData := gradioFileData(uploadedPath)
	_, err := c.queueCall(ctx, sessionHash, api.SetEbook, []interface{}{fileData, sessionID}, 30*time.Second)
	return err
}

func (c *Client) submitConversion(ctx context.Context, sessionHash, sessionID, uploadedPath, voicePath, fineTuned string) error {
	api := c.cfg.GradioAPINames

	fileData := gradioFileData(uploadedPath)

	data := []interface{}{
		sessionID,             // [0]  session_id
		c.cfg.Device,          // [1]  device
		fileData,              // [2]  ebook_file
		false,                 // [3]  blocks_preview
		c.cfg.TTSEngine,       // [4]  tts_engine
		c.cfg.Language,        // [5]  language
		voicePath,             // [6]  voice
		nil,                   // [7]  custom_model
		fineTuned,             // [8]  fine_tuned
		c.cfg.OutputFormat,    // [9]  output_format
		c.cfg.OutputChannel,   // [10] output_channel
		c.cfg.Temperature,     // [11] xtts_temperature
		c.cfg.LengthPenalty,   // [12] xtts_length_penalty
		c.cfg.NumBeams,        // [13] xtts_num_beams
		c.cfg.RepetitionPenalty, // [14] xtts_repetition_penalty
		50,                    // [15] xtts_top_k
		0.95,                  // [16] xtts_top_p
		c.cfg.Speed,           // [17] xtts_speed
		c.cfg.TextSplitting,   // [18] xtts_enable_text_splitting
		0.22,                  // [19] bark_text_temp
		0.44,                  // [20] bark_waveform_temp
		false,                 // [21] output_split
		"6",                   // [22] output_split_hours
	}

	slog.Info("conversion submitted, waiting for completion")
	convertTimeout := time.Duration(c.cfg.ConvertTimeoutMin) * time.Minute
	_, err := c.queueCall(ctx, sessionHash, api.SubmitConvert, data, convertTimeout)
	if err != nil {
		return err
	}

	slog.Info("conversion complete")
	return nil
}

func (c *Client) getAudiobookPath(ctx context.Context, sessionHash, sessionID string) (string, error) {
	api := c.cfg.GradioAPINames
	result, err := c.queueCall(ctx, sessionHash, api.RefreshUI, []interface{}{sessionID}, 30*time.Second)
	if err != nil {
		return "", err
	}

	data, _ := result["data"].([]interface{})
	for _, item := range data {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		if choices, ok := m["choices"].([]interface{}); ok {
			for _, ch := range choices {
				pair, ok := ch.([]interface{})
				if !ok || len(pair) < 2 {
					continue
				}
				value, _ := pair[1].(string)
				if strings.Contains(value, "/audiobooks/") {
					return value, nil
				}
			}
		}
		if val, ok := m["value"].(string); ok && strings.Contains(val, "/audiobooks/") {
			return val, nil
		}
	}

	for i, item := range data {
		raw, _ := json.Marshal(item)
		slog.Debug("refresh element", "index", i, "data", string(raw))
	}
	return "", fmt.Errorf("no audiobook found in refresh output (got %d elements)", len(data))
}

func (c *Client) getAudiobookURL(ctx context.Context, sessionHash, sessionID, audioPath string) (string, error) {
	api := c.cfg.GradioAPINames
	result, err := c.queueCall(ctx, sessionHash, api.AudiobookPlayer, []interface{}{sessionID}, 30*time.Second)
	if err != nil {
		return "", err
	}

	data, _ := result["data"].([]interface{})
	for _, item := range data {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		if val, ok := m["value"].(map[string]interface{}); ok {
			if u, ok := val["url"].(string); ok && u != "" {
				return u, nil
			}
		}
		if u, ok := m["url"].(string); ok && u != "" {
			return u, nil
		}
	}

	return "", fmt.Errorf("no audio URL in response")
}

type queueEvent struct {
	Msg     string                 `json:"msg"`
	Output  map[string]interface{} `json:"output"`
	Success bool                   `json:"success"`
	Log     string                 `json:"log"`
	Level   string                 `json:"level"`
}

func (c *Client) queueCall(ctx context.Context, sessionHash string, apiName string, data []interface{}, timeout time.Duration) (map[string]interface{}, error) {
	fnIndex, err := c.resolveFnIndex(apiName)
	if err != nil {
		return nil, err
	}

	joinURL := fmt.Sprintf("%s%s/queue/join", c.cfg.URL, c.cfg.GradioAPIPrefix)
	reqBody, err := json.Marshal(map[string]interface{}{
		"data":         data,
		"fn_index":     fnIndex,
		"session_hash": sessionHash,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal join request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, joinURL, bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("create join request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("join: %w", err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()

	return c.pollQueue(ctx, sessionHash, timeout)
}

func (c *Client) pollQueue(ctx context.Context, sessionHash string, timeout time.Duration) (map[string]interface{}, error) {
	dataURL := fmt.Sprintf("%s%s/queue/data?session_hash=%s", c.cfg.URL, c.cfg.GradioAPIPrefix, sessionHash)

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("cancelled: %w", ctx.Err())
		default:
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, dataURL, nil)
		if err != nil {
			return nil, fmt.Errorf("create poll request: %w", err)
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			if rep := progress.FromContext(ctx); rep != nil {
				rep.Heartbeat()
			}
			time.Sleep(2 * time.Second)
			continue
		}

		event, err := readSSEUntilComplete(resp.Body, deadline)
		resp.Body.Close()
		if err != nil {
			slog.Debug("SSE read error, retrying", "error", err)
			if rep := progress.FromContext(ctx); rep != nil {
				rep.Heartbeat()
			}
			time.Sleep(2 * time.Second)
			continue
		}

		if !event.Success {
			errMsg, _ := event.Output["error"].(string)
			if errMsg == "" {
				errMsg = "unknown error"
			}
			return nil, fmt.Errorf("server error: %s", errMsg)
		}

		return event.Output, nil
	}

	return nil, fmt.Errorf("timeout after %v", timeout)
}

func readSSEUntilComplete(body io.Reader, deadline time.Time) (*queueEvent, error) {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	for scanner.Scan() {
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("deadline exceeded")
		}

		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		payload := line[6:]
		var event queueEvent
		if err := json.Unmarshal([]byte(payload), &event); err != nil {
			continue
		}

		switch event.Msg {
		case "process_completed":
			return &event, nil
		case "log":
			if event.Log != "" {
				slog.Info("e2a", "level", event.Level, "msg", event.Log)
			}
		}
	}

	return nil, fmt.Errorf("stream ended without completion")
}

func (c *Client) uploadFile(ctx context.Context, localPath string) (string, error) {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	file, err := os.Open(localPath)
	if err != nil {
		return "", fmt.Errorf("open: %w", err)
	}
	defer file.Close()

	part, err := writer.CreateFormFile("files", filepath.Base(localPath))
	if err != nil {
		return "", fmt.Errorf("form: %w", err)
	}
	if _, err := io.Copy(part, file); err != nil {
		return "", fmt.Errorf("copy: %w", err)
	}
	writer.Close()

	uploadURL := fmt.Sprintf("%s%s/upload?upload_id=dnd_%d", c.cfg.URL, c.cfg.GradioAPIPrefix, time.Now().UnixNano())

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, uploadURL, &buf)
	if err != nil {
		return "", fmt.Errorf("create upload request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("post: %w", err)
	}
	defer resp.Body.Close()

	var paths []string
	if err := json.NewDecoder(resp.Body).Decode(&paths); err != nil {
		return "", fmt.Errorf("decode: %w", err)
	}
	if len(paths) == 0 {
		return "", fmt.Errorf("no paths returned")
	}

	return paths[0], nil
}

func (c *Client) downloadFile(ctx context.Context, rawURL, outputPath string) error {
	if !strings.HasPrefix(rawURL, "http") {
		rawURL = c.cfg.URL + rawURL
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return fmt.Errorf("create download request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()

	out, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("create: %w", err)
	}
	defer out.Close()

	n, err := io.Copy(out, resp.Body)
	if err != nil {
		return fmt.Errorf("write: %w", err)
	}

	slog.Info("downloaded audio", "path", outputPath, "bytes", n)
	return nil
}

func gradioFileData(path string) map[string]interface{} {
	return map[string]interface{}{
		"path":      path,
		"orig_name": filepath.Base(path),
		"meta":      map[string]string{"_type": "gradio.FileData"},
	}
}

func writeTempText(text string) (string, error) {
	f, err := os.CreateTemp("", "dnd-summary-*.txt")
	if err != nil {
		return "", err
	}
	if _, err := f.WriteString(text); err != nil {
		f.Close()
		return "", err
	}
	f.Close()
	return f.Name(), nil
}
