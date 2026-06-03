package msbimport

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
)

// Hard ceiling for a single ffmpeg invocation. With a pool size of 1 a hung
// ffmpeg would otherwise block every queued conversion indefinitely.
const ffmpegTimeout = 120 * time.Second

// Telegram rejects video stickers longer than 3s. Sources beyond this can skip
// the first regular encode and go straight to safe mode, while sources at or
// below the limit still get a normal encode so we avoid trimming unnecessarily.
const telegramVideoMaxDuration = 3.0

// CPU-heavy encodes (VP9) run niced so the HTTP/health-check goroutine keeps
// getting CPU on the shared single-core VM. `nice` exec-replaces itself with the
// target binary (same PID), so CommandContext timeouts still reach ffmpeg.
const niceLevel = "19"

func niceCommand(bin string, args ...string) *exec.Cmd {
	return exec.Command("nice", append([]string{"-n", niceLevel, bin}, args...)...)
}

func niceCommandContext(ctx context.Context, bin string, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, "nice", append([]string{"-n", niceLevel, bin}, args...)...)
}

var FFMPEG_BIN = "ffmpeg"
var FFPROBE_BIN = "ffprobe"
var BSDTAR_BIN = "bsdtar"
var CONVERT_BIN = "convert"
var IDENTIFY_BIN = "identify"
var CONVERT_ARGS []string
var IDENTIFY_ARGS []string

// ffmpegQ are the standard quiet flags prepended to every ffmpeg call.
// -loglevel error : suppress info/warning messages, only show errors
// -nostats        : suppress the frame=.../fps=.../size=... progress line
var ffmpegQ = []string{"-hide_banner", "-loglevel", "error", "-nostats"}

const (
	FORMAT_TG_REGULAR_STATIC   = "tg_reg_static"
	FORMAT_TG_EMOJI_STATIC     = "tg_emoji_static"
	FORMAT_TG_REGULAR_ANIMATED = "tg_reg_ani"
	FORMAT_TG_EMOJI_ANIMATED   = "tg_emoji_ani"
)

// See: http://en.wikipedia.org/wiki/Binary_prefix
const (
	// Decimal
	KB = 1000
	MB = 1000 * KB
	GB = 1000 * MB
	TB = 1000 * GB
	PB = 1000 * TB

	// Binary
	KiB = 1024
	MiB = 1024 * KiB
	GiB = 1024 * MiB
	TiB = 1024 * GiB
	PiB = 1024 * TiB
)

// Should call before using functions in this package.
// Otherwise, defaults to Linux environment.
// This function also call CheckDeps to check if executables.
func InitConvert() {
	switch runtime.GOOS {
	case "linux":
		CONVERT_BIN = "convert"
	default:
		CONVERT_BIN = "magick"
		IDENTIFY_BIN = "magick"
		CONVERT_ARGS = []string{"convert"}
		IDENTIFY_ARGS = []string{"identify"}
	}
	unfoundBins := CheckDeps()
	if len(unfoundBins) != 0 {
		log.Warning("Following required executables not found!:")
		log.Warnln(strings.Join(unfoundBins, "  "))
		log.Warning("Please install missing executables to your PATH, or some features will not work!")
	}
}

// Check if required dependencies exist and return a string slice
// containing binaries that are not found in PATH.
func CheckDeps() []string {
	unfoundBins := []string{}

	if _, err := exec.LookPath(FFMPEG_BIN); err != nil {
		unfoundBins = append(unfoundBins, FFMPEG_BIN)
	}
	if _, err := exec.LookPath(FFPROBE_BIN); err != nil {
		unfoundBins = append(unfoundBins, FFPROBE_BIN)
	}
	if _, err := exec.LookPath(BSDTAR_BIN); err != nil {
		unfoundBins = append(unfoundBins, BSDTAR_BIN)
	}
	if _, err := exec.LookPath(CONVERT_BIN); err != nil {
		unfoundBins = append(unfoundBins, CONVERT_BIN)
	}
	if _, err := exec.LookPath("gifsicle"); err != nil {
		unfoundBins = append(unfoundBins, "gifsicle")
	}
	return unfoundBins
}

