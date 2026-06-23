package msbimport

import (
	"os"
	"reflect"
	"testing"
)

func TestImageMagickResourceArgsDefaults(t *testing.T) {
	clearImageMagickLimitEnv(t)

	got := imageMagickResourceArgs()
	want := []string{"-limit", "memory", defaultImageMagickMemoryLimit, "-limit", "map", defaultImageMagickMapLimit}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("imageMagickResourceArgs() = %v, want %v", got, want)
	}

	got = imageMagickOOMResourceArgs()
	want = []string{"-limit", "memory", defaultImageMagickOOMMemoryLimit, "-limit", "map", defaultImageMagickOOMMapLimit}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("imageMagickOOMResourceArgs() = %v, want %v", got, want)
	}
}

func TestImageMagickConvertArgsUsesOOMLimits(t *testing.T) {
	clearImageMagickLimitEnv(t)
	t.Setenv("MSB_IM_MEMORY_LIMIT", "48MiB")
	t.Setenv("MSB_IM_MAP_LIMIT", "96MiB")
	t.Setenv("MSB_IM_OOM_MEMORY_LIMIT", "24MiB")
	t.Setenv("MSB_IM_OOM_MAP_LIMIT", "48MiB")

	got := imageMagickConvertArgs(true, "input.webp", "output.png")
	want := append([]string{}, CONVERT_ARGS...)
	want = append(want, "-limit", "memory", "24MiB", "-limit", "map", "48MiB", "input.webp", "output.png")
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("imageMagickConvertArgs(true) = %v, want %v", got, want)
	}
}

func clearImageMagickLimitEnv(t *testing.T) {
	t.Helper()

	for _, key := range []string{"MSB_IM_MEMORY_LIMIT", "MSB_IM_MAP_LIMIT", "MSB_IM_OOM_MEMORY_LIMIT", "MSB_IM_OOM_MAP_LIMIT"} {
		oldValue, hadValue := os.LookupEnv(key)
		cleanupKey := key
		cleanupOldValue := oldValue
		cleanupHadValue := hadValue
		if err := os.Unsetenv(key); err != nil {
			t.Fatalf("unset %s: %v", key, err)
		}
		t.Cleanup(func() {
			if cleanupHadValue {
				_ = os.Setenv(cleanupKey, cleanupOldValue)
			} else {
				_ = os.Unsetenv(cleanupKey)
			}
		})
	}
}
