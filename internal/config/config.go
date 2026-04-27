package config

import (
	"bufio"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type WhisperConfig struct {
	URL             string `yaml:"url"`
	Model           string `yaml:"model"`
	Language        string `yaml:"language"`
	VAD             bool   `yaml:"vad"`
	Diarize         bool   `yaml:"diarize"`
	BeamSize        int    `yaml:"beam_size"`
	PollIntervalSec int    `yaml:"poll_interval_sec"`
	TLSSkipVerify   bool   `yaml:"tls_skip_verify"`

	// Tier 2/3: operational tuning
	PollTimeoutMin    int    `yaml:"poll_timeout_min"`
	UploadTimeoutMin  int    `yaml:"upload_timeout_min"`
	MaxPollErrors     int    `yaml:"max_poll_errors"`
	ComputeType       string `yaml:"compute_type"`
	BestOf            int    `yaml:"best_of"`
	BatchSize         int    `yaml:"batch_size"`
	NoSpeechThreshold string `yaml:"no_speech_threshold"`
	LogProbThreshold  string `yaml:"log_prob_threshold"`
	Temperature       string `yaml:"temperature"`
	WordTimestamps    *bool  `yaml:"word_timestamps"`
	ConditionOnPrev   *bool  `yaml:"condition_on_previous_text"`
}

type PerplexitySelectors struct {
	TextInput    string `yaml:"text_input"`
	FileInput    string `yaml:"file_input"`
	SubmitButton string `yaml:"submit_button"`
	AttachButton string `yaml:"attach_button"`
	ResponseArea string `yaml:"response_area"`
	CopyButton   string `yaml:"copy_button"`
	CloseButton  string `yaml:"close_button"`
}

type PerplexityConfig struct {
	SpaceURL         string `yaml:"space_url"`
	ChromeProfile    string `yaml:"chrome_profile"`
	PromptFile       string `yaml:"prompt_file"`
	ThreadName       string `yaml:"thread_name"`
	Headless         bool   `yaml:"headless"`
	SessionRecapsDir string `yaml:"session_recaps_dir"`

	// Tier 2: timing and response detection
	ResponseTimeoutMin      int    `yaml:"response_timeout_min"`
	ResponsePollIntervalSec int    `yaml:"response_poll_interval_sec"`
	ResponseStableCount     int    `yaml:"response_stable_count"`
	WindowSize              string `yaml:"window_size"`
	PostNavigateSleepSec    int    `yaml:"post_navigate_sleep_sec"`
	AfterNewThreadSleepSec  int    `yaml:"after_new_thread_sleep_sec"`
	AfterUploadSleepSec     int    `yaml:"after_upload_sleep_sec"`

	// Tier 3: DOM selectors (override if Perplexity changes their UI)
	Selectors PerplexitySelectors `yaml:"selectors"`
}

type GradioAPINames struct {
	CreateSession   string `yaml:"create_session"`
	RestoreUI       string `yaml:"restore_ui"`
	SetEbook        string `yaml:"set_ebook"`
	SubmitConvert   string `yaml:"submit_convert"`
	RefreshUI       string `yaml:"refresh_ui"`
	AudiobookPlayer string `yaml:"audiobook_player"`
}

type TTSConfig struct {
	URL           string  `yaml:"url"`
	Device        string  `yaml:"device"`
	Language      string  `yaml:"language"`
	OutputFormat  string  `yaml:"output_format"`
	TTSEngine     string  `yaml:"tts_engine"`
	Voice         string  `yaml:"voice"`
	CustomModel   string  `yaml:"custom_model"`
	FineTuned     string  `yaml:"fine_tuned"`
	Speed         float64 `yaml:"speed"`
	Temperature   float64 `yaml:"temperature"`
	TextSplitting bool    `yaml:"text_splitting"`
	OutputChannel string  `yaml:"output_channel"`
	TLSSkipVerify bool    `yaml:"tls_skip_verify"`

	// Tier 2: timeouts and API paths
	ConvertTimeoutMin int    `yaml:"convert_timeout_min"`
	HTTPTimeoutMin    int    `yaml:"http_timeout_min"`
	GradioAPIPrefix   string `yaml:"gradio_api_prefix"`

	// Tier 2: decode hyperparameters
	RepetitionPenalty float64 `yaml:"repetition_penalty"`
	NumBeams          int     `yaml:"num_beams"`
	LengthPenalty     float64 `yaml:"length_penalty"`

	// Tier 1: Gradio API endpoint names (change when e2a updates)
	GradioAPINames GradioAPINames `yaml:"gradio_api_names"`
}

type AudioConfig struct {
	MinPauseMs      int `yaml:"min_pause_ms"`
	SilenceThreshDB int `yaml:"silence_thresh_db"`
	MinSilenceLenMs int `yaml:"min_silence_len_ms"`

	// Tier 2: encoding and post-processing
	OutputBitrate     string  `yaml:"output_bitrate"`
	OutputFormat      string  `yaml:"output_format"`
	PadSampleRate     int     `yaml:"pad_sample_rate"`
	PadChannelLayout  string  `yaml:"pad_channel_layout"`
	TargetLoudness    float64 `yaml:"target_loudness"`
	ParagraphPauseSec float64 `yaml:"paragraph_pause_sec"`
	SectionPauseSec   float64 `yaml:"section_pause_sec"`
	SavePauseText     bool    `yaml:"save_pause_text"`
	FFmpegPath        string  `yaml:"ffmpeg_path"`
	FFprobePath       string  `yaml:"ffprobe_path"`
}

type WikiJSConfig struct {
	URL               string   `yaml:"url"`
	BasePath          string   `yaml:"base_path"`
	Locale            string   `yaml:"locale"`
	Editor            string   `yaml:"editor"`
	IsPublished       *bool    `yaml:"is_published"`
	Tags              []string `yaml:"tags"`
	PageTitleTemplate string   `yaml:"page_title_template"`
}

type DistributeConfig struct {
	TranscriptDir     string `yaml:"transcript_dir"`
	AudioDir          string `yaml:"audio_dir"`
	AudioCompletedDir string `yaml:"audio_completed_dir"`
	OriginalAudioDir  string `yaml:"original_audio_dir"`
}

type BenchmarksConfig struct {
	WhisperRate   float64 `yaml:"whisper_rate"`
	TTSRate       float64 `yaml:"tts_rate"`
	AudioRate     float64 `yaml:"audio_rate"`
	HistoryWindow int     `yaml:"history_window"`
}

type Config struct {
	Whisper    WhisperConfig    `yaml:"whisper"`
	Perplexity PerplexityConfig `yaml:"perplexity"`
	TTS        TTSConfig        `yaml:"tts"`
	Audio      AudioConfig      `yaml:"audio"`
	WikiJS     WikiJSConfig     `yaml:"wikijs"`
	Distribute DistributeConfig `yaml:"distribute"`
	Benchmarks BenchmarksConfig `yaml:"benchmarks"`
	OutputDir  string           `yaml:"output_dir"`
}

func Load(path string) (*Config, error) {
	loadDotEnv(filepath.Dir(path))

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	cfg := &Config{}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	cfg.applyDefaults()
	cfg.expandPaths()
	cfg.applyEnvOverrides()

	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("config validation: %w", err)
	}

	return cfg, nil
}