// Convert any image to static WEBP image, for Telegram use.
// `format` takes either FORMAT_TG_REGULAR_STATIC or FORMAT_TG_EMOJI_STATIC
func IMToWebpTGStatic(f string, isCustomEmoji bool) (string, error) {
	pathOut := f + ".webp"
	bin := CONVERT_BIN
	args := append([]string{}, CONVERT_ARGS...)
	args = append(args, f+"[0]", "-background", "none", "-alpha", "on", "-filter", "Lanczos")
	if isCustomEmoji {
		args = append(args, "-resize", "100x100", "-gravity", "center", "-extent", "100x100")
	} else {
		args = append(args, "-resize", "512x512")
	}
	args = append(args, "-define", "webp:lossless=true", pathOut)

	out, err := exec.Command(bin, args...).CombinedOutput()
	if err != nil {
		log.Warnln("IMToWebpTGRegular ERROR:", string(out))
		return "", err
	}

	st, err := os.Stat(pathOut)
	if err != nil {
		return "", err
	}

	// 100x100 should never exceed 255KIB, no need for extra check.
	if st.Size() > 255*KiB {
		args := append([]string{}, CONVERT_ARGS...)
		args = append(args, f+"[0]", "-background", "none", "-alpha", "on", "-filter", "Lanczos", "-resize", "512x512", pathOut)
		exec.Command(bin, args...).CombinedOutput()
	}

	return pathOut, err
}

// Convert image to static Webp for Whatsapp, size limit is 100KiB.
func IMToWebpWA(f string) error {
	pathOut := f
	bin := CONVERT_BIN
	qualities := []string{"75", "50"}
	for _, q := range qualities {
		args := append([]string{}, CONVERT_ARGS...)
		args = append(args,
			f+"[0]", "-background", "none", "-alpha", "on", "-filter", "Lanczos",
			"-define", "webp:quality="+q,
			"-resize", "512x512", "-gravity", "center", "-extent", "512x512",
			pathOut)

		out, err := exec.Command(bin, args...).CombinedOutput()
		if err != nil {
			log.Warnln("imToWebp ERROR:", string(out))
			return err
		}
		st, err := os.Stat(pathOut)
		if err != nil {
			return err
		}
		if st.Size() > 100*KiB {
			continue
		} else {
			return nil
		}
	}
	return errors.New("bad webp")
}

func IMToPng(f string) (string, error) {
	pathOut := f + ".png"
	bin := CONVERT_BIN
	args := CONVERT_ARGS
	args = append(args, f, pathOut)

	out, err := exec.Command(bin, args...).CombinedOutput()
	if err != nil {
		log.Warnln("imToPng ERROR:", string(out))
		return "", err
	}
	return pathOut, err
}

