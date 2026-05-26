package msbimport

import (
	"bytes"
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

var FFMPEG_BIN = "ffmpeg"
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
	args := CONVERT_ARGS
	if isCustomEmoji {
		args = append(args, "-resize", "100x100", "-gravity", "center", "-extent", "100x100", "-background", "none")
	} else {
		args = append(args, "-resize", "512x512")
	}
	args = append(args, "-filter", "Lanczos", "-define", "webp:lossless=true", f+"[0]", pathOut)

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
		args := CONVERT_ARGS
		args = append(args, "-resize", "512x512", "-filter", "Lanczos", f+"[0]", pathOut)
		exec.Command(bin, args...).CombinedOutput()
	}

	return pathOut, err
}

// Convert image to static Webp for Whatsapp, size limit is 100KiB.
func IMToWebpWA(f string) error {
	pathOut := f
	bin := CONVERT_BIN
	args := CONVERT_ARGS
	qualities := []string{"75", "50"}
	for _, q := range qualities {
		args = append(args, "-define", "webp:quality="+q,
			"-resize", "512x512", "-gravity", "center", "-extent", "512x512",
			"-background", "none", f+"[0]", pathOut)

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
	log.Infof("WebpToWebmViaPipe: %s fps=%.2f", f, fps)

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
	} else {
		log.Infof("imToApng: OK, %d bytes -> %s", st.Size(), pathOut)
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

func FFToWebmTGVideo(f string, isCustomEmoji bool) (string, error) {
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

	for rc := 0; rc < 4; rc++ {
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
		out, err := exec.Command(bin, args...).CombinedOutput()
		if err != nil {
			log.Warnln("ffToWebm ERROR:", string(out))
			//FFMPEG does not support animated webp.
			//Convert to APNG first than WEBM.
			outStr := string(out)
			webpUnsupported := strings.Contains(outStr, "skipping unsupported chunk: ANIM") ||
				strings.Contains(outStr, "image data not found")
			// Only attempt APNG fallback once (not if input is already APNG).
			if webpUnsupported && !strings.HasSuffix(f, ".apng") {
				log.Warnln("Trying to convert animated WebP to APNG first.")
				f2, err2 := IMToApng(f)
				if err2 != nil {
					log.Warnln("IMToApng ERROR:", err2)
					return pathOut, err2
				}
				return FFToWebmTGVideo(f2, isCustomEmoji)
			}
			return pathOut, err
		}
		stat, err := os.Stat(pathOut)
		if err != nil {
			return pathOut, err
		}
		if stat.Size() > 255*KiB {
			continue
		} else {
			return pathOut, err
		}
	}
	log.Errorln("FFToWebmTGVideo: unable to compress below 256KiB:", pathOut)
	return pathOut, errors.New("FFToWebmTGVideo: unable to compress below 256KiB")
}

// This function will be called if Telegram's API rejected our webm.
// It is normally due to overlength or bad FPS rate.
func FFToWebmSafe(f string, isCustomEmoji bool) (string, error) {
	pathOut := f + ".webm"
	bin := FFMPEG_BIN
	args := append([]string{}, ffmpegQ...)
	args = append(args, "-i", f)
	if isCustomEmoji {
		args = append(args, "-vf", "scale=100:100:force_original_aspect_ratio=decrease")
	} else {
		args = append(args, "-vf", "scale=512:512:force_original_aspect_ratio=decrease:flags=lanczos")
	}
	args = append(args, "-threads", "1", "-pix_fmt", "yuva420p",
		"-c:v", "libvpx-vp9", "-cpu-used", "5", "-minrate", "50k", "-b:v", "200k", "-maxrate", "300k",
		"-to", "00:00:02.800", "-r", "30", "-an", "-y", pathOut)

	out, err := exec.Command(bin, args...).CombinedOutput()
	if err != nil {
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
		"-lavfi", "split[a][b];[a]palettegen=reserve_transparent=1[p];[b][p]paletteuse=alpha_threshold=128:dither=atkinson",
		"-gifflags", "-transdiff", "-gifflags", "-offsetting",
		"-y", pathOut)

	out, err := exec.Command(bin, args...).CombinedOutput()
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
	args := CONVERT_ARGS
	args = append(args,
		"-resize", "96x96",
		"-gravity", "center", "-extent", "96x96", "-background", "none",
		f+"[0]", pathOut)

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
