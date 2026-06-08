package msbimport

import (
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestParseWebpDelayTicks(t *testing.T) {
	delays, ok := parseWebpDelayTicks("10\n100\n100\n")
	if !ok {
		t.Fatal("expected delay parsing to succeed")
	}
	want := []float64{10, 100, 100}
	if len(delays) != len(want) {
		t.Fatalf("delay count = %d, want %d", len(delays), len(want))
	}
	for i := range want {
		if delays[i] != want[i] {
			t.Fatalf("delay[%d] = %v, want %v", i, delays[i], want[i])
		}
	}
}

func TestParseWebpDelayTicksRejectsInvalidTiming(t *testing.T) {
	if _, ok := parseWebpDelayTicks("10\n0\n100\n"); ok {
		t.Fatal("expected zero delay to be rejected")
	}
	if _, ok := parseWebpDelayTicks("10\nwat\n100\n"); ok {
		t.Fatal("expected non-numeric delay to be rejected")
	}
}

func TestNormalizeFrameDurationsPreservesVariableDelays(t *testing.T) {
	durations := normalizeFrameDurations([]float64{10, 100, 100}, 3)
	want := []float64{0.1, 1.0, 1.0}
	if len(durations) != len(want) {
		t.Fatalf("duration count = %d, want %d", len(durations), len(want))
	}
	for i := range want {
		if math.Abs(durations[i]-want[i]) > 0.000001 {
			t.Fatalf("duration[%d] = %v, want %v", i, durations[i], want[i])
		}
	}

	total := 0.0
	for _, duration := range durations {
		total += duration
	}
	if math.Abs(total-2.1) > 0.000001 {
		t.Fatalf("total duration = %v, want 2.1", total)
	}
}

func TestAverageFPSFromDelayTicksUsesWholeAnimation(t *testing.T) {
	got := averageFPSFromDelayTicks([]float64{10, 100, 100})
	want := 3.0 * 100.0 / 210.0
	if math.Abs(got-want) > 0.000001 {
		t.Fatalf("fps = %v, want %v", got, want)
	}
}

func TestMaterializeTimedFrameSequence(t *testing.T) {
	dir := t.TempDir()
	frames := []string{
		filepath.Join(dir, "frame-00000.png"),
		filepath.Join(dir, "frame-00001.png"),
		filepath.Join(dir, "frame-00002.png"),
	}
	for _, frame := range frames {
		if err := os.WriteFile(frame, []byte("png"), 0644); err != nil {
			t.Fatalf("write frame fixture: %v", err)
		}
	}
	durations := []float64{0.1, 1.0, 1.0}

	pattern, count, err := materializeTimedFrameSequence(dir, frames, durations, kakaoWebmOutputFPS)
	if err != nil {
		t.Fatalf("materializeTimedFrameSequence returned error: %v", err)
	}
	if count != 63 {
		t.Fatalf("timed frame count = %d, want 63", count)
	}
	if !strings.HasSuffix(pattern, filepath.Join("timed", "frame-%05d.png")) {
		t.Fatalf("pattern = %q", pattern)
	}
	timedFrames, err := filepath.Glob(filepath.Join(dir, "timed", "frame-*.png"))
	if err != nil {
		t.Fatalf("glob timed frames: %v", err)
	}
	if len(timedFrames) != 63 {
		t.Fatalf("timed files = %d, want 63", len(timedFrames))
	}
}

func TestKakaoAnimatedWebpToWebmPreservesVariableDelayDuration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping ffmpeg/ImageMagick integration test in short mode")
	}

	InitConvert()
	for _, bin := range []string{CONVERT_BIN, IDENTIFY_BIN, FFMPEG_BIN, FFPROBE_BIN} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skipf("%s not available: %v", bin, err)
		}
	}

	dir := t.TempDir()
	red := filepath.Join(dir, "red.png")
	green := filepath.Join(dir, "green.png")
	blue := filepath.Join(dir, "blue.png")
	writeSolidPNG(t, red, "red")
	writeSolidPNG(t, green, "green")
	writeSolidPNG(t, blue, "blue")

	source := filepath.Join(dir, "source.webp")
	args := append([]string{}, CONVERT_ARGS...)
	args = append(args,
		"-delay", "10", red,
		"-delay", "100", green,
		"-delay", "100", blue,
		"-loop", "0", source,
	)
	if out, err := exec.Command(CONVERT_BIN, args...).CombinedOutput(); err != nil {
		t.Fatalf("create animated webp: %v\n%s", err, string(out))
	}

	webm, err := KakaoAnimatedWebpToWebm(source, NewConversionStatus())
	if err != nil {
		t.Fatalf("KakaoAnimatedWebpToWebm returned error: %v", err)
	}

	duration := ffprobeDurationForTest(t, webm)
	if duration < 2.0 || duration > 2.25 {
		t.Fatalf("duration = %.3fs, want about 2.1s", duration)
	}
}

func writeSolidPNG(t *testing.T, path string, color string) {
	t.Helper()
	args := append([]string{}, CONVERT_ARGS...)
	args = append(args, "-size", "64x64", "xc:"+color, path)
	if out, err := exec.Command(CONVERT_BIN, args...).CombinedOutput(); err != nil {
		t.Fatalf("create %s png: %v\n%s", color, err, string(out))
	}
}

func ffprobeDurationForTest(t *testing.T, path string) float64 {
	t.Helper()
	out, err := exec.Command(FFPROBE_BIN,
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "default=nw=1:nk=1",
		path,
	).Output()
	if err != nil {
		t.Fatalf("ffprobe duration: %v", err)
	}
	duration, err := strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
	if err != nil {
		t.Fatalf("parse ffprobe duration %q: %v", strings.TrimSpace(string(out)), err)
	}
	return duration
}