func (c *Config) applyDefaults() {
	w := &c.Whisper
	if w.PollIntervalSec == 0 {
		w.PollIntervalSec = 5
	}
	if w.BeamSize == 0 {
		w.BeamSize = 5
	}
	if w.PollTimeoutMin == 0 {
		w.PollTimeoutMin = 60
	}
	if w.UploadTimeoutMin == 0 {
		w.UploadTimeoutMin = 30
	}
	if w.MaxPollErrors == 0 {
		w.MaxPollErrors = 30
	}
	if w.ComputeType == "" {
		w.ComputeType = "float16"
	}
	if w.BestOf == 0 {
		w.BestOf = 5
	}
	if w.BatchSize == 0 {
		w.BatchSize = 24
	}
	if w.NoSpeechThreshold == "" {
		w.NoSpeechThreshold = "0.6"
	}
	if w.LogProbThreshold == "" {
		w.LogProbThreshold = "-1.0"
	}
	if w.Temperature == "" {
		w.Temperature = "0"
	}
	if w.WordTimestamps == nil {
		f := false
		w.WordTimestamps = &f
	}
	if w.ConditionOnPrev == nil {
		t := true
		w.ConditionOnPrev = &t
	}

	p := &c.Perplexity
	if p.ResponseTimeoutMin == 0 {
		p.ResponseTimeoutMin = 10
	}
	if p.ResponsePollIntervalSec == 0 {
		p.ResponsePollIntervalSec = 5
	}
	if p.ResponseStableCount == 0 {
		p.ResponseStableCount = 4
	}
	if p.WindowSize == "" {
		p.WindowSize = "1280,900"
	}
	if p.PostNavigateSleepSec == 0 {
		p.PostNavigateSleepSec = 5
	}
	if p.AfterNewThreadSleepSec == 0 {
		p.AfterNewThreadSleepSec = 2
	}
	if p.AfterUploadSleepSec == 0 {
		p.AfterUploadSleepSec = 3
	}
	applyDefaultSelectors(&p.Selectors)

	tt := &c.TTS
	if tt.ConvertTimeoutMin == 0 {
		tt.ConvertTimeoutMin = 60
	}
	if tt.HTTPTimeoutMin == 0 {
		tt.HTTPTimeoutMin = 60
	}
	if tt.GradioAPIPrefix == "" {
		tt.GradioAPIPrefix = "/gradio_api"
	}
	if tt.RepetitionPenalty == 0 {
		tt.RepetitionPenalty = 1.0
	}
	if tt.NumBeams == 0 {
		tt.NumBeams = 1
	}
	if tt.LengthPenalty == 0 {
		tt.LengthPenalty = 1.0
	}
	if tt.OutputChannel == "" {
		tt.OutputChannel = "mono"
	}
	applyDefaultAPINames(&tt.GradioAPINames)

	a := &c.Audio
	if a.MinPauseMs == 0 {
		a.MinPauseMs = 600
	}
	if a.SilenceThreshDB == 0 {
		a.SilenceThreshDB = -40
	}
	if a.MinSilenceLenMs == 0 {
		a.MinSilenceLenMs = 150
	}
	if a.OutputBitrate == "" {
		a.OutputBitrate = "170k"
	}
	if a.OutputFormat == "" {
		a.OutputFormat = "mp3"
	}
	if a.PadSampleRate == 0 {
		a.PadSampleRate = 44100
	}
	if a.PadChannelLayout == "" {
		a.PadChannelLayout = "mono"
	}
	if a.TargetLoudness == 0 {
		a.TargetLoudness = -16
	}
	if a.ParagraphPauseSec == 0 {
		a.ParagraphPauseSec = 1.5
	}
	if a.SectionPauseSec == 0 {
		a.SectionPauseSec = 3.0
	}
	if a.FFmpegPath == "" {
		a.FFmpegPath = "ffmpeg"
	}
	if a.FFprobePath == "" {
		a.FFprobePath = "ffprobe"
	}

	wk := &c.WikiJS
	if wk.Locale == "" {
		wk.Locale = "en"
	}
	if wk.Editor == "" {
		wk.Editor = "markdown"
	}
	if wk.IsPublished == nil {
		t := true
		wk.IsPublished = &t
	}
	if wk.Tags == nil {
		wk.Tags = []string{"dnd", "session-notes", "your-campaign"}
	}
	if wk.PageTitleTemplate == "" {
		wk.PageTitleTemplate = "Session Notes - {date}"
	}

	if c.OutputDir == "" {
		c.OutputDir = "./output"
	}

	b := &c.Benchmarks
	if b.WhisperRate == 0 {
		b.WhisperRate = 0.17
	}
	if b.TTSRate == 0 {
		b.TTSRate = 0.04
	}
	if b.AudioRate == 0 {
		b.AudioRate = 0.02
	}
	if b.HistoryWindow == 0 {
		b.HistoryWindow = 5
	}
}

