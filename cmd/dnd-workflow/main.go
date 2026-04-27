package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"dnd-workflow/internal/audio"
	"dnd-workflow/internal/config"
	"dnd-workflow/internal/distribute"
	"dnd-workflow/internal/perplexity"
	"dnd-workflow/internal/pipeline"
	"dnd-workflow/internal/tts"
	"dnd-workflow/internal/whisper"
	"dnd-workflow/internal/wikijs"

	"github.com/spf13/cobra"
)

var cfgPath string
var logLevel string

func main() {
	rootCmd := buildRootCmd()
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func buildRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:          "dnd-workflow",
		Short:        "D&D session post-processing pipeline",
		Long:         "Automates transcription, note generation, TTS, audio processing, and wiki publishing for D&D sessions.",
		SilenceUsage: true,
	}

	root.PersistentFlags().StringVarP(&cfgPath, "config", "c", "config.yaml", "path to config file")
	root.AddCommand(buildRunCmd())

	return root
}

func newRunner(cfg *config.Config, force bool) *pipeline.Runner {
	whisperClient := whisper.NewClient(cfg.Whisper)
	browser := perplexity.NewBrowser(cfg.Perplexity)
	ttsClient := tts.NewClient(cfg.TTS)
	speaker := &ttsAdapter{client: ttsClient}
	proc := audio.NewProcessor(cfg.Audio)

	token := config.WikiJSToken()
	wikiClient := wikijs.NewClient(cfg.WikiJS, token)
	dist := distribute.New(cfg.Distribute)

	r := pipeline.NewRunner(cfg, whisperClient, browser, speaker, proc, wikiClient, dist)
	r.SetForce(force)
	return r
}

type ttsAdapter struct {
	client *tts.Client
}

func (a *ttsAdapter) ConvertTextToAudio(ctx context.Context, text, outputPath string, _ interface{}) error {
	return a.client.ConvertTextToAudio(ctx, text, outputPath)
}

func buildRunCmd() *cobra.Command {
	var audioPath, date, step string
	var force, continueSteps bool

	cmd := &cobra.Command{
		Use:   "run",
		Short: "Run the pipeline (single step or from --step with --continue)",
		Long: fmt.Sprintf(
			"Run the D&D post-processing pipeline.\n\nValid --step values: all, %s\n\n"+
				"By default --step runs only the specified step. Add --continue to run\n"+
				"from that step through the end of the pipeline.",
			strings.Join(pipeline.StepOrder, ", "),
		),
		RunE: func(cmd *cobra.Command, args []string) error {
			if date == "" {
				date = time.Now().Format("2006-01-02")
			}

			if !pipeline.ValidStep(step) {
				return fmt.Errorf("invalid --step %q; valid values: all, %s",
					step, strings.Join(pipeline.StepOrder, ", "))
			}

			needsAudio := step == "all" || step == "whisper"
			if needsAudio && audioPath == "" {
				return fmt.Errorf("--audio is required when --step is %q", step)
			}

			cfg, err := config.Load(cfgPath)
			if err != nil {
				return err
			}

			var level slog.Level
			if err := level.UnmarshalText([]byte(logLevel)); err != nil {
				return fmt.Errorf("invalid log level %q: %w", logLevel, err)
			}
			slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})))

			return newRunner(cfg, force).RunFrom(cmd.Context(), audioPath, date, step, continueSteps)
		},
	}

	cmd.Flags().StringVar(&audioPath, "audio", "", "path to session audio file (required for whisper step)")
	cmd.Flags().StringVar(&date, "date", "", "session date YYYY-MM-DD (default: today)")
	cmd.Flags().StringVar(&step, "step", "all", "step to run: all, "+strings.Join(pipeline.StepOrder, ", "))
	cmd.Flags().BoolVar(&continueSteps, "continue", false, "continue through remaining steps after --step")
	cmd.Flags().BoolVar(&force, "force", false, "re-run steps even if output files exist")
	cmd.Flags().StringVar(&logLevel, "log-level", "info", "log level (debug, info, warn, error)")

	return cmd
}