// WebpToWebmViaPipe converts an animated WebP to webm by streaming PNG frames
// from ImageMagick directly into ffmpeg via pipe, avoiding large intermediate
// files and reducing peak memory usage.
func WebpToWebmViaPipe(f string, isCustomEmoji bool) (string, error) {
	pathOut := f + ".webm"

	fps := webpFPS(f)
	log.Debugf("WebpToWebmViaPipe: %s fps=%.2f", f, fps)

	scale := "512:512:force_original_aspect_ratio=decrease"
	if isCustomEmoji {
		scale = "100:100:force_original_aspect_ratio=decrease"
	}

	ffArgs := append([]string{}, ffmpegQ...)
	ffArgs = append(ffArgs,
		"-f", "image2pipe", "-vcodec", "png",
		"-framerate", fmt.Sprintf("%g", fps),
		"-i", "pipe:0",
		"-vf", "scale="+scale,
		"-threads", "1", "-pix_fmt", "yuva420p", "-c:v", "libvpx-vp9",
		"-cpu-used", "8", "-lag-in-frames", "0",
		"-minrate", "50k", "-b:v", "350k", "-maxrate", "450k",
		"-to", "00:00:03", "-an", "-y", pathOut,
	)

	imArgs := append([]string{}, CONVERT_ARGS...)
	imArgs = append(imArgs, "WEBP:"+f, "-coalesce", "png:-")

	imCmd := exec.Command(CONVERT_BIN, imArgs...)
	ffCmd := exec.Command(FFMPEG_BIN, ffArgs...)

	pr, pw := io.Pipe()
	imCmd.Stdout = pw
	var ffOut bytes.Buffer
	ffCmd.Stdin = pr
	ffCmd.Stderr = &ffOut

	if err := imCmd.Start(); err != nil {
		return pathOut, fmt.Errorf("WebpToWebmViaPipe: imCmd start: %w", err)
	}
	if err := ffCmd.Start(); err != nil {
		imCmd.Process.Kill()
		return pathOut, fmt.Errorf("WebpToWebmViaPipe: ffCmd start: %w", err)
	}

	imErr := imCmd.Wait()
	pw.Close()
	ffErr := ffCmd.Wait()

	if imErr != nil || ffErr != nil {
		log.Warnln("WebpToWebmViaPipe ERROR ffmpeg:", ffOut.String())
		if ffErr != nil {
			return pathOut, ffErr
		}
		return pathOut, imErr
	}
	if st, err := os.Stat(pathOut); err != nil || st.Size() == 0 {
		return pathOut, errors.New("WebpToWebmViaPipe: output empty")
	}
	return pathOut, nil
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

func mediaDurationSeconds(f string) (float64, bool) {
	if duration, ok := ffprobeDurationSeconds(f); ok {
		return duration, true
	}
	return identifyDurationSeconds(f)
}

func ffprobeDurationSeconds(f string) (float64, bool) {
	out, err := exec.Command(FFPROBE_BIN,
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "default=nw=1:nk=1",
		f,
	).Output()
	if err == nil {
		if duration, ok := parsePositiveFloat(strings.TrimSpace(string(out))); ok {
			return duration, true
		}
	}

	out, err = exec.Command(FFPROBE_BIN,
		"-v", "error",
		"-count_packets",
		"-select_streams", "v:0",
		"-show_entries", "stream=duration,nb_read_packets,nb_read_frames,avg_frame_rate,r_frame_rate",
		"-of", "default=nw=1",
		f,
	).Output()
	if err != nil {
		return 0, false
	}

	values := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			values[parts[0]] = strings.TrimSpace(parts[1])
		}
	}
	if duration, ok := parsePositiveFloat(values["duration"]); ok {
		return duration, true
	}

	frameCount, ok := parsePositiveFloat(values["nb_read_packets"])
	if !ok {
		frameCount, ok = parsePositiveFloat(values["nb_read_frames"])
	}
	if !ok {
		return 0, false
	}

	fps, ok := parseFrameRate(values["avg_frame_rate"])
	if !ok {
		fps, ok = parseFrameRate(values["r_frame_rate"])
	}
	if !ok {
		return 0, false
	}
	return frameCount / fps, true
}

func identifyDurationSeconds(f string) (float64, bool) {
	out, err := exec.Command(IDENTIFY_BIN,
		append(IDENTIFY_ARGS, "-format", "%T\n", f)...,
	).Output()
	if err != nil || len(out) == 0 {
		return 0, false
	}

	var ticks float64
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		delay, ok := parsePositiveFloat(strings.TrimSpace(line))
		if ok {
			ticks += delay
		}
	}
	if ticks <= 0 {
		return 0, false
	}
	return ticks / 100.0, true
}