func applyDefaultSelectors(s *PerplexitySelectors) {
	if s.TextInput == "" {
		s.TextInput = `div[role="textbox"]`
	}
	if s.FileInput == "" {
		s.FileInput = `input[type="file"][multiple]`
	}
	if s.SubmitButton == "" {
		s.SubmitButton = `button[aria-label="Submit"]`
	}
	if s.AttachButton == "" {
		s.AttachButton = `button[aria-label="Add files or tools"]`
	}
	if s.ResponseArea == "" {
		s.ResponseArea = `div.prose, div[class*="markdown"], .break-words`
	}
	if s.CopyButton == "" {
		s.CopyButton = `button[aria-label="Copy"]`
	}
	if s.CloseButton == "" {
		s.CloseButton = `button[aria-label="Close"]`
	}
}

func applyDefaultAPINames(api *GradioAPINames) {
	if api.CreateSession == "" {
		api.CreateSession = "/change_gr_restore_session"
	}
	if api.RestoreUI == "" {
		api.RestoreUI = "/restore_interface"
	}
	if api.SetEbook == "" {
		api.SetEbook = "/change_gr_ebook_file"
	}
	if api.SubmitConvert == "" {
		api.SubmitConvert = "/start_conversion"
	}
	if api.RefreshUI == "" {
		api.RefreshUI = "/refresh_interface"
	}
	if api.AudiobookPlayer == "" {
		api.AudiobookPlayer = "/update_gr_audiobook_player"
	}
}

