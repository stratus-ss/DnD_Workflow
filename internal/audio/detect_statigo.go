//go:build statigo

package audio

import (
	"fmt"
	"strconv"
	"strings"

	ffmpeg "github.com/linuxmatters/ffmpeg-statigo"
)

type audioInput struct {
	fmtCtx   *ffmpeg.AVFormatContext
	decCtx   *ffmpeg.AVCodecContext
	streamIdx int
}

func openAudioDecoder(audioPath string) (*audioInput, error) {
	var fmtCtx *ffmpeg.AVFormatContext

	path := ffmpeg.ToCStr(audioPath)
	defer path.Free()

	if _, err := ffmpeg.AVFormatOpenInput(&fmtCtx, path, nil, nil); err != nil {
		return nil, fmt.Errorf("open input: %w", err)
	}

	if _, err := ffmpeg.AVFormatFindStreamInfo(fmtCtx, nil); err != nil {
		ffmpeg.AVFormatCloseInput(&fmtCtx)
		return nil, fmt.Errorf("find stream info: %w", err)
	}

	idx := findAudioStream(fmtCtx)
	if idx < 0 {
		ffmpeg.AVFormatCloseInput(&fmtCtx)
		return nil, fmt.Errorf("no audio stream found")
	}

	decCtx, err := openStreamDecoder(fmtCtx, idx)
	if err != nil {
		ffmpeg.AVFormatCloseInput(&fmtCtx)
		return nil, err
	}

	return &audioInput{fmtCtx: fmtCtx, decCtx: decCtx, streamIdx: idx}, nil
}

func (a *audioInput) close() {
	if a.decCtx != nil {
		ffmpeg.AVCodecFreeContext(&a.decCtx)
	}
	if a.fmtCtx != nil {
		ffmpeg.AVFormatCloseInput(&a.fmtCtx)
	}
}

func findAudioStream(fmtCtx *ffmpeg.AVFormatContext) int {
	streams := fmtCtx.Streams()
	for i := uintptr(0); i < uintptr(fmtCtx.NbStreams()); i++ {
		if streams.Get(i).Codecpar().CodecType() == ffmpeg.AVMediaTypeAudio {
			return int(i)
		}
	}
	return -1
}

func openStreamDecoder(fmtCtx *ffmpeg.AVFormatContext, idx int) (*ffmpeg.AVCodecContext, error) {
	stream := fmtCtx.Streams().Get(uintptr(idx))
	codecpar := stream.Codecpar()

	decoder := ffmpeg.AVCodecFindDecoder(codecpar.CodecId())
	if decoder == nil {
		return nil, fmt.Errorf("decoder not found for codec %d", codecpar.CodecId())
	}

	decCtx := ffmpeg.AVCodecAllocContext3(decoder)
	if decCtx == nil {
		return nil, fmt.Errorf("alloc decoder context")
	}

	if _, err := ffmpeg.AVCodecParametersToContext(decCtx, codecpar); err != nil {
		ffmpeg.AVCodecFreeContext(&decCtx)
		return nil, fmt.Errorf("codec params to context: %w", err)
	}

	if _, err := ffmpeg.AVCodecOpen2(decCtx, decoder, nil); err != nil {
		ffmpeg.AVCodecFreeContext(&decCtx)
		return nil, fmt.Errorf("open decoder: %w", err)
	}

	return decCtx, nil
}

type silenceFilter struct {
	graph   *ffmpeg.AVFilterGraph
	bufSrc  *ffmpeg.AVFilterContext
	bufSink *ffmpeg.AVFilterContext
}

