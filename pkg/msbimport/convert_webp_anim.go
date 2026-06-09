package msbimport

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	log "github.com/sirupsen/logrus"
)

type webmRateControl struct {
	minrate string
	bitrate string
	maxrate string
}

const kakaoWebmOutputFPS = 30.0

var kakaoWebmRateControls = []webmRateControl{
	{bitrate: "610k", maxrate: "910k"},
	{bitrate: "590k", maxrate: "880k"},
	{bitrate: "560k", maxrate: "840k"},
	{bitrate: "530k", maxrate: "800k"},
	{bitrate: "500k", maxrate: "750k"},
	{bitrate: "470k", maxrate: "700k"},
	{bitrate: "440k", maxrate: "660k"},
	{bitrate: "400k", maxrate: "600k"},
	{bitrate: "350k", maxrate: "520k"},
	{bitrate: "300k", maxrate: "450k"},
	{bitrate: "260k", maxrate: "390k"},
	{bitrate: "220k", maxrate: "330k"},
	{bitrate: "180k", maxrate: "270k"},
}

// KakaoAnimatedWebpToWebm converts Kakao animated WebP stickers to Telegram
// WebM. The default path writes PNG frames to disk, then runs two-pass VP9 so
// fast-motion frames get better bit allocation under Telegram's 255KiB limit.
func KakaoAnimatedWebpToWebm(f string, status *ConversionStatus) (string, error) {
	if os.Getenv("MSB_KAKAO_FAST_PIPE") == "1" && webpHasConstantFrameDelay(f) {
		return webpToWebmViaPipeFastContext(context.Background(), f, false, status)
	}
	return webpToWebmViaFramesTwoPass(f, false, status)
}

func webpToWebmViaPipeFast(f string, isCustomEmoji bool, status *ConversionStatus) (string, error) {
	return webpToWebmViaPipeFastContext(context.Background(), f, isCustomEmoji, status)
}

func animatedWebpToWebmTGVideoContext(ctx context.Context, f string, isCustomEmoji bool, status *ConversionStatus) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return f + ".webm", err
	}
	if webpHasConstantFrameDelay(f) {
		return webpToWebmViaPipeFastContext(ctx, f, isCustomEmoji, status)
	}
	log.Debugln("animatedWebpToWebmTGVideoContext: variable frame delays detected, using frame-sequence path.")
	return webpToWebmViaFramesTwoPass(f, isCustomEmoji, status)
}

func webpToWebmViaPipeFastContext(ctx context.Context, f string, isCustomEmoji bool, status *ConversionStatus) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	pathOut := f + ".webm"
	status.Clear()

	fps := webpFPS(f)
	log.Debugf("webpToWebmViaPipeFast: %s fps=%.2f", f, fps)

	scale := "512:512:force_original_aspect_ratio=decrease"
	if isCustomEmoji {
		scale = "100:100:force_original_aspect_ratio=decrease"
	}

	var lastErr error
	for _, rc := range kakaoWebmRateControls {
		if err := ctx.Err(); err != nil {
			return pathOut, err
		}
		err := webpToWebmViaPipeOnceContext(ctx, f, pathOut, scale, fps, rc)
		if err != nil {
			lastErr = err
			log.Warnln("webpToWebmViaPipeFast: retrying with two-pass frame sequence fallback.")
			os.Remove(pathOut)
			if fallback, fallbackErr := webpToWebmViaFramesTwoPass(f, isCustomEmoji, status); fallbackErr == nil {
				return fallback, nil
			} else {
				log.Warnln("webpToWebmViaPipeFast fallback ERROR:", fallbackErr)
			}
			return pathOut, err
		}
		st, err := os.Stat(pathOut)
		if err != nil || st.Size() == 0 {
			lastErr = errors.New("webpToWebmViaPipeFast: output empty")
			os.Remove(pathOut)
			continue
		}
		if st.Size() <= 255*KiB {
			status.Clear()
			return pathOut, nil
		}
		lastErr = fmt.Errorf("webpToWebmViaPipeFast: output too large: %d bytes", st.Size())
		status.Set(stickerTooLargeStatus())
		log.Warnf("webpToWebmViaPipeFast: output too large at %s, retrying lower bitrate: %d bytes", rc.bitrate, st.Size())
		os.Remove(pathOut)
	}
	if lastErr != nil {
		return pathOut, lastErr
	}
	return pathOut, errors.New("webpToWebmViaPipeFast: no encode attempts")
}

func webpToWebmViaPipeOnce(f string, pathOut string, scale string, fps float64, rc webmRateControl) error {
	return webpToWebmViaPipeOnceContext(context.Background(), f, pathOut, scale, fps, rc)
}

func webpToWebmViaPipeOnceContext(ctx context.Context, f string, pathOut string, scale string, fps float64, rc webmRateControl) error {
	if ctx == nil {
		ctx = context.Background()
	}
	ffArgs := append([]string{}, ffmpegQ...)
	ffArgs = append(ffArgs,
		"-f", "image2pipe", "-vcodec", "png",
		"-framerate", fmt.Sprintf("%g", fps),
		"-i", "pipe:0",
		"-vf", "scale="+scale,
		"-threads", "1", "-pix_fmt", "yuva420p", "-c:v", "libvpx-vp9",
		"-cpu-used", "5", "-lag-in-frames", "0", "-tile-columns", "0", "-tile-rows", "0", "-auto-alt-ref", "0",
	)
	if rc.minrate != "" {
		ffArgs = append(ffArgs, "-minrate", rc.minrate)
	}
	ffArgs = append(ffArgs, "-b:v", rc.bitrate)
	if rc.maxrate != "" {
		ffArgs = append(ffArgs, "-maxrate", rc.maxrate)
	}
	ffArgs = append(ffArgs, "-to", "00:00:03", "-an", "-y", pathOut)

	imArgs := append([]string{}, CONVERT_ARGS...)
	imArgs = append(imArgs, imageMagickResourceArgs()...)
	imArgs = append(imArgs, "WEBP:"+f, "-coalesce", "png:-")

	runCtx, cancel := context.WithTimeout(ctx, ffmpegTimeout)
	defer cancel()

	imCmd := exec.CommandContext(runCtx, CONVERT_BIN, imArgs...)
	ffCmd := niceCommandContext(runCtx, FFMPEG_BIN, ffArgs...)

	pr, pw := io.Pipe()
	imCmd.Stdout = pw
	var imOut bytes.Buffer
	imCmd.Stderr = &imOut
	var ffOut bytes.Buffer
	ffCmd.Stdin = pr
	ffCmd.Stderr = &ffOut

	releaseFFmpeg := acquireFFmpegSlot()
	if err := imCmd.Start(); err != nil {
		releaseFFmpeg()
		return fmt.Errorf("webpToWebmViaPipeOnce: imCmd start: %w", err)
	}
	if err := ffCmd.Start(); err != nil {
		releaseFFmpeg()
		imCmd.Process.Kill()
		return fmt.Errorf("webpToWebmViaPipeOnce: ffCmd start: %w", err)
	}

	imErr := imCmd.Wait()
	pw.Close()
	ffErr := ffCmd.Wait()
	releaseFFmpeg()

	if err := ctx.Err(); err != nil {
		return err
	}
	if imErr != nil || ffErr != nil {
		log.Warnln("webpToWebmViaPipeOnce ERROR ImageMagick:", imOut.String())
		log.Warnln("webpToWebmViaPipeOnce ERROR ffmpeg:", ffOut.String())
		if runCtx.Err() != nil {
			return runCtx.Err()
		}
		if ffErr != nil {
			return ffErr
		}
		return imErr
	}
	return nil
}

// webpToWebmViaFramesTwoPass trades temporary disk writes for lower memory and
// better motion quality: ImageMagick exits before ffmpeg starts, then VP9
// two-pass encoding allocates bits across the whole sticker.
func webpToWebmViaFramesTwoPass(f string, isCustomEmoji bool, status *ConversionStatus) (string, error) {
	pathOut := f + ".webm"
	status.Clear()
	frameDir, err := os.MkdirTemp(filepath.Dir(f), filepath.Base(f)+".frames-*")
	if err != nil {
		return pathOut, err
	}
	defer os.RemoveAll(frameDir)

	framePattern := filepath.Join(frameDir, "frame-%05d.png")
	size := "512x512>"
	scale := "512:512:force_original_aspect_ratio=decrease"
	if isCustomEmoji {
		size = "100x100>"
		scale = "100:100:force_original_aspect_ratio=decrease"
	}

	imArgs := append([]string{}, CONVERT_ARGS...)
	imArgs = append(imArgs, imageMagickResourceArgs()...)
	imArgs = append(imArgs, "WEBP:"+f, "-coalesce", "-resize", size, framePattern)
	imOut, err := exec.Command(CONVERT_BIN, imArgs...).CombinedOutput()
	if err != nil {
		log.Warnln("webpToWebmViaFramesTwoPass ImageMagick ERROR:", string(imOut))
		return pathOut, err
	}
	frames, err := filepath.Glob(filepath.Join(frameDir, "frame-*.png"))
	if err != nil || len(frames) == 0 {
		return pathOut, errors.New("webpToWebmViaFramesTwoPass: no frames produced")
	}
	timing := webpTimingForFrames(f, len(frames))
	timedFramePattern, _, err := materializeTimedFrameSequence(frameDir, frames, timing.durations, timing.outputFPS)
	if err != nil {
		return pathOut, err
	}

	var lastErr error
	for _, rc := range kakaoWebmRateControls {
		out, err := encodeWebmFramesTwoPass(timedFramePattern, pathOut, scale, timing.outputFPS, frameDir, rc)
		if err != nil {
			log.Warnln("webpToWebmViaFramesTwoPass ffmpeg ERROR:", string(out))
			return pathOut, err
		}
		st, err := os.Stat(pathOut)
		if err != nil || st.Size() == 0 {
			lastErr = errors.New("webpToWebmViaFramesTwoPass: output empty")
			os.Remove(pathOut)
			continue
		}
		if st.Size() <= 255*KiB {
			status.Clear()
			return pathOut, nil
		}
		lastErr = fmt.Errorf("webpToWebmViaFramesTwoPass: output too large: %d bytes", st.Size())
		status.Set(stickerTooLargeStatus())
		log.Warnf("webpToWebmViaFramesTwoPass: output too large at %s, retrying lower bitrate: %d bytes", rc.bitrate, st.Size())
		os.Remove(pathOut)
	}
	if lastErr != nil {
		return pathOut, lastErr
	}
	return pathOut, errors.New("webpToWebmViaFramesTwoPass: no encode attempts")
}

func stickerTooLargeStatus() string {
	return "too large for Telegram. Compressing..."
}

func encodeWebmFramesTwoPass(framePattern string, pathOut string, scale string, fps float64, workDir string, rc webmRateControl) (string, error) {
	keyframeInterval := int(fps + 0.5)
	if keyframeInterval < 12 {
		keyframeInterval = 12
	}
	if keyframeInterval > 30 {
		keyframeInterval = 30
	}

	passlog := filepath.Join(workDir, "vp9-passlog")
	baseArgs := append([]string{}, ffmpegQ...)
	baseArgs = append(baseArgs,
		"-framerate", fmt.Sprintf("%g", fps),
		"-i", framePattern,
		"-vf", "scale="+scale,
		"-threads", "1", "-pix_fmt", "yuva420p", "-c:v", "libvpx-vp9",
		"-cpu-used", "4", "-lag-in-frames", "0", "-tile-columns", "0", "-tile-rows", "0", "-auto-alt-ref", "0",
		"-b:v", rc.bitrate, "-maxrate", rc.maxrate,
		"-g", strconv.Itoa(keyframeInterval),
		"-to", "00:00:03", "-an",
	)

	releaseFFmpeg := acquireFFmpegSlot()
	defer releaseFFmpeg()

	pass1Args := append([]string{}, baseArgs...)
	pass1Args = append(pass1Args, "-pass", "1", "-passlogfile", passlog, "-f", "webm", "-y", os.DevNull)
	out, err := niceCommand(FFMPEG_BIN, pass1Args...).CombinedOutput()
	if err != nil {
		return string(out), err
	}

	pass2Args := append([]string{}, baseArgs...)
	pass2Args = append(pass2Args, "-pass", "2", "-passlogfile", passlog, "-y", pathOut)
	out, err = niceCommand(FFMPEG_BIN, pass2Args...).CombinedOutput()
	if err != nil {
		return string(out), err
	}
	return "", nil
}

type webpTiming struct {
	durations []float64
	outputFPS float64
}

func webpTimingForFrames(f string, frameCount int) webpTiming {
	durations, ok := webpFrameDurations(f, frameCount)
	if !ok {
		fps := webpFPS(f)
		if fps <= 0 {
			fps = 25
		}
		durations = fallbackFrameDurations(frameCount, fps)
	}
	return webpTiming{
		durations: durations,
		outputFPS: kakaoWebmOutputFPS,
	}
}

