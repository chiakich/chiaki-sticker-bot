#!/usr/bin/python3

# Convert animated WebP to GIF using Pillow.
# Usage: msb_webp_to_gif.py input.webp output.gif

from PIL import Image
import sys

def main():
    f_in = sys.argv[1]
    f_out = sys.argv[2]

    img = Image.open(f_in)
    frames = []
    durations = []
    try:
        while True:
            frames.append(img.copy().convert("RGBA"))
            durations.append(img.info.get("duration", 100))
            img.seek(img.tell() + 1)
    except EOFError:
        pass

    if not frames:
        print("msb_webp_to_gif: no frames found", file=sys.stderr)
        sys.exit(1)

    frames[0].save(
        f_out,
        format="GIF",
        save_all=True,
        append_images=frames[1:],
        loop=0,
        duration=durations,
        disposal=2,
    )

main()
