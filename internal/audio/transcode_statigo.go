//go:build statigo

package audio

import (
	"fmt"
	"log/slog"
	"unsafe"

	ffmpeg "github.com/linuxmatters/ffmpeg-statigo"
)

type transcoder struct {
	inFmt     *ffmpeg.AVFormatContext
	inDecCtx  *ffmpeg.AVCodecContext
	inStream  int
	outFmt    *ffmpeg.AVFormatContext
	outEncCtx *ffmpeg.AVCodecContext
	outStream *ffmpeg.AVStream
	outputPTS int64
	frameSize int
}

func newTranscoder(inputPath, outputPath string) (*transcoder, error) {
	input, err := openAudioDecoder(inputPath)
	if err != nil {
		return nil, fmt.Errorf("open input: %w", err)
	}

	tc := &transcoder{
		inFmt:    input.fmtCtx,
		inDecCtx: input.decCtx,
		inStream: input.streamIdx,
	}

	if err := tc.openOutput(outputPath); err != nil {
		input.close()
		return nil, err
	}

	return tc, nil
}

func (tc *transcoder) openOutput(outputPath string) error {
	outPath := ffmpeg.ToCStr(outputPath)
	defer outPath.Free()

	if _, err := ffmpeg.AVFormatAllocOutputContext2(&tc.outFmt, nil, nil, outPath); err != nil {
		return fmt.Errorf("alloc output context: %w", err)
	}

	if err := tc.setupEncoder(); err != nil {
		return err
	}

	if _, err := ffmpeg.AVIOOpen(&tc.outFmt.Pb, outPath, ffmpeg.AVIOFlagWrite); err != nil {
		return fmt.Errorf("open output file: %w", err)
	}

	if _, err := ffmpeg.AVFormatWriteHeader(tc.outFmt, nil); err != nil {
		return fmt.Errorf("write header: %w", err)
	}

	return nil
}

func (tc *transcoder) setupEncoder() error {
	inPar := tc.inFmt.Streams().Get(uintptr(tc.inStream)).Codecpar()
	encoder := ffmpeg.AVCodecFindEncoder(inPar.CodecId())
	if encoder == nil {
		return fmt.Errorf("encoder not found for codec %d", inPar.CodecId())
	}

	tc.outStream = ffmpeg.AVFormatNewStream(tc.outFmt, encoder)
	if tc.outStream == nil {
		return fmt.Errorf("new output stream")
	}

	tc.outEncCtx = ffmpeg.AVCodecAllocContext3(encoder)
	if tc.outEncCtx == nil {
		return fmt.Errorf("alloc encoder context")
	}

	configureEncoder(tc.outEncCtx, tc.inDecCtx)

	if _, err := ffmpeg.AVCodecOpen2(tc.outEncCtx, encoder, nil); err != nil {
		return fmt.Errorf("open encoder: %w", err)
	}

	ffmpeg.AVCodecParametersFromContext(tc.outStream.Codecpar(), tc.outEncCtx)
	tc.frameSize = int(tc.outEncCtx.FrameSize())
	if tc.frameSize <= 0 {
		tc.frameSize = 1024
	}

	return nil
}

func configureEncoder(enc, dec *ffmpeg.AVCodecContext) {
	enc.SetSampleRate(dec.SampleRate())
	enc.SetChannelLayout(dec.ChannelLayout())
	enc.SetChannels(dec.Channels())
	enc.SetSampleFmt(dec.SampleFmt())
	enc.SetBitRate(dec.BitRate())
	enc.SetTimeBase(ffmpeg.AVRational{Num: 1, Den: dec.SampleRate()})
}

// processSegments decodes the input linearly, inserting silence frames
// at gap midpoints. Each gap triggers a silence insertion of the
// appropriate duration before continuing to decode audio.
func (tc *transcoder) processSegments(gaps []SilenceRange, minPauseMs int) error {
	targetSec := float64(minPauseMs) / 1000.0
	midpoints, padDurs := computeInsertionPoints(gaps, targetSec)

	pkt := ffmpeg.AVPacketAlloc()
	defer ffmpeg.AVPacketFree(&pkt)
	frame := ffmpeg.AVFrameAlloc()
	defer ffmpeg.AVFrameFree(&frame)

	gapIdx := 0
	timeBase := tc.inputTimeBase()

	for {
		if _, err := ffmpeg.AVReadFrame(tc.inFmt, pkt); err != nil {
			break
		}
		if pkt.StreamIndex() != int32(tc.inStream) {
			ffmpeg.AVPacketUnref(pkt)
			continue
		}

		gapIdx = tc.decodeAndWrite(pkt, frame, midpoints, padDurs, gapIdx, timeBase)
		ffmpeg.AVPacketUnref(pkt)
	}

	tc.flushEncoder()
	return nil
}

func (tc *transcoder) decodeAndWrite(pkt *ffmpeg.AVPacket, frame *ffmpeg.AVFrame, midpoints []float64, padDurs []float64, gapIdx int, tb ffmpeg.AVRational) int {
	ffmpeg.AVCodecSendPacket(tc.inDecCtx, pkt)

	for {
		if _, err := ffmpeg.AVCodecReceiveFrame(tc.inDecCtx, frame); err != nil {
			break
		}

		frameSec := float64(frame.Pts()) * float64(tb.Num()) / float64(tb.Den())

		for gapIdx < len(midpoints) && frameSec >= midpoints[gapIdx] {
			tc.writeSilence(padDurs[gapIdx])
			gapIdx++
		}

		tc.writeDecodedFrame(frame)
	}
	return gapIdx
}

