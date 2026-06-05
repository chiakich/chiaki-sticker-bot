#!/usr/bin/python3

# Utilize rlottie-python to convert TGS images.
# Credit https://github.com/laggykiller/rlottie-python GPL-2.0 license  Copyright @laggykiller


# Example:
# msb_rlottie in.tgs out.gif

import os
import subprocess
from rlottie_python import LottieAnimation
import sys


DEFAULT_FPS = 20
DEFAULT_MAX_SIZE = 384


def env_int(name, default, minimum=None, maximum=None):
    try:
        value = int(os.environ.get(name, default))
    except (TypeError, ValueError):
        value = default
    if minimum is not None:
        value = max(minimum, value)
    if maximum is not None:
        value = min(maximum, value)
    return value


def scaled_size(width, height, max_size):
    if max_size <= 0 or (width <= max_size and height <= max_size):
        return width, height

    if width >= height:
        new_width = max_size
        new_height = max(1, round(height * max_size / width))
    else:
        new_height = max_size
        new_width = max(1, round(width * max_size / height))
    return new_width, new_height


def render_streaming_gif(anim, f_out):
    source_width, source_height = anim.lottie_animation_get_size()
    width, height = scaled_size(
        source_width,
        source_height,
        env_int("MSB_RLOTTIE_MAX_SIZE", DEFAULT_MAX_SIZE, minimum=1),
    )
    fps = env_int("MSB_RLOTTIE_FPS", DEFAULT_FPS, minimum=1, maximum=50)

    source_fps = anim.lottie_animation_get_framerate()
    total_frames = anim.lottie_animation_get_totalframe()
    if source_fps <= 0 or total_frames <= 0:
        raise RuntimeError("invalid lottie animation metadata")

    output_frames = max(1, round(total_frames * fps / source_fps))
    cmd = [
        "ffmpeg",
        "-hide_banner",
        "-loglevel",
        "error",
        "-nostats",
        "-y",
        "-f",
        "rawvideo",
        "-pix_fmt",
        "bgra",
        "-s",
        f"{width}x{height}",
        "-r",
        str(fps),
        "-i",
        "pipe:0",
        "-loop",
        "0",
        f_out,
    ]

    proc = subprocess.Popen(cmd, stdin=subprocess.PIPE)
    try:
        for output_frame in range(output_frames):
            source_frame = min(
                total_frames - 1,
                round(output_frame * source_fps / fps),
            )
            frame = anim.lottie_animation_render(
                frame_num=source_frame,
                width=width,
                height=height,
            )
            proc.stdin.write(frame)
    finally:
        if proc.stdin:
            proc.stdin.close()

    if proc.wait() != 0:
        raise RuntimeError("ffmpeg failed while encoding gif")


def main():
    f_in = sys.argv[1]
    f_out = sys.argv[2]

    anim = LottieAnimation.from_tgs(f_in)
    try:
        render_streaming_gif(anim, f_out)
    except Exception as err:
        print(f"streaming render failed, falling back to save_animation: {err}", file=sys.stderr)
        fps = env_int("MSB_RLOTTIE_FPS", DEFAULT_FPS, minimum=1, maximum=50)
        max_size = env_int("MSB_RLOTTIE_MAX_SIZE", DEFAULT_MAX_SIZE, minimum=1)
        width, height = scaled_size(*anim.lottie_animation_get_size(), max_size)
        anim.save_animation(f_out, fps=fps, width=width, height=height)

main()
