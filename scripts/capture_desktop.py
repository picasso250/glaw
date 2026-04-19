from __future__ import annotations

import argparse
import datetime as dt
import pathlib
import sys

from PIL import ImageGrab


def default_output_path() -> pathlib.Path:
    home = pathlib.Path.home()
    timestamp = dt.datetime.now().strftime("%Y%m%d-%H%M%S")
    return home / f"desktop-screenshot-{timestamp}.png"


def main() -> int:
    parser = argparse.ArgumentParser(
        description="Capture the current Windows desktop to a PNG file."
    )
    parser.add_argument(
        "--out",
        default="",
        help="Output PNG path. Defaults to ~/desktop-screenshot-YYYYMMDD-HHMMSS.png",
    )
    parser.add_argument(
        "--single-screen",
        action="store_true",
        help="Capture only the primary screen instead of all screens.",
    )
    args = parser.parse_args()

    out_path = pathlib.Path(args.out).expanduser() if args.out else default_output_path()
    out_path.parent.mkdir(parents=True, exist_ok=True)

    try:
        image = ImageGrab.grab(all_screens=not args.single_screen)
    except Exception as exc:
        print(f"capture failed: {exc}", file=sys.stderr)
        return 1

    image.save(out_path, format="PNG")
    print(f"saved={out_path}")
    print(f"size={image.width}x{image.height}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