func parsePositiveFloat(s string) (float64, bool) {
	if s == "" || s == "N/A" {
		return 0, false
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || v <= 0 {
		return 0, false
	}
	return v, true
}

func parseFrameRate(s string) (float64, bool) {
	if s == "" || s == "0/0" || s == "N/A" {
		return 0, false
	}
	parts := strings.SplitN(s, "/", 2)
	if len(parts) == 1 {
		return parsePositiveFloat(s)
	}
	num, ok := parsePositiveFloat(parts[0])
	if !ok {
		return 0, false
	}
	den, ok := parsePositiveFloat(parts[1])
	if !ok {
		return 0, false
	}
	return num / den, true
}

// IMToGif converts an animated WebP (no extension) to GIF using ImageMagick.
// GIF is palette-based (8-bit) so decoded frame memory is ~4x smaller than
// APNG (RGBA), making it more suitable for memory-constrained environments.
func IMToGif(f string) (string, error) {
	pathOut := f + ".gif"
	bin := CONVERT_BIN
	args := CONVERT_ARGS
	// -coalesce ensures proper frame disposal before palette reduction.
	args = append(args, "WEBP:"+f, "-coalesce", pathOut)

	out, err := exec.Command(bin, args...).CombinedOutput()
	if err != nil {
		log.Warnln("IMToGif ERROR:", string(out))
		return "", err
	}
	if st, stErr := os.Stat(pathOut); stErr != nil || st.Size() == 0 {
		log.Warnln("IMToGif: output file missing or empty:", string(out))
		return "", errors.New("IMToGif: output file missing or empty")
	} else {
		log.Infof("IMToGif: OK, %d bytes -> %s", st.Size(), pathOut)
	}
	return pathOut, nil
}

func IMToApng(f string) (string, error) {
	pathOut := f + ".apng"
	bin := CONVERT_BIN
	args := CONVERT_ARGS
	// Use "WEBP:" prefix so ImageMagick detects the format even without a file extension.
	args = append(args, "WEBP:"+f, pathOut)

	out, err := exec.Command(bin, args...).CombinedOutput()
	if err != nil {
		log.Warnln("imToApng ERROR:", string(out))
		return "", err
	}
	// Detect silent failure: ImageMagick may exit 0 but produce an empty file.
	if st, stErr := os.Stat(pathOut); stErr != nil || st.Size() == 0 {
		log.Warnln("imToApng: output file missing or empty, ImageMagick output:", string(out))
		return "", errors.New("imToApng: output file missing or empty")
	}
	return pathOut, nil
}

// If the source is IMAGE, convert to WEBP,
// If the source is VIDEO, convert to WEBM
func ConverMediaToTGStickerSmart(f string, isCustomEmoji bool) (string, error) {
	// Count frames by running identify without -format and counting output lines.
	// This is more reliable than -format "%n" for animated WebP, which older
	// ImageMagick versions may misreport as 1 frame.
	identifyOut, err := exec.Command(IDENTIFY_BIN, append(IDENTIFY_ARGS, f)...).CombinedOutput()
	if err != nil {
		log.Warnln("ConverMediaToTGStickerSmart identify ERROR:", string(identifyOut))
		return "", err
	}

	lines := strings.Split(strings.TrimSpace(string(identifyOut)), "\n")
	frameCount := len(lines)

	if frameCount == 0 {
		log.Warnln("ConverMediaToTGStickerSmart ERROR: Frame count is zero.")
		return "", errors.New("frame count is zero")
	}

	log.Debugf("ConverMediaToTGStickerSmart: %s frameCount=%d", f, frameCount)

	if frameCount > 1 {
		return FFToWebmTGVideo(f, isCustomEmoji)
	}
	return IMToWebpTGStatic(f, isCustomEmoji)
}

// isAnimatedWebp reports whether f is an animated WebP by inspecting the
// container header. Animated files use the extended VP8X chunk with the
// animation flag (0x02) set. ffmpeg's native webp decoder cannot decode these
// (it skips the ANIM/ANMF chunks), so callers must convert via APNG first.
func isAnimatedWebp(f string) bool {
	file, err := os.Open(f)
	if err != nil {
		return false
	}
	defer file.Close()

	var hdr [21]byte
	if _, err := io.ReadFull(file, hdr[:]); err != nil {
		return false
	}
	if string(hdr[0:4]) != "RIFF" || string(hdr[8:12]) != "WEBP" {
		return false
	}
	// VP8X (extended format) is required for animation; flags byte is at offset 20.
	if string(hdr[12:16]) != "VP8X" {
		return false
	}
	return hdr[20]&0x02 != 0
}

func FFToWebmTGVideo(f string, isCustomEmoji bool) (string, error) {
	return FFToWebmTGVideoContext(context.Background(), f, isCustomEmoji)
}

func FFToWebmTGVideoContext(ctx context.Context, f string, isCustomEmoji bool) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	// Input may be gone if the session was torn down (cancel/error) while this
	// conversion was still queued. Bail fast instead of running 4 pointless rc retries.
	if _, err := os.Stat(f); err != nil {
		log.Warnln("FFToWebmTGVideo: input file gone, skipping conversion:", f)
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", err
	}

	// ffmpeg cannot decode animated WebP, so convert to APNG up front rather
	// than letting the first ffmpeg attempt fail and relying on the fallback.
	if !strings.HasSuffix(f, ".apng") && isAnimatedWebp(f) {
		log.Debugln("FFToWebmTGVideo: animated WebP detected, converting to APNG first.")
		f2, err := IMToApng(f)
		if err != nil {
			log.Warnln("IMToApng ERROR:", err)
			return "", err
		}
		f = f2
	}

	if duration, ok := mediaDurationSeconds(f); ok && duration > telegramVideoMaxDuration {
		log.Debugf("FFToWebmTGVideo: source duration %.3fs exceeds Telegram limit %.3fs, using safe mode.", duration, telegramVideoMaxDuration)
		return FFToWebmSafeContext(ctx, f, isCustomEmoji)
	}

	pathOut := f + ".webm"
	bin := FFMPEG_BIN
	baseargs := []string{}
	baseargs = append(baseargs, ffmpegQ...)
	baseargs = append(baseargs, "-i", f)
	if isCustomEmoji {
		baseargs = append(baseargs, "-vf", "scale=100:100:force_original_aspect_ratio=decrease")
	} else {
		baseargs = append(baseargs, "-vf", "scale=512:512:force_original_aspect_ratio=decrease")
	}
	// -cpu-used 8: VP9 fastest mode (0=slowest/best, 8=fastest/lowest quality)
	// -lag-in-frames 0: disable VP9 look-ahead buffer (saves ~30-50MB RSS)
	// -tile-columns 0 -tile-rows 0: disable VP9 tiling (saves additional memory)
	baseargs = append(baseargs, "-threads", "1", "-pix_fmt", "yuva420p", "-c:v", "libvpx-vp9", "-cpu-used", "8", "-lag-in-frames", "0", "-tile-columns", "0", "-tile-rows", "0")

	var lastErr error
	for rc := 0; rc < 4; rc++ {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if _, err := os.Stat(f); err != nil {
			log.Warnln("FFToWebmTGVideo: input file gone, skipping conversion:", f)
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			return "", err
		}
		rcargs := []string{}
		switch rc {
		case 0:
			rcargs = []string{"-minrate", "50k", "-b:v", "350k", "-maxrate", "450k"}
		case 1:
			rcargs = []string{"-minrate", "50k", "-b:v", "200k", "-maxrate", "300k"}
		case 2:
			rcargs = []string{"-minrate", "20k", "-b:v", "100k", "-maxrate", "200k"}
		case 3:
			rcargs = []string{"-minrate", "10k", "-b:v", "50k", "-maxrate", "100k"}
		}
		args := append(baseargs, rcargs...)
		args = append(args, []string{"-to", "00:00:03", "-an", "-y", pathOut}...)
		runCtx, cancel := context.WithTimeout(ctx, ffmpegTimeout)
		out, err := niceCommandContext(runCtx, bin, args...).CombinedOutput()
		cancel()
		if err != nil {
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			if _, statErr := os.Stat(f); statErr != nil {
				log.Warnln("FFToWebmTGVideo: input file gone, skipping conversion:", f)
				if ctx.Err() != nil {
					return "", ctx.Err()
				}
				return "", statErr
			}
			// Don't bail on the first failure; let the remaining rc attempts run
			// in case the error was transient.
			log.Warnf("ffToWebm ERROR (rc=%d), retrying:\n%s", rc, string(out))
			lastErr = err
			continue
		}
		stat, err := os.Stat(pathOut)
		if err != nil {
			lastErr = err
			continue
		}
		if stat.Size() > 255*KiB {
			continue
		}
		return pathOut, nil
	}
	if lastErr != nil {
		log.Errorln("FFToWebmTGVideo: all attempts failed:", lastErr)
		return pathOut, lastErr
	}
	log.Errorln("FFToWebmTGVideo: unable to compress below 256KiB:", pathOut)
	return pathOut, errors.New("FFToWebmTGVideo: unable to compress below 256KiB")
}

