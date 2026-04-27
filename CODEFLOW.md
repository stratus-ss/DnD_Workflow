# D&D Workflow Code Flow Documentation

This document provides Mermaid diagrams to visualize the code flow and decision paths in the D&D Workflow project.

## Table of Contents

1. [Main Pipeline Overview](#main-pipeline-overview)
2. [Whisper Transcription Flow](#whisper-transcription-flow)
3. [Perplexity Notes Generation Flow](#perplexity-notes-generation-flow)
4. [Text-to-Speech Flow](#text-to-speech-flow)
5. [Audio Processing Flow](#audio-processing-flow)
6. [Wiki.js Publishing Flow](#wikijs-publishing-flow)
7. [File Distribution Flow](#file-distribution-flow)
8. [Error Handling and Checkpointing](#error-handling-and-checkpointing)

## Main Pipeline Overview

```mermaid
graph TD
    A[Start] --> B[Validate Date]
    B --> C[Ensure Session Directory]
    C --> D[Initialize Progress Reporter]
    D --> E{Start Step}
    
    E -->|whisper| F[Transcribe Audio]
    E -->|perplexity| G[Generate Notes]
    E -->|perplexity-upload| H[Upload to Perplexity]
    E -->|perplexity-scrape| I[Scrape Response]
    E -->|tts| J[Convert Text to Speech]
    E -->|audio| K[Process Audio]
    E -->|wiki| L[Publish to Wiki]
    E -->|distribute| M[Distribute Files]
    
    F --> N["Checkpoint: transcript_*.srt.txt"]
    G --> O["Checkpoint: notes_*.md + narration_*.md"]
    H --> P[Browser remains open]
    I --> O
    J --> Q["Checkpoint: narration_raw_*.mp3"]
    K --> R["Checkpoint: narration_final_*.mp3"]
    L --> S["Checkpoint: Wiki.js page"]
    M --> T["Checkpoint: Distributed files"]
    
    N --> U[Continue to next step?]
    O --> U
    P --> U
    Q --> U
    R --> U
    S --> U
    T --> U
    
    U -->|Yes| E
    U -->|No| V[Pipeline Complete]
```

## Whisper Transcription Flow

```mermaid
graph TD
    A[runTranscribe] --> B{Force flag?}
    B -->|No| C[Check if transcript exists]
    C -->|Exists| D[Skip step]
    C -->|Not exists| E[Probe audio duration]
    B -->|Yes| E
    
    E --> F[Start progress reporter]
    F --> G[Call Transcribe API]
    G --> H{Success?}
    H -->|Yes| I[Save transcript]
    H -->|No| J[Return error]
    
    I --> K[Move original audio if configured]
    K --> L[Complete step]
    J --> M[Fail step]
```

## Perplexity Notes Generation Flow

```mermaid
graph TD
    A[runNotes] --> B{Force flag?}
    B -->|No| C[Check if notes exist]
    C -->|Exist| D[Skip step]
    C -->|Not exist| E[Load prompt template]
    B -->|Yes| E
    
    E --> F[Start browser]
    F --> G[GenerateNotesInThread]
    G --> H{Success?}
    H -->|Yes| I[Write notes and narration]
    H -->|No| J[Close browser and return error]
    
    I --> K[Write to session directory]
    K --> L[Complete step]
    J --> M[Fail step]
```

## Perplexity Upload and Scrape Flow

```mermaid
graph TD
    subgraph Upload[Upload]
        A[runPerplexityUpload] --> B[Load prompt]
        B --> C[Start browser]
        C --> D[UploadAndSubmit]
        D --> E{Success?}
        E -->|Yes| F[Complete upload step]
        E -->|No| G[Fail step]
    end
    
    subgraph Scrape[Scrape]
        H[runPerplexityScrape] --> I[Start browser]
        I --> J[ScrapeExistingResponse]
        J --> K{Success?}
        K -->|Yes| L[Write notes and narration]
        K -->|No| M[Fail step]
        L --> N[Complete scrape step]
    end
```

## Text-to-Speech Flow

```mermaid
graph TD
    A[runTTS] --> B{Force flag?}
    B -->|No| C[Check if TTS output exists]
    C -->|Exists| D[Skip step]
    C -->|Not exists| E[Insert pause tags]
    B -->|Yes| E
    
    E --> F[Save pause text if configured]
    F --> G[Probe text length]
    G --> H[Start progress reporter]
    H --> I[Call ConvertTextToAudio]
    I --> J{Success?}
    J -->|Yes| K[Complete step]
    J -->|No| L[Fail step]
```

## Audio Processing Flow

```mermaid
graph TD
    A[runAudioFix] --> B{Force flag?}
    B -->|No| C[Check if audio output exists]
    C -->|Exists| D[Skip step]
    C -->|Not exists| E[Probe input duration]
    B -->|Yes| E
    
    E --> F[Start progress reporter]
    F --> G[Process audio]
    G --> H{Success?}
    H -->|Yes| I[Complete step]
    H -->|No| J[Fail step]
```

## Wiki.js Publishing Flow

```mermaid
graph TD
    A[RunPublish] --> B[Validate date]
    B --> C[Start progress reporter]
    C --> D[Check WIKIJS_TOKEN]
    D -->|Missing| E[Fail step]
    D -->|Present| F[Build page path and title]
    F --> G[Check if page exists]
    G -->|Exists| H[Skip step]
    G -->|Not exists| I[Create page]
    I --> J{Success?}
    J -->|Yes| K[Complete step]
    J -->|No| L[Fail step]
```

## File Distribution Flow

```mermaid
graph TD
    A[runDistribute] --> B[Start progress reporter]
    B --> C[Check transcript exists]
    C -->|No| D[Log warning]
    C -->|Yes| E[Continue]
    E --> F[Check audio exists]
    F -->|No| G[Log warning]
    F -->|Yes| H[Continue]
    H --> I{Both missing?}
    I -->|Yes| J[Skip distribution]
    I -->|No| K[Distribute files]
    K --> L{Success?}
    L -->|Yes| M[Complete step]
    L -->|No| N[Fail step]
```

## Error Handling and Checkpointing

```mermaid
graph TD
    A[Step starts] --> B[Check if output exists]
    B -->|Exists| C[Skip step]
    B -->|Not exists| D[Execute step]
    D --> E{Success?}
    E -->|Yes| F[Write output file]
    E -->|No| G[Log error]
    F --> H[Update progress reporter]
    G --> I[Update progress reporter with failure]
    H --> J[Continue to next step]
    I --> K[Exit with error]
    
    subgraph Progress Reporter
        L[StartStep] --> M[Update status]
        M --> N[Estimate duration]
        N --> O[CompleteStep/SkipStep/FailStep]
        O --> P[Write .progress.json]
    end
```

## Decision Points and Configuration

```mermaid
graph TD
    A[Configuration] --> B[Force flag]
    A --> C[Continue flag]
    A --> D[Step selection]
    A --> E[Audio format]
    A --> F[Prompt template]
    A --> G[Directory paths]
    
    B --> H[Bypass checkpointing]
    C --> I[Run multiple steps]
    D --> J[Select starting step]
    E --> K[Affects output format]
    F --> L[Used in Perplexity step]
    G --> M[Affects file locations]
```

## Data Flow Between Steps

```mermaid
graph LR
    subgraph Inputs[Inputs]
        A[Audio file] --> B[Whisper]
    end
    
    subgraph Step Outputs
        B -->|transcript_*.srt.txt| C[Perplexity]
        C -->|notes_*.md| G[Wiki]
        C -->|narration_*.md| D[TTS]
        D -->|narration_raw_*.mp3| E[Audio Processing]
        E -->|narration_final_*.mp3| F[Distribution]
        B -->|transcript_*.srt.txt| F
    end
    
    subgraph Final Outputs
        G --> H[Wiki.js page]
        F --> I[Distributed transcript]
        F --> J[Distributed audio]
    end
```

## Key Components and Their Responsibilities

| Component | Responsibility | Key Methods |
|-----------|---------------|-------------|
| `Runner` | Orchestrates pipeline execution | `RunFrom`, `runTranscribe`, `runNotes`, etc. |
| `Transcriber` | Handles audio transcription | `Transcribe` |
| `NotesGenerator` | Generates notes from transcript | `GenerateNotesInThread`, `UploadAndSubmit`, `ScrapeExistingResponse` |
| `Speaker` | Converts text to speech | `ConvertTextToAudio` |
| `AudioFixer` | Processes audio files | `Process` |
| `Publisher` | Publishes to Wiki.js | `CreatePage`, `CheckPageExists` |
| `FileDistributor` | Distributes final files | `Distribute`, `MoveOriginalAudio` |
| `Reporter` | Tracks progress and benchmarks | `StartStep`, `CompleteStep`, `EstimateStepDuration` |

## Checkpointing Strategy

The pipeline uses file-based checkpointing to avoid re-running completed steps:

1. Each step checks for the existence of its output file(s)
2. If files exist and `--force` is not set, the step is skipped
3. Checkpoint files are stored in the session directory: `output/<date>/`
4. The `--force` flag bypasses checkpointing for testing/recovery

## Error Handling Flow

```mermaid
graph TD
    A[Error occurs] --> B[Log error with slog]
    B --> C[Update progress reporter with failure]
    C --> D[Return error to caller]
    D --> E[Main function catches error]
    E --> F[Exit with status 1]
    
    subgraph Recovery[Recovery]
        G[User can use --force] --> H[Re-run failed step]
        H --> I[Continue pipeline]
    end
```