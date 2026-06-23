package msbimport

import (
	"strconv"
	"strings"
)

func mediaDurationSeconds(f string) (float64, bool) {
	if duration, ok := ffprobeDurationSeconds(f); ok {
		return duration, true
	}
	return identifyDurationSeconds(f)
}

func ffprobeDurationSeconds(f string) (float64, bool) {
	out, err := commandOutputWithTimeout(FFPROBE_BIN,
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "default=nw=1:nk=1",
		f,
	)
	if err == nil {
		if duration, ok := parsePositiveFloat(strings.TrimSpace(string(out))); ok {
			return duration, true
		}
	}

	out, err = commandOutputWithTimeout(FFPROBE_BIN,
		"-v", "error",
		"-count_packets",
		"-select_streams", "v:0",
		"-show_entries", "stream=duration,nb_read_packets,nb_read_frames,avg_frame_rate,r_frame_rate",
		"-of", "default=nw=1",
		f,
	)
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
	out, err := commandOutputWithTimeout(IDENTIFY_BIN,
		append(IDENTIFY_ARGS, "-format", "%T\n", f)...,
	)
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
