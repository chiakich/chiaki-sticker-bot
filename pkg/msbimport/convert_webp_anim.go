package msbimport

import (
	"bytes"
	"errors"
	"fmt"
	"io"
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

var kakaoWebmRateControls = []webmRateControl{
	{bitrate: "650k", maxrate: "980k"},
	{bitrate: "630k", maxrate: "940k"},
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
func KakaoAnimatedWebpToWebm(f string) (string, error) {
	if os.Getenv("MSB_KAKAO_FAST_PIPE") != "1" {
		return webpToWebmViaFramesTwoPass(f, false)
	}
	return webpToWebmViaPipeFast(f, false)
}

func webpToWebmViaPipeFast(f string, isCustomEmoji bool) (string, error) {
	pathOut := f + ".webm"

	fps := webpFPS(f)
	log.Debugf("webpToWebmViaPipeFast: %s fps=%.2f", f, fps)

	scale := "512:512:force_original_aspect_ratio=decrease"
	if isCustomEmoji {
		scale = "100:100:force_original_aspect_ratio=decrease"
	}

	var lastErr error
	for _, rc := range kakaoWebmRateControls {
		err := webpToWebmViaPipeOnce(f, pathOut, scale, fps, rc)
		if err != nil {
			lastErr = err
			log.Warnln("webpToWebmViaPipeFast: retrying with two-pass frame sequence fallback.")
			os.Remove(pathOut)
			if fallback, fallbackErr := webpToWebmViaFramesTwoPass(f, isCustomEmoji); fallbackErr == nil {
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
			return pathOut, nil
		}
		lastErr = fmt.Errorf("webpToWebmViaPipeFast: output too large: %d bytes", st.Size())
		log.Warnf("webpToWebmViaPipeFast: output too large at %s, retrying lower bitrate: %d bytes", rc.bitrate, st.Size())
		os.Remove(pathOut)
	}
	if lastErr != nil {
		return pathOut, lastErr
	}
	return pathOut, errors.New("webpToWebmViaPipeFast: no encode attempts")
}

func webpToWebmViaPipeOnce(f string, pathOut string, scale string, fps float64, rc webmRateControl) error {
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

	imCmd := exec.Command(CONVERT_BIN, imArgs...)
	ffCmd := niceCommand(FFMPEG_BIN, ffArgs...)

	pr, pw := io.Pipe()
	imCmd.Stdout = pw
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

	if imErr != nil || ffErr != nil {
		log.Warnln("webpToWebmViaPipeOnce ERROR ffmpeg:", ffOut.String())
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
func webpToWebmViaFramesTwoPass(f string, isCustomEmoji bool) (string, error) {
	pathOut := f + ".webm"
	fps := webpFPS(f)
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

	var lastErr error
	for _, rc := range kakaoWebmRateControls {
		out, err := encodeWebmFramesTwoPass(framePattern, pathOut, scale, fps, frameDir, rc)
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
			return pathOut, nil
		}
		lastErr = fmt.Errorf("webpToWebmViaFramesTwoPass: output too large: %d bytes", st.Size())
		log.Warnf("webpToWebmViaFramesTwoPass: output too large at %s, retrying lower bitrate: %d bytes", rc.bitrate, st.Size())
		os.Remove(pathOut)
	}
	if lastErr != nil {
		return pathOut, lastErr
	}
	return pathOut, errors.New("webpToWebmViaFramesTwoPass: no encode attempts")
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

// webpFPS returns the playback FPS of an animated WebP by reading the first
// frame's delay (in centiseconds) via identify. Falls back to 25 if unknown.
func webpFPS(f string) float64 {
	out, err := exec.Command(IDENTIFY_BIN,
		append(IDENTIFY_ARGS, "-format", "%T\n", "WEBP:"+f)...,
	).Output()
	if err != nil || len(out) == 0 {
		return 25
	}
	first := strings.SplitN(strings.TrimSpace(string(out)), "\n", 2)[0]
	delay, err := strconv.ParseFloat(strings.TrimSpace(first), 64)
	if err != nil || delay <= 0 {
		return 25
	}
	return 100.0 / delay
}