func buildSilenceFilterGraph(decCtx *ffmpeg.AVCodecContext, threshDB int, minDurMs int) (*silenceFilter, error) {
	graph := ffmpeg.AVFilterGraphAlloc()
	if graph == nil {
		return nil, fmt.Errorf("alloc filter graph")
	}

	bufSrc, err := createBufferSource(graph, decCtx)
	if err != nil {
		ffmpeg.AVFilterGraphFree(&graph)
		return nil, err
	}

	bufSink, err := createBufferSink(graph)
	if err != nil {
		ffmpeg.AVFilterGraphFree(&graph)
		return nil, err
	}

	detect, err := createSilenceDetect(graph, threshDB, minDurMs)
	if err != nil {
		ffmpeg.AVFilterGraphFree(&graph)
		return nil, err
	}

	if err := linkFilters(bufSrc, detect, bufSink); err != nil {
		ffmpeg.AVFilterGraphFree(&graph)
		return nil, err
	}

	if _, err := ffmpeg.AVFilterGraphConfig(graph, nil); err != nil {
		ffmpeg.AVFilterGraphFree(&graph)
		return nil, fmt.Errorf("config filter graph: %w", err)
	}

	return &silenceFilter{graph: graph, bufSrc: bufSrc, bufSink: bufSink}, nil
}

func (f *silenceFilter) close() {
	if f.graph != nil {
		ffmpeg.AVFilterGraphFree(&f.graph)
	}
}

func createBufferSource(graph *ffmpeg.AVFilterGraph, decCtx *ffmpeg.AVCodecContext) (*ffmpeg.AVFilterContext, error) {
	src := ffmpeg.AVFilterGetByName(ffmpeg.ToCStr("abuffer"))
	args := ffmpeg.ToCStr(fmt.Sprintf(
		"time_base=1/%d:sample_rate=%d:sample_fmt=%s:channel_layout=0x%x",
		decCtx.SampleRate(), decCtx.SampleRate(),
		ffmpeg.AVGetSampleFmtName(decCtx.SampleFmt()).GoString(),
		decCtx.ChannelLayout(),
	))
	defer args.Free()

	name := ffmpeg.ToCStr("in")
	defer name.Free()

	var ctx *ffmpeg.AVFilterContext
	if _, err := ffmpeg.AVFilterGraphCreateFilter(&ctx, src, name, args, nil, graph); err != nil {
		return nil, fmt.Errorf("create buffer source: %w", err)
	}
	return ctx, nil
}

func createBufferSink(graph *ffmpeg.AVFilterGraph) (*ffmpeg.AVFilterContext, error) {
	sink := ffmpeg.AVFilterGetByName(ffmpeg.ToCStr("abuffersink"))
	name := ffmpeg.ToCStr("out")
	defer name.Free()

	var ctx *ffmpeg.AVFilterContext
	if _, err := ffmpeg.AVFilterGraphCreateFilter(&ctx, sink, name, nil, nil, graph); err != nil {
		return nil, fmt.Errorf("create buffer sink: %w", err)
	}
	return ctx, nil
}

func createSilenceDetect(graph *ffmpeg.AVFilterGraph, threshDB, minDurMs int) (*ffmpeg.AVFilterContext, error) {
	detect := ffmpeg.AVFilterGetByName(ffmpeg.ToCStr("silencedetect"))
	args := ffmpeg.ToCStr(fmt.Sprintf("noise=%ddB:d=%f", threshDB, float64(minDurMs)/1000.0))
	defer args.Free()

	name := ffmpeg.ToCStr("silence")
	defer name.Free()

	var ctx *ffmpeg.AVFilterContext
	if _, err := ffmpeg.AVFilterGraphCreateFilter(&ctx, detect, name, args, nil, graph); err != nil {
		return nil, fmt.Errorf("create silencedetect: %w", err)
	}
	return ctx, nil
}

func linkFilters(src, mid, sink *ffmpeg.AVFilterContext) error {
	if _, err := ffmpeg.AVFilterLink(src, 0, mid, 0); err != nil {
		return fmt.Errorf("link src→detect: %w", err)
	}
	if _, err := ffmpeg.AVFilterLink(mid, 0, sink, 0); err != nil {
		return fmt.Errorf("link detect→sink: %w", err)
	}
	return nil
}