// This function will be called if Telegram's API rejected our webm.
// It is normally due to overlength or bad FPS rate.
func FFToWebmSafe(f string, isCustomEmoji bool) (string, error) {
	return FFToWebmSafeContext(context.Background(), f, isCustomEmoji)
}

func FFToWebmSafeContext(ctx context.Context, f string, isCustomEmoji bool) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if !strings.HasSuffix(f, ".apng") && isAnimatedWebp(f) {
		log.Debugln("FFToWebmSafe: animated WebP detected, converting to APNG first.")
		f2, err := IMToApng(f)
		if err != nil {
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			log.Warnln("IMToApng ERROR:", err)
			return "", err
		}
		f = f2
	}

	pathOut := f + ".webm"
	bin := FFMPEG_BIN
	args := append([]string{}, ffmpegQ...)
	args = append(args, "-i", f)
	if isCustomEmoji {
		args = append(args, "-vf", "scale=100:100:force_original_aspect_ratio=decrease:flags=lanczos,pad=100:100:-1:-1:color=black@0,format=yuva420p")
	} else {
		args = append(args, "-vf", "scale=512:512:force_original_aspect_ratio=decrease:flags=lanczos,pad=512:512:-1:-1:color=black@0,format=yuva420p")
	}
	args = append(args, "-threads", "1", "-pix_fmt", "yuva420p",
		"-c:v", "libvpx-vp9", "-cpu-used", "5", "-auto-alt-ref", "0", "-minrate", "50k", "-b:v", "200k", "-maxrate", "300k",
		"-to", "00:00:02.800", "-r", "30", "-an", "-y", pathOut)

	runCtx, cancel := context.WithTimeout(ctx, ffmpegTimeout)
	defer cancel()
	out, err := niceCommandContext(runCtx, bin, args...).CombinedOutput()
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		log.Warnf("FFToWebmSafe ERROR:\n%s", string(out))
	}
	return pathOut, err
}

