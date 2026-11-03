#!/usr/bin/env python3
"""Replace one literal, only when it occurs exactly once.

Pattern replacement that hits an earlier or later identical block has broken this
repo three times (rounds 135, 136, 159) and swallowed the tail of a Go source file
a fourth time (round 181). Every one of those runs had a needle that matched more
than once and nobody counted.

usage: edit-once.py <file> <old> <new>

applies exactly one replacement, or exits non-zero without touching the file.
"""
import sys

def main(argv):
    if len(argv) != 4:
        print("usage: edit-once.py <file> <old> <new>", file=sys.stderr)
        return 2
    path, old, new = argv[1], argv[2], argv[3]
    with open(path, encoding="utf-8") as handle:
        text = handle.read()
    hits = text.count(old)
    if hits != 1:
        print(
            "edit-once: %s matches %d time(s) in %s; refusing to edit"
            % ("the needle", hits, path),
            file=sys.stderr,
        )
        return 1
    with open(path, "w", encoding="utf-8") as handle:
        handle.write(text.replace(old, new, 1))
    print("edit-once: one replacement in %s" % path)
    return 0

if __name__ == "__main__":
    sys.exit(main(sys.argv))
