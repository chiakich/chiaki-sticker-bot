package msbimport

import (
	"context"
	"errors"
	"os"
	"strings"

	log "github.com/sirupsen/logrus"
)

// Convert any image to static WEBP image, for Telegram use.
// `format` takes either FORMAT_TG_REGULAR_STATIC or FORMAT_TG_EMOJI_STATIC
func IMToWebpTGStatic(f string, isCustomEmoji bool) (string, error) {
	return IMToWebpTGStaticContext(context.Background(), f, isCustomEmoji)
}

func IMToWebpTGStaticContext(ctx context.Context, f string, isCustomEmoji bool) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	pathOut := f + ".webp"
	args := []string{f + "[0]", "-background", "none", "-alpha", "on", "-filter", "Lanczos"}
	if isCustomEmoji {
		args = append(args, "-resize", "100x100", "-gravity", "center", "-extent", "100x100")
	} else {
		args = append(args, "-resize", "512x512")
	}
	args = append(args, "-define", "webp:lossless=true", pathOut)

	out, err := runImageMagickConvertWithOOMRetry(ctx, imageMagickTimeout, args...)
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
		args := []string{f + "[0]", "-background", "none", "-alpha", "on", "-filter", "Lanczos", "-resize", "512x512", pathOut}
		out, err := runImageMagickConvertWithOOMRetry(ctx, imageMagickTimeout, args...)
		if err != nil {
			log.Warnln("IMToWebpTGRegular fallback ERROR:", string(out))
			return "", err
		}
	}

	return pathOut, err
}

// Convert image to static Webp for Whatsapp, size limit is 100KiB.
func IMToWebpWA(f string) error {
	pathOut := f
	qualities := []string{"75", "50"}
	for _, q := range qualities {
		args := []string{
			f + "[0]", "-background", "none", "-alpha", "on", "-filter", "Lanczos",
			"-define", "webp:quality=" + q,
			"-resize", "512x512", "-gravity", "center", "-extent", "512x512",
			pathOut,
		}

		out, err := runImageMagickConvertWithOOMRetry(context.Background(), convertCommandTimeout(), args...)
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
	args := []string{f, pathOut}

	out, err := runImageMagickConvertWithOOMRetry(context.Background(), convertCommandTimeout(), args...)
	if err != nil {
		log.Warnln("imToPng ERROR:", string(out))
		return "", err
	}
	return pathOut, err
}

// Replaces .webm ext to .webp
func IMToAnimatedWebpLQ(f string) error {
	pathOut := strings.ReplaceAll(f, ".webm", ".webp")
	args := []string{"-resize", "128x128", f, pathOut}

	out, err := runImageMagickConvertWithOOMRetry(context.Background(), convertCommandTimeout(), args...)
	if err != nil {
		log.Warnln("imToWebp ERROR:", string(out))
		return err
	}
	return err
}