func FFToGif(f string) (string, error) {
	var decoder []string
	var args []string
	if strings.HasSuffix(f, ".webm") {
		decoder = []string{"-c:v", "libvpx-vp9"}
	}
	pathOut := f + ".gif"
	bin := FFMPEG_BIN
	args = append(args, decoder...)
	args = append(args, ffmpegQ...)
	args = append(args, "-i", f,
		"-lavfi", "split[a][b];[a]palettegen=reserve_transparent=1[p];[b][p]paletteuse=alpha_threshold=128:dither=sierra2_4a",
		"-gifflags", "-transdiff", "-gifflags", "-offsetting",
		"-y", pathOut)

	ctx, cancel := context.WithTimeout(context.Background(), ffmpegTimeout)
	defer cancel()
	out, err := niceCommandContext(ctx, bin, args...).CombinedOutput()
	if err != nil {
		log.Warnf("ffToGif ERROR:\n%s", string(out))
		return "", err
	}
	//Optimize GIF produced by ffmpeg
	exec.Command("gifsicle", "--batch", "-O2", "--lossy=60", pathOut).CombinedOutput()

	return pathOut, err
}

// func FFToAPNG(f string) (string, error) {
// 	var decoder []string
// 	var args []string
// 	if strings.HasSuffix(f, ".webm") {
// 		decoder = []string{"-c:v", "libvpx-vp9"}
// 	}
// 	pathOut := f + ".apng"
// 	bin := FFMPEG_BIN
// 	args = append(args, decoder...)
// 	args = append(args, "-i", f, "-hide_banner",
// 		"-loglevel", "error", "-y", pathOut)