func (c *Config) expandPaths() {
	c.Perplexity.ChromeProfile = expandHome(c.Perplexity.ChromeProfile)
	c.Perplexity.PromptFile = expandHome(c.Perplexity.PromptFile)
	c.Perplexity.SessionRecapsDir = expandHome(c.Perplexity.SessionRecapsDir)
	c.OutputDir = expandHome(c.OutputDir)
	c.Distribute.TranscriptDir = expandHome(c.Distribute.TranscriptDir)
	c.Distribute.AudioDir = expandHome(c.Distribute.AudioDir)
	c.Distribute.AudioCompletedDir = expandHome(c.Distribute.AudioCompletedDir)
	c.Distribute.OriginalAudioDir = expandHome(c.Distribute.OriginalAudioDir)
}

func (c *Config) applyEnvOverrides() {
	if v := os.Getenv("DND_OUTPUT_DIR"); v != "" {
		c.OutputDir = v
	}
	if v := os.Getenv("DND_DISTRIBUTE_TRANSCRIPT_DIR"); v != "" {
		c.Distribute.TranscriptDir = v
	}
	if v := os.Getenv("DND_DISTRIBUTE_AUDIO_DIR"); v != "" {
		c.Distribute.AudioDir = v
	}
	if v := os.Getenv("DND_DISTRIBUTE_AUDIO_COMPLETED_DIR"); v != "" {
		c.Distribute.AudioCompletedDir = v
	}
	if v := os.Getenv("DND_DISTRIBUTE_ORIGINAL_AUDIO_DIR"); v != "" {
		c.Distribute.OriginalAudioDir = v
	}
	if v := os.Getenv("DND_PERPLEXITY_SESSION_RECAPS_DIR"); v != "" {
		c.Perplexity.SessionRecapsDir = v
	}
}

func (c *Config) validate() error {
	if c.Whisper.URL == "" {
		return fmt.Errorf("whisper.url is required")
	}
	if c.TTS.URL == "" {
		return fmt.Errorf("tts.url is required")
	}
	if c.WikiJS.URL == "" {
		return fmt.Errorf("wikijs.url is required")
	}
	if c.Perplexity.PromptFile == "" {
		return fmt.Errorf("perplexity.prompt_file is required")
	}
	if c.Audio.SilenceThreshDB > 0 {
		return fmt.Errorf("audio.silence_thresh_db must be negative, got %d", c.Audio.SilenceThreshDB)
	}
	if float64(c.TTS.NumBeams) < c.TTS.LengthPenalty {
		return fmt.Errorf("tts.num_beams (%d) must be >= tts.length_penalty (%.1f)", c.TTS.NumBeams, c.TTS.LengthPenalty)
	}
	return nil
}

func WikiJSToken() string {
	return os.Getenv("WIKIJS_TOKEN")
}

func PerplexitySessionToken() string {
	return os.Getenv("PERPLEXITY_SESSION_TOKEN")
}

// loadDotEnv reads a .env file from dir and sets any variables not already
// present in the environment. Supports KEY=VALUE and export KEY=VALUE syntax.
// Silently skips if the file doesn't exist.
func loadDotEnv(dir string) {
	envPath := filepath.Join(dir, ".env")
	f, err := os.Open(envPath)
	if err != nil {
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		v = strings.Trim(v, `"'`)
		if os.Getenv(k) == "" {
			os.Setenv(k, v)
			slog.Debug("loaded env from .env", "key", k)
		}
	}
}

func expandHome(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		return filepath.Join(home, path[2:])
	}
	return path
}

func (c *Config) SessionOutputDir(date string) string {
	return filepath.Join(c.OutputDir, date)
}

func (c *Config) EnsureSessionDir(date string) (string, error) {
	dir := c.SessionOutputDir(date)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create session dir: %w", err)
	}
	return dir, nil
}

// WikiPageTitle generates the wiki page title using the configured template.
func (c *Config) WikiPageTitle(date string) string {
	return strings.Replace(c.WikiJS.PageTitleTemplate, "{date}", date, 1)
}