func (tc *transcoder) inputTimeBase() ffmpeg.AVRational {
	return tc.inFmt.Streams().Get(uintptr(tc.inStream)).TimeBase()
}

func (tc *transcoder) writeDecodedFrame(frame *ffmpeg.AVFrame) {
	frame.SetPts(tc.outputPTS)
	tc.outputPTS += int64(frame.NbSamples())
	tc.encodeFrame(frame)
}

func (tc *transcoder) writeSilence(durSec float64) {
	sampleRate := tc.outEncCtx.SampleRate()
	totalSamples := int(durSec * float64(sampleRate))

	slog.Debug("inserting silence", "dur_sec", durSec, "samples", totalSamples)

	for remaining := totalSamples; remaining > 0; {
		n := tc.frameSize
		if n > remaining {
			n = remaining
		}
		tc.writeSilentFrame(n)
		remaining -= n
	}
}

func (tc *transcoder) writeSilentFrame(nbSamples int) {
	frame := ffmpeg.AVFrameAlloc()
	defer ffmpeg.AVFrameFree(&frame)

	frame.SetFormat(int(tc.outEncCtx.SampleFmt()))
	frame.SetChannelLayout(tc.outEncCtx.ChannelLayout())
	frame.SetSampleRate(tc.outEncCtx.SampleRate())
	frame.SetNbSamples(int32(nbSamples))
	frame.SetPts(tc.outputPTS)

	ffmpeg.AVFrameGetBuffer(frame, 0)
	clearFrameData(frame, nbSamples, tc.outEncCtx)

	tc.outputPTS += int64(nbSamples)
	tc.encodeFrame(frame)
}

func clearFrameData(frame *ffmpeg.AVFrame, nbSamples int, encCtx *ffmpeg.AVCodecContext) {
	bytesPerSample := ffmpeg.AVGetBytesPerSample(encCtx.SampleFmt())
	channels := encCtx.Channels()
	size := int(bytesPerSample) * int(channels) * nbSamples

	if size > 0 && frame.Data(0) != nil {
		buf := unsafe.Slice((*byte)(unsafe.Pointer(frame.Data(0))), size)
		for i := range buf {
			buf[i] = 0
		}
	}
}

func (tc *transcoder) encodeFrame(frame *ffmpeg.AVFrame) {
	ffmpeg.AVCodecSendFrame(tc.outEncCtx, frame)

	pkt := ffmpeg.AVPacketAlloc()
	defer ffmpeg.AVPacketFree(&pkt)

	for {
		if _, err := ffmpeg.AVCodecReceivePacket(tc.outEncCtx, pkt); err != nil {
			break
		}
		pkt.SetStreamIndex(0)
		ffmpeg.AVPacketRescaleTs(pkt, tc.outEncCtx.TimeBase(), tc.outStream.TimeBase())
		ffmpeg.AVInterleavedWriteFrame(tc.outFmt, pkt)
		ffmpeg.AVPacketUnref(pkt)
	}
}

func (tc *transcoder) flushEncoder() {
	ffmpeg.AVCodecSendFrame(tc.outEncCtx, nil)

	pkt := ffmpeg.AVPacketAlloc()
	defer ffmpeg.AVPacketFree(&pkt)

	for {
		if _, err := ffmpeg.AVCodecReceivePacket(tc.outEncCtx, pkt); err != nil {
			break
		}
		pkt.SetStreamIndex(0)
		ffmpeg.AVPacketRescaleTs(pkt, tc.outEncCtx.TimeBase(), tc.outStream.TimeBase())
		ffmpeg.AVInterleavedWriteFrame(tc.outFmt, pkt)
		ffmpeg.AVPacketUnref(pkt)
	}
}

func (tc *transcoder) close() {
	if tc.outFmt != nil {
		ffmpeg.AVWriteTrailer(tc.outFmt)
	}
	if tc.outEncCtx != nil {
		ffmpeg.AVCodecFreeContext(&tc.outEncCtx)
	}
	if tc.outFmt != nil {
		if tc.outFmt.Pb != nil {
			ffmpeg.AVIOClosep(&tc.outFmt.Pb)
		}
		ffmpeg.AVFormatFreeContext(tc.outFmt)
	}
	if tc.inDecCtx != nil {
		ffmpeg.AVCodecFreeContext(&tc.inDecCtx)
	}
	if tc.inFmt != nil {
		ffmpeg.AVFormatCloseInput(&tc.inFmt)
	}
}

func computeInsertionPoints(gaps []SilenceRange, targetSec float64) ([]float64, []float64) {
	var midpoints []float64
	var padDurs []float64

	for _, gap := range gaps {
		padDur := targetSec - (gap.EndSec - gap.StartSec)
		if padDur < 0.01 {
			continue
		}
		midpoints = append(midpoints, (gap.StartSec+gap.EndSec)/2.0)
		padDurs = append(padDurs, padDur)
	}

	return midpoints, padDurs
}
