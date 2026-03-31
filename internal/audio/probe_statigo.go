//go:build statigo

package audio

import (
	"context"
	"fmt"

	ffmpeg "github.com/linuxmatters/ffmpeg-statigo"
)

// ProbeDuration returns the duration in seconds of an audio file using
// ffmpeg-statigo's avformat bindings. The ffprobePath arg is ignored.
func ProbeDuration(_ context.Context, _, audioPath string) (float64, error) {
	var ctx *ffmpeg.AVFormatContext

	path := ffmpeg.ToCStr(audioPath)
	defer path.Free()

	if _, err := ffmpeg.AVFormatOpenInput(&ctx, path, nil, nil); err != nil {
		return 0, fmt.Errorf("avformat open: %w", err)
	}
	defer ffmpeg.AVFormatCloseInput(&ctx)

	if _, err := ffmpeg.AVFormatFindStreamInfo(ctx, nil); err != nil {
		return 0, fmt.Errorf("avformat find stream info: %w", err)
	}

	if d := formatDuration(ctx); d > 0 {
		return d, nil
	}

	return streamDuration(ctx)
}

func formatDuration(ctx *ffmpeg.AVFormatContext) float64 {
	dur := ctx.Duration()
	if dur <= 0 {
		return 0
	}
	return float64(dur) / float64(ffmpeg.AVTimeBase)
}

func streamDuration(ctx *ffmpeg.AVFormatContext) (float64, error) {
	streams := ctx.Streams()
	nb := uintptr(ctx.NbStreams())

	for i := uintptr(0); i < nb; i++ {
		s := streams.Get(i)
		if s.Duration() > 0 {
			tb := s.TimeBase()
			return float64(s.Duration()) * float64(tb.Num()) / float64(tb.Den()), nil
		}
	}

	return 0, fmt.Errorf("no duration found in any stream")
}