func drainSilences(input *audioInput, sf *silenceFilter) ([]SilenceRange, error) {
	pkt := ffmpeg.AVPacketAlloc()
	defer ffmpeg.AVPacketFree(&pkt)

	frame := ffmpeg.AVFrameAlloc()
	defer ffmpeg.AVFrameFree(&frame)

	filtFrame := ffmpeg.AVFrameAlloc()
	defer ffmpeg.AVFrameFree(&filtFrame)

	var silences []SilenceRange

	for {
		if _, err := ffmpeg.AVReadFrame(input.fmtCtx, pkt); err != nil {
			break
		}
		if pkt.StreamIndex() != int32(input.streamIdx) {
			ffmpeg.AVPacketUnref(pkt)
			continue
		}

		decoded := decodeAndFilter(input.decCtx, pkt, frame, sf)
		silences = append(silences, collectSilenceMetadata(sf, filtFrame)...)
		silences = append(silences, decoded...)
		ffmpeg.AVPacketUnref(pkt)
	}

	flushDecoder(input.decCtx, frame, sf)
	silences = append(silences, collectSilenceMetadata(sf, filtFrame)...)

	return silences, nil
}

func decodeAndFilter(decCtx *ffmpeg.AVCodecContext, pkt *ffmpeg.AVPacket, frame *ffmpeg.AVFrame, sf *silenceFilter) []SilenceRange {
	ffmpeg.AVCodecSendPacket(decCtx, pkt)
	var silences []SilenceRange

	for {
		if _, err := ffmpeg.AVCodecReceiveFrame(decCtx, frame); err != nil {
			break
		}
		ffmpeg.AVBuffersrcAddFrameFlags(sf.bufSrc, frame, 0)
	}
	return silences
}

func flushDecoder(decCtx *ffmpeg.AVCodecContext, frame *ffmpeg.AVFrame, sf *silenceFilter) {
	ffmpeg.AVCodecSendPacket(decCtx, nil)
	for {
		if _, err := ffmpeg.AVCodecReceiveFrame(decCtx, frame); err != nil {
			break
		}
		ffmpeg.AVBuffersrcAddFrameFlags(sf.bufSrc, frame, 0)
	}
	ffmpeg.AVBuffersrcAddFrameFlags(sf.bufSrc, nil, 0)
}

func collectSilenceMetadata(sf *silenceFilter, frame *ffmpeg.AVFrame) []SilenceRange {
	var ranges []SilenceRange
	silenceKey := ffmpeg.ToCStr("lavfi.silence_start")
	endKey := ffmpeg.ToCStr("lavfi.silence_end")
	defer silenceKey.Free()
	defer endKey.Free()

	for {
		if _, err := ffmpeg.AVBuffersinkGetFrame(sf.bufSink, frame); err != nil {
			break
		}

		meta := frame.Metadata()
		if meta != nil {
			if r, ok := extractSilenceRange(meta, silenceKey, endKey); ok {
				ranges = append(ranges, r)
			}
		}
		ffmpeg.AVFrameUnref(frame)
	}
	return ranges
}

func extractSilenceRange(meta *ffmpeg.AVDictionary, startKey, endKey ffmpeg.CStr) (SilenceRange, bool) {
	startEntry := ffmpeg.AVDictGet(meta, startKey, nil, 0)
	endEntry := ffmpeg.AVDictGet(meta, endKey, nil, 0)

	if startEntry == nil || endEntry == nil {
		return SilenceRange{}, false
	}

	s, err1 := strconv.ParseFloat(strings.TrimSpace(startEntry.Value().GoString()), 64)
	e, err2 := strconv.ParseFloat(strings.TrimSpace(endEntry.Value().GoString()), 64)
	if err1 != nil || err2 != nil {
		return SilenceRange{}, false
	}

	return SilenceRange{StartSec: s, EndSec: e}, true
}
