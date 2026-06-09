package msbimport

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"

	log "github.com/sirupsen/logrus"
)

func FFToWebmTGVideo(f string, isCustomEmoji bool) (string, error) {
	return FFToWebmTGVideoContext(context.Background(), f, isCustomEmoji)
}

func FFToWebmTGVideoContext(ctx context.Context, f string, isCustomEmoji bool) (string, error) {
	return FFToWebmTGVideoContextWithStatus(ctx, f, isCustomEmoji, nil)
}

func FFToWebmTGVideoContextWithStatus(ctx context.Context, f string, isCustomEmoji bool, status *ConversionStatus) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	status.Clear()
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

	// ffmpeg cannot decode animated WebP directly. Route those through the
	// lower-memory WebP pipeline instead of materializing a large APNG first.
	if !strings.HasSuffix(f, ".apng") && isAnimatedWebp(f) {
		log.Debugln("FFToWebmTGVideo: animated WebP detected, using streaming/frame-sequence pipeline.")
		return animatedWebpToWebmTGVideoContext(ctx, f, isCustomEmoji, status)
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
	baseargs = append(baseargs, "-threads", "1", "-pix_fmt", "yuva420p", "-c:v", "libvpx-vp9", "-cpu-used", "8", "-lag-in-frames", "0", "-tile-columns", "0", "-tile-rows", "0", "-auto-alt-ref", "0")

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
		releaseFFmpeg := acquireFFmpegSlot()
		out, err := niceCommandContext(runCtx, bin, args...).CombinedOutput()
		releaseFFmpeg()
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
			status.Set(stickerTooLargeStatus())
			continue
		}
		status.Clear()
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
		log.Debugln("FFToWebmSafe: animated WebP detected, using streaming/frame-sequence pipeline.")
		return animatedWebpToWebmTGVideoContext(ctx, f, isCustomEmoji, nil)
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
		"-c:v", "libvpx-vp9", "-cpu-used", "5", "-lag-in-frames", "0", "-tile-columns", "0", "-tile-rows", "0", "-auto-alt-ref", "0", "-minrate", "50k", "-b:v", "200k", "-maxrate", "300k",
		"-to", "00:00:02.800", "-r", "30", "-an", "-y", pathOut)

	runCtx, cancel := context.WithTimeout(ctx, ffmpegTimeout)
	defer cancel()
	releaseFFmpeg := acquireFFmpegSlot()
	out, err := niceCommandContext(runCtx, bin, args...).CombinedOutput()
	releaseFFmpeg()
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
	releaseFFmpeg := acquireFFmpegSlot()
	out, err := niceCommandContext(ctx, bin, args...).CombinedOutput()
	releaseFFmpeg()
	if err != nil {
		log.Warnf("ffToGif ERROR:\n%s", string(out))
		return "", err
	}
	//Optimize GIF produced by ffmpeg
	exec.Command("gifsicle", "--batch", "-O2", "--lossy=60", pathOut).CombinedOutput()

	return pathOut, err
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