func materializeTimedFrameSequence(parentDir string, frames []string, durations []float64, fps float64) (string, int, error) {
	if len(frames) == 0 {
		return "", 0, errors.New("materializeTimedFrameSequence: no frames")
	}
	if len(frames) != len(durations) {
		return "", 0, errors.New("materializeTimedFrameSequence: frame/duration count mismatch")
	}
	if fps <= 0 {
		fps = kakaoWebmOutputFPS
	}

	timedDir := filepath.Join(parentDir, "timed")
	if err := os.MkdirAll(timedDir, 0755); err != nil {
		return "", 0, err
	}

	maxFrames := int(math.Ceil(telegramVideoMaxDuration * fps))
	if maxFrames < 1 {
		maxFrames = 1
	}

	outFrameCount := 0
	for i, frame := range frames {
		repeat := int(math.Round(durations[i] * fps))
		if repeat < 1 {
			repeat = 1
		}
		for j := 0; j < repeat && outFrameCount < maxFrames; j++ {
			dst := filepath.Join(timedDir, fmt.Sprintf("frame-%05d.png", outFrameCount))
			if err := linkOrCopyFrame(frame, dst); err != nil {
				return "", 0, err
			}
			outFrameCount++
		}
		if outFrameCount >= maxFrames {
			break
		}
	}
	if outFrameCount == 0 {
		return "", 0, errors.New("materializeTimedFrameSequence: no timed frames produced")
	}

	return filepath.Join(timedDir, "frame-%05d.png"), outFrameCount, nil
}

func linkOrCopyFrame(src string, dst string) error {
	if err := os.Link(src, dst); err == nil {
		return nil
	}

	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}

func webpFrameDurations(f string, frameCount int) ([]float64, bool) {
	delays, ok := webpDelayTicks(f)
	if !ok {
		return nil, false
	}
	return normalizeFrameDurations(delays, frameCount), true
}

func normalizeFrameDurations(delayTicks []float64, frameCount int) []float64 {
	if frameCount <= 0 {
		return nil
	}
	if len(delayTicks) == 0 {
		return fallbackFrameDurations(frameCount, 25)
	}

	durations := make([]float64, frameCount)
	for i := 0; i < frameCount; i++ {
		delayIndex := i
		if delayIndex >= len(delayTicks) {
			delayIndex = len(delayTicks) - 1
		}
		durations[i] = delayTicks[delayIndex] / 100.0
	}
	return durations
}

func fallbackFrameDurations(frameCount int, fps float64) []float64 {
	if fps <= 0 {
		fps = 25
	}
	durations := make([]float64, frameCount)
	duration := 1.0 / fps
	for i := range durations {
		durations[i] = duration
	}
	return durations
}

// webpFPS returns the average playback FPS of an animated WebP from all frame
// delays reported by ImageMagick. Falls back to 25 if unknown.
func webpFPS(f string) float64 {
	delays, ok := webpDelayTicks(f)
	if !ok {
		return 25
	}
	return averageFPSFromDelayTicks(delays)
}

func averageFPSFromDelayTicks(delays []float64) float64 {
	totalTicks := 0.0
	for _, delay := range delays {
		totalTicks += delay
	}
	if totalTicks <= 0 {
		return 25
	}
	return float64(len(delays)) * 100.0 / totalTicks
}

func webpDelayTicks(f string) ([]float64, bool) {
	out, err := exec.Command(IDENTIFY_BIN,
		append(IDENTIFY_ARGS, "-format", "%T\n", "WEBP:"+f)...,
	).Output()
	if err != nil || len(out) == 0 {
		return nil, false
	}
	return parseWebpDelayTicks(string(out))
}

func parseWebpDelayTicks(out string) ([]float64, bool) {
	var delays []float64
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		delay, err := strconv.ParseFloat(strings.TrimSpace(line), 64)
		if err != nil || delay <= 0 {
			return nil, false
		}
		delays = append(delays, delay)
	}
	if len(delays) == 0 {
		return nil, false
	}
	return delays, true
}

func webpHasConstantFrameDelay(f string) bool {
	delays, ok := webpDelayTicks(f)
	if !ok {
		return false
	}
	if len(delays) < 2 {
		return true
	}
	first := delays[0]
	for _, delay := range delays[1:] {
		if math.Abs(delay-first) > 0.001 {
			return false
		}
	}
	return true
}