// 	out, err := exec.Command(bin, args...).CombinedOutput()
// 	if err != nil {
// 		log.Warnf("ffToAPNG ERROR:\n%s", string(out))
// 		return "", err
// 	}
// 	return pathOut, err
// }

func IMStackToWebp(base string, overlay string) (string, error) {
	bin := CONVERT_BIN
	args := CONVERT_ARGS
	fOut := base + ".composite.webp"

	args = append(args, base, overlay, "-background", "none", "-filter", "Lanczos", "-resize", "512x512", "-composite",
		"-define", "webp:lossless=true", fOut)
	out, err := exec.Command(bin, args...).CombinedOutput()
	if err != nil {
		log.Errorln("IM stack ERROR!", string(out))
		return "", err
	} else {
		return fOut, nil
	}
}

// Replaces tgs to gif.
func RlottieToGIF(f string) (string, error) {
	bin := "msb_rlottie.py"
	fOut := strings.ReplaceAll(f, ".tgs", ".gif")
	args := []string{f, fOut}
	out, err := exec.Command(bin, args...).CombinedOutput()
	if err != nil {
		log.Errorln("lottieToGIF ERROR!", string(out))
		return "", err
	}
	//Optimize GIF
	exec.Command("gifsicle", "--batch", "-O2", "--lossy=60", fOut).CombinedOutput()
	return fOut, nil
}

// Replaces tgs to webp.
// The only purpose for this func is for WhatsApp export.
// func RlottieToWebpWAAnimated(f string) (string, error) {
// 	bin := "msb_rlottie.py"
// 	pathOut := strings.ReplaceAll(f, ".tgs", ".webp")

// 	qualities := []string{"50", "20", "0"}
// 	for _, q := range qualities {
// 		args := []string{f, pathOut, q}
// 		out, err := exec.Command(bin, args...).CombinedOutput()
// 		if err != nil {
// 			log.Errorln("RlottieToWebp ERROR!", string(out))
// 			return "", err
// 		}
// 		//WhatsApp uses KiB.
// 		st, err := os.Stat(pathOut)
// 		if err != nil {
// 			return pathOut, err
// 		}
// 		if st.Size() > 500*KiB {
// 			log.Warnf("convert: awebp exceeded 500KiB, q:%s z:%d s:%s", q, st.Size(), pathOut)
// 			continue
// 		} else {
// 			return pathOut, nil
// 		}
// 	}
// 	log.Warnln("all quality failed! s:", pathOut)
// 	return pathOut, errors.New("bad animated webp?")
// }

// Replaces .webm ext to .webp
func IMToAnimatedWebpLQ(f string) error {
	pathOut := strings.ReplaceAll(f, ".webm", ".webp")
	bin := CONVERT_BIN
	args := CONVERT_ARGS
	args = append(args, "-resize", "128x128", f, pathOut)

	out, err := exec.Command(bin, args...).CombinedOutput()
	if err != nil {
		log.Warnln("imToWebp ERROR:", string(out))
		return err
	}
	return err
}

// Replaces .webm ext to .webp
func FFToAnimatedWebpLQ(f string) error {
	pathOut := strings.ReplaceAll(f, ".webm", ".webp")
	bin := FFMPEG_BIN

	args := append([]string{}, ffmpegQ...)
	args = append(args, "-c:v", "libvpx-vp9", "-i", f,
		"-vf", "scale=128:128:force_original_aspect_ratio=decrease",
		"-loop", "0", "-pix_fmt", "yuva420p",
		"-an", "-y", pathOut)

	out, err := exec.Command(bin, args...).CombinedOutput()
	if err != nil {
		log.Warnln("ffToAnimatedWebpWA ERROR:", string(out))
		return err
	}
	return nil
}

