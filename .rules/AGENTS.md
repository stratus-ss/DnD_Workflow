# D&D Session Workflow — Agent Guidelines

## Project Overview

- **Language**: Go 1.26
- **Module**: `dnd-workflow`
- **Package manager**: `go mod` (`go.mod` / `go.sum`)
- **Build**: `make build` (standard) or `make build-statigo` (static FFmpeg)

## Key Packages

| Package | Purpose |
|---------|---------|
| `cmd/dnd-workflow/` | CLI entry point (Cobra) |
| `internal/pipeline/` | Orchestration and step dispatch |
| `internal/whisper/` | Whisper-WebUI HTTP client |
| `internal/perplexity/` | Chrome automation via chromedp |
| `internal/tts/` | ebook2audiobook Gradio client |
| `internal/audio/` | FFmpeg silence detection and pause injection |
| `internal/wikijs/` | GraphQL client for Wiki.js |
| `internal/distribute/` | File distribution to configured dirs |
| `internal/progress/` | Real-time progress and benchmark tracking |

## Configuration

1. Copy `config.yaml.example` to `config.yaml` and fill in service URLs and tokens
2. Copy `.env.example` to `.env` and set `WIKIJS_TOKEN`
3. Service URLs, API keys, and selectors are NOT hardcoded — all in `config.yaml`

## Common Commands

```bash
go build -o dnd-workflow ./cmd/dnd-workflow   # build
go test ./...                                  # run all tests
make build-statigo                             # self-contained binary
```

## Pipeline

`--step` values: `whisper`, `perplexity`, `perplexity-upload`, `perplexity-scrape`, `tts`, `audio`, `wiki`, `distribute`
Use `--continue` to run from a step through end of pipeline.
Use `--force` to re-run a step even if output exists.

## Don't

- Never commit `config.yaml`, `.env`, or `output/`
- Never add new external dependencies without checking `go.mod`
- Never hardcode URLs, API keys, or CSS selectors in Go files