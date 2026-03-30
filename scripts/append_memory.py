from __future__ import annotations

import pathlib
import sys


def main() -> int:
    if len(sys.argv) != 3:
        print("usage: python scripts/append_memory.py <memory-file> <line>", file=sys.stderr)
        return 2

    path = pathlib.Path(sys.argv[1]).expanduser()
    line = sys.argv[2]

    if "\n" in line or "\r" in line:
        print("line must be a single logical line", file=sys.stderr)
        return 1

    path.parent.mkdir(parents=True, exist_ok=True)
    with path.open("a", encoding="utf-8", newline="\n") as f:
        f.write(line)
        f.write("\n")

    print(f"appended 1 line to {path}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