// // animated webp has a pretty bad compression ratio comparing to VP9,
// // shrink down quality as much as possible.
func FFToAnimatedWebpWA(f string) error {
	pathOut := strings.ReplaceAll(f, ".webm", ".webp")
	bin := FFMPEG_BIN
	//Try qualities from best to worst.
	qualities := []string{"75", "50", "20", "0", "_DS256", "_DS256Q0"}

	for _, q := range qualities {
		args := append([]string{}, ffmpegQ...)
		args = append(args, "-c:v", "libvpx-vp9", "-i", f,
			"-vf", "scale=512:512:force_original_aspect_ratio=decrease,pad=512:512:-1:-1:color=black@0",
			"-quality", q, "-loop", "0", "-pix_fmt", "yuva420p",
			"-an", "-y", pathOut)

		if q == "_DS256" {
			args = append([]string{}, ffmpegQ...)
			args = append(args, "-c:v", "libvpx-vp9", "-i", f,
				"-vf", "scale=256:256:force_original_aspect_ratio=decrease,pad=512:512:-1:-1:color=black@0",
				"-quality", "20", "-loop", "0", "-pix_fmt", "yuva420p",
				"-an", "-y", pathOut)
		}

		if q == "_DS256Q0" {
			args = append([]string{}, ffmpegQ...)
			args = append(args, "-c:v", "libvpx-vp9", "-i", f,
				"-vf", "scale=256:256:force_original_aspect_ratio=decrease,pad=512:512:-1:-1:color=black@0",
				"-quality", "0", "-loop", "0", "-pix_fmt", "yuva420p",
				"-an", "-y", pathOut)
		}

		out, err := exec.Command(bin, args...).CombinedOutput()
		if err != nil {
			log.Warnln("ffToAnimatedWebpWA ERROR:", string(out))
			return err
		}
		//WhatsApp uses KiB.
		st, err := os.Stat(pathOut)
		if err != nil {
			return err
		}
		if st.Size() > 500*KiB {
			log.Warnf("convert: awebp exceeded 500KiB, q:%s z:%d s:%s", q, st.Size(), pathOut)
			continue
		} else {
			return nil
		}
	}
	log.Warnln("all quality failed! s:", pathOut)

	return errors.New("bad animated webp?")
}

// appends png
func FFtoPNG(f string, pathOut string) error {
	var args []string
	bin := FFMPEG_BIN
	args = append(args, ffmpegQ...)
	args = append(args, "-c:v", "libvpx-vp9", "-i", f, "-frames", "1", "-y", pathOut)

	out, err := exec.Command(bin, args...).CombinedOutput()
	if err != nil {
		log.Warnf("fftoPNG ERROR:\n%s", string(out))
		return err
	}
	return err
}

// Replaces .webm or .webp to .png
func IMToPNGThumb(f string) error {
	pathOut := strings.ReplaceAll(f, ".webm", ".png")
	pathOut = strings.ReplaceAll(pathOut, ".webp", ".png")

	if strings.HasSuffix(f, ".webm") {
		tempThumb := f + ".thumb.png"
		FFtoPNG(f, tempThumb)
		f = tempThumb
	}

	bin := CONVERT_BIN
	args := append([]string{}, CONVERT_ARGS...)
	args = append(args,
		f+"[0]", "-background", "none", "-alpha", "on",
		"-resize", "96x96",
		"-gravity", "center", "-extent", "96x96",
		pathOut)

	out, err := exec.Command(bin, args...).CombinedOutput()
	if err != nil {
		log.Warnln("imToPng ERROR:", string(out))
		return err
	}
	return err
}

func SetImageTime(f string, t time.Time) error {
	return os.Chtimes(f, t, t)
	// asciiTime := t.Format("2006:01:02 15:04:05")
	// _, err := exec.Command("exiv2", "-M", "set Exif.Image.DateTime "+asciiTime, f).CombinedOutput()
	// if err != nil {
	// 	return err
	// }
	// return nil
}
