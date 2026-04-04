# D&D Session Workflow

A Go CLI that automates D&D session post-processing: transcribe audio, generate notes with AI, convert narration to speech, post-process audio, and publish to a wiki.

## Dependencies

| Service | Purpose | Link |
|---------|---------|------|
| Whisper-WebUI | REST API for audio-to-SRT transcription (faster-whisper, VAD, diarization) | [GitHub](https://github.com/stratus-ss/Whisper-WebUI-Swear-Removal) |
| ebook2audiobook | Gradio-based TTS server with XTTSv2 voice cloning (v26+, Gradio 5) | [GitHub](https://github.com/DrewThomasson/ebook2audiobook) |
| Perplexity AI | AI note generation via browser automation (requires Pro subscription) | [perplexity.ai](https://www.perplexity.ai) |
| Wiki.js | Self-hosted wiki for publishing session notes | [js.wiki](https://js.wiki) |
| ffmpeg / ffprobe | Audio silence detection and pause injection (default build only; not needed with `make build-statigo`) | [ffmpeg.org](https://ffmpeg.org) |
| ffmpeg-statigo | Static FFmpeg libraries for self-contained binary (optional; replaces ffmpeg/ffprobe runtime dependency) | [GitHub](https://github.com/linuxmatters/ffmpeg-statigo) |
| Google Chrome | Required for Perplexity browser automation via chromedp | |

## Pipeline

| Step | Name | `--step` flag | Typical time |
|------|------|---------------|--------------|
| 1 | Whisper Transcription | `whisper` | ~15 min |
| 2 | Perplexity Notes | `perplexity` | ~1-2 min |
| 3 | Text-to-Speech | `tts` | ~5 min |
| 4 | Audio Post-Processing | `audio` | ~10 sec |
| 5 | Wiki.js Publish | `wiki` | ~2 sec |
| 6 | File Distribution | `distribute` | ~1 sec |

Each step checkpoints its output. If the output file exists, the step is skipped (override with `--force`).

### Step 1: Whisper Transcription

Uploads the session audio to the Whisper REST API. Model, beam size, VAD, diarization, and all query params are configurable. Outputs an SRT file with timestamps.

- **Input**: Audio file (FLAC, M4A, WAV, etc.)
- **Output**: `output/<date>/transcript_<date>.srt.txt`

### Step 2: Perplexity Notes

Opens Chrome via `chromedp`, navigates to the configured Perplexity Space, uploads the SRT file, and submits the prompt. Extracts the AI response as markdown. Headless mode, selectors, and timing are all configurable.

- **Input**: SRT file + prompt template from `perplexity.prompt_file`
- **Output**: `output/<date>/notes_<date>.md` + `output/<date>/narration_<date>.md`
- **Prerequisite**: One-time Chrome login required (see Quick Start)

### Step 3: Text-to-Speech

Injects inline SML `[pause:N]` tags at paragraph/section boundaries (stripping markdown headings), then sends the narration to the ebook2audiobook Gradio server for voice synthesis. The client auto-discovers Gradio 5 function indices from the server's `/config` endpoint using `gradio_api_names` strings. Set `audio.save_pause_text: true` to write the pause-tagged text to disk for inspection.

- **Input**: Narration text from Step 2
- **Output**: `output/<date>/narration_raw_<date>.<format>`
- **Optional**: `output/<date>/narration_paused_<date>.txt` (when `save_pause_text` is enabled)

### Step 4: Audio Post-Processing

Detects silence gaps and injects pauses. Default build shells out to `ffmpeg`/`ffprobe`; the `statigo` build uses linked FFmpeg C libraries (avformat, avfilter, avcodec) with no external runtime dependencies. Bitrate, sample rate, channel layout, and silence thresholds are configurable.

- **Input**: Raw audio from Step 3
- **Output**: `output/<date>/narration_final_<date>.<format>`

### Step 5: Wiki.js Publish

Publishes session notes to Wiki.js via GraphQL. Checks for duplicate pages before creating. Title template, tags, locale, and editor are configurable.

- **Input**: Full notes markdown from Step 2
- **Output**: Wiki.js page at `<base_path>/<year>/<date>`
- **Dedup**: Skipped if page already exists at that path

### Step 6: File Distribution

Copies pipeline outputs to their final destinations with human-friendly date naming (`MMM_DD_YYYY`). Any existing `session_recap_*` file in the audio destination is moved to a `Completed/` subdirectory before the new one is placed. Skipped entirely if no destination paths are configured.

- **Input**: Transcript from Step 1 + final audio from Step 4
- **Output**: `<transcript_dir>/Apr_04_2026.srt.txt` + `<audio_dir>/session_recap_Apr_04_2026.mp3`
- **Config**: Paths set via `distribute.*` in `config.yaml` or `DND_DISTRIBUTE_*` env vars

## Quick Start

```bash
# Build
go build -o dnd-workflow ./cmd/dnd-workflow

# Configure
cp config.yaml.example config.yaml    # edit with your URLs
cp .env.example .env                   # set WIKIJS_TOKEN

# One-time: log in to Perplexity via Chrome
google-chrome-stable \
  --password-store=basic \
  --user-data-dir=~/.config/dnd-workflow/chrome-profile \
  https://www.perplexity.ai
# Log in, then close Chrome.

# Run full pipeline (--date defaults to today if omitted)
./dnd-workflow run --audio session.m4a --date 2026-03-15
./dnd-workflow run --audio session.m4a                     # uses today's date

# Run a single step (prior outputs must exist)
./dnd-workflow run --date 2026-03-15 --step perplexity
./dnd-workflow run --step wiki                             # uses today's date

# Run from a step through the rest of the pipeline
./dnd-workflow run --date 2026-03-15 --step perplexity --continue

# Force re-run a step even if output exists
./dnd-workflow run --date 2026-03-15 --step perplexity --force
```

`--audio` is only required when `--step` is `all` (default) or `whisper`.

By default `--step` runs only the specified step. Add `--continue` to run from that step through the end of the pipeline.

## Configuration

All settings live in `config.yaml`. See `config.yaml.example` for the full reference with documented defaults.

| File | Purpose |
|------|---------|
| `config.yaml.example` | Template with all options and known-working defaults |
| `.env.example` | Required environment variables |
| `prompts/session_notes.txt` | Prompt template sent to Perplexity (`{SESSION_DATE}` is substituted) |

## Output

```
output/
├── .benchmarks.json                    # adaptive benchmark history (auto-generated)
└── 2026-03-15/
    ├── transcript_2026-03-15.srt.txt
    ├── notes_2026-03-15.md
    ├── narration_2026-03-15.md
    ├── narration_paused_2026-03-15.txt # only when audio.save_pause_text: true
    ├── narration_raw_2026-03-15.mp3
    ├── narration_final_2026-03-15.mp3
    ├── .progress.json                  # real-time step progress (written during run)
    └── status.json                     # final pipeline result summary
```

## Progress Reporting and Benchmarking

The pipeline writes real-time progress to `.progress.json` during execution, enabling lightweight LLM-based monitoring without high token costs. Each step reports its status, health, elapsed time, and estimated remaining time.

**Health states**: `healthy`, `slow`, `stalled`, `completed`, `failed`, `skipped`

### Adaptive Benchmarking

Each step automatically records its input size and elapsed time to `output/.benchmarks.json`. After enough runs (controlled by `benchmarks.history_window`), the system averages historical rates to produce more accurate ETAs for future runs. Seed rates in `config.yaml` provide initial estimates before history is available.

| Step | Input Metric | Default Rate |
|------|-------------|--------------|
| whisper | seconds of audio | 0.17 sec/sec |
| tts | characters of text | 0.04 sec/char |
| audio | seconds of TTS audio | 0.02 sec/sec |

The perplexity step is excluded from benchmarking due to high variability.

## Build Targets

```bash
make build          # standard build (requires ffmpeg/ffprobe on PATH at runtime)
make build-statigo  # self-contained binary with static FFmpeg 8.0.1 (no runtime deps)
make test           # run all tests
make tidy           # go mod tidy
```

### Static FFmpeg Build (statigo)

The `statigo` build links FFmpeg C libraries directly into the binary, eliminating `ffmpeg` and `ffprobe` as runtime dependencies. It uses `avformat` for duration probing, `avfilter` with `silencedetect` for silence detection, and a decode-insert-encode pipeline for pause injection.

```bash
# One-time setup: clone submodule + download static libs (~100MB)
make setup-statigo

# Build
make build-statigo
```

Requires `CGO_ENABLED=1` and a C compiler (gcc/clang). The static libraries are platform-specific (Linux/macOS, amd64/arm64).

## Legal

This project is licensed under the [GNU Affero General Public License v3.0](LICENSE).

D&D Session Workflow is unofficial Fan Content permitted under the [Fan Content Policy](https://company.wizards.com/fancontentpolicy). Not approved/endorsed by Wizards. Portions of the materials used are property of Wizards of the Coast. &copy;Wizards of the Coast LLC.
