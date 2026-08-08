#!/usr/bin/env python3
"""glyph-audit.py — find UI glyphs that no loaded font can draw.

imzero2 loads four faces and composes them into two family chains
(rust/imzero2/src/imzero2/apphost.rs):

    Proportional:  main -> phosphor -> fallback
    Monospace:     mono -> phosphor -> fallback

A codepoint outside every face in the chain that draws it renders as a tofu
box. A codepoint that lands on the CJK fallback renders at that face's
ideographic metrics — oversized and off the Latin baseline — which is the
defect the imzero2 skill's §12 "Oversized, Off-Centre Glyph" describes. And two
marks in one vocabulary can rasterise identically at UI size even when their
codepoints differ, which is how ◈ passed for ◆ for as long as it did.

This checks all three by rasterising each glyph in the face that will actually
draw it, at the size it is actually drawn.

    scripts/dev/glyph-audit.py                 # audit the repo, exit 1 on new tofu
    scripts/dev/glyph-audit.py --baseline scripts/ci/glyph-baseline.txt
    scripts/dev/glyph-audit.py --write-baseline scripts/ci/glyph-baseline.txt
    scripts/dev/glyph-audit.py --all           # include fallback-drawn glyphs
    scripts/dev/glyph-audit.py ◆ ◇ ❖           # audit an explicit set instead
    scripts/dev/glyph-audit.py --mono ⟨ ⟩       # ... resolved in the Monospace chain

Notes and limits:

  * It reads Go string literals in non-test, non-generated sources; a glyph
    assembled at runtime is invisible to it, as is one in Rust or in a doc.
  * It cannot tell a rendered widget from a terminal report or a deliberate
    font-fallback demo. `wasmsurvey`'s report, `eh`'s error formatting and the
    SQL demo's flag emoji are all text by intent — read the site before
    changing it.
  * It assumes a PROPORTIONAL run unless the literal's line mentions
    Monospace(), which is a heuristic — a Monospace() call on a preceding line
    is missed and reports a false tofu.
  * The faces come from fc-match, so a run on another machine resolves against
    that machine's fonts. That is the point rather than a flaw: the answer is
    host-specific, and a host with a thinner font stack has more tofu. But it
    also makes this a poor GATE on an arbitrary box, so the run REFUSES to
    judge unless fontconfig actually returned the families it asked for —
    otherwise it reports what it found and exits 0, because "this machine has
    no Noto Sans" is not a finding about the repo.
  * Text is not the target. CJK samples, Greek, dashes and quotes legitimately
    fall back; --all shows them, the default does not.
"""
import argparse
import os
import re
import struct
import subprocess
import sys
import unicodedata

try:
    from PIL import Image, ImageDraw, ImageFont
except ImportError:
    sys.exit("glyph-audit: needs Pillow (python3-pillow)")

REPO = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
ROOTS = ["public", "apps"]
PHOSPHOR = os.path.join(REPO, "rust/imzero2/assets/fonts/phosphor/Phosphor.ttf")

# Sizes worth checking: body text and the small badge tier.
SIZES = (12, 13)


def cmap(path):
    """Every codepoint a TrueType/OpenType file maps, formats 4 and 12."""
    data = open(path, "rb").read()
    off = 0
    if data[:4] == b"ttcf":
        off = struct.unpack(">I", data[12:16])[0]
    ntab = struct.unpack(">H", data[off + 4 : off + 6])[0]
    cm = None
    for i in range(ntab):
        rec = off + 12 + 16 * i
        if data[rec : rec + 4] == b"cmap":
            cm = struct.unpack(">I", data[rec + 8 : rec + 12])[0]
    if cm is None:
        return set()
    nsub = struct.unpack(">H", data[cm + 2 : cm + 4])[0]
    chars = set()
    for i in range(nsub):
        _, _, o = struct.unpack(">HHI", data[cm + 4 + 8 * i : cm + 12 + 8 * i])
        o += cm
        fmt = struct.unpack(">H", data[o : o + 2])[0]
        if fmt == 4:
            seg2 = struct.unpack(">H", data[o + 6 : o + 8])[0]
            seg = seg2 // 2
            ends = struct.unpack(">%dH" % seg, data[o + 14 : o + 14 + seg2])
            starts = struct.unpack(">%dH" % seg, data[o + 16 + seg2 : o + 16 + 2 * seg2])
            for s, e in zip(starts, ends):
                if s == 0xFFFF:
                    continue
                chars.update(range(s, e + 1))
        elif fmt == 12:
            ngroups = struct.unpack(">I", data[o + 12 : o + 16])[0]
            for g in range(ngroups):
                b = o + 16 + 12 * g
                sc, ec, _ = struct.unpack(">III", data[b : b + 12])
                chars.update(range(sc, min(ec, sc + 0x10000) + 1))
    return chars


def fc(family):
    """Resolve a family to a file, and say whether fontconfig really had it.

    fc-match always answers, substituting something when the family is absent,
    so the family name comes back too and the caller checks it. Without that a
    box with no Noto Sans silently audits against whatever it does have.
    """
    try:
        out = subprocess.check_output(
            ["fc-match", "-f", "%{file}\t%{family}", family]
        ).decode()
    except Exception:
        return "", False
    path, _, fam = out.partition("\t")
    want = family.split()[0].lower()
    return path, want in fam.lower()


def load_faces():
    """The four faces, resolved the way rust/imzero2/hmi.sh resolves them."""
    resolved = {
        "main": fc("Noto Sans"),
        "mono": fc("DejaVu Sans Mono"),
        "fallback": fc("Noto Sans Mono CJK JP"),
    }
    faces = {k: v[0] for k, v in resolved.items()}
    faces["phosphor"] = PHOSPHOR if os.path.exists(PHOSPHOR) else ""
    exact = all(v[1] for v in resolved.values()) and bool(faces["phosphor"])
    cov, path = {}, {}
    for name, p in faces.items():
        if not p or not os.path.exists(p):
            continue
        try:
            cov[name] = cmap(p)
            path[name] = p
        except Exception as exc:
            print(f"glyph-audit: cannot read {name} ({p}): {exc}", file=sys.stderr)
    return cov, path, exact


# The chains apphost.rs registers.
CHAINS = {"prop": ("main", "phosphor", "fallback"), "mono": ("mono", "phosphor", "fallback")}


def resolve(ch, chain, cov):
    for face in CHAINS[chain]:
        if face in cov and ord(ch) in cov[face]:
            return face
    return None


def raster(ch, path, size):
    font = ImageFont.truetype(path, size)
    im = Image.new("L", (size + 8, size + 8), 0)
    ImageDraw.Draw(im).text((3, 1), ch, font=font, fill=255)
    px = im.load()
    return tuple(
        tuple(1 if px[x, y] > 110 else 0 for x in range(im.size[0])) for y in range(im.size[1])
    )


def ink_box(r):
    pts = [(x, y) for y, row in enumerate(r) for x, v in enumerate(row) if v]
    if not pts:
        return None
    xs = [p[0] for p in pts]
    ys = [p[1] for p in pts]
    return len(pts), max(xs) - min(xs) + 1, max(ys) - min(ys) + 1


LITERAL = re.compile(r'"((?:[^"\\]|\\.)*)"')


def scan():
    """Every non-ASCII, non-PUA character in a Go string literal, with sites."""
    hits = {}
    for root in ROOTS:
        for dirpath, _, names in os.walk(os.path.join(REPO, root)):
            for name in names:
                # Tests and generated files render nothing.
                if not name.endswith(".go") or name.endswith("_test.go"):
                    continue
                if name.endswith(".out.go"):
                    continue
                p = os.path.join(dirpath, name)
                try:
                    src = open(p, encoding="utf-8").read()
                except Exception:
                    continue
                for i, line in enumerate(src.splitlines(), 1):
                    if line.lstrip().startswith("//"):
                        continue
                    mono = "Monospace()" in line
                    for m in LITERAL.finditer(line):
                        for ch in m.group(1):
                            o = ord(ch)
                            if o < 0x80 or 0xE000 <= o <= 0xF8FF:
                                continue
                            rel = os.path.relpath(p, REPO)
                            hits.setdefault(ch, []).append((rel, i, "mono" if mono else "prop"))
    return hits


def main():
    ap = argparse.ArgumentParser(add_help=False)
    ap.add_argument("glyphs", nargs="*")
    ap.add_argument("--all", action="store_true", help="also list fallback-drawn glyphs")
    ap.add_argument("--mono", action="store_true", help="resolve explicit glyphs in the Monospace chain")
    ap.add_argument("--baseline", metavar="FILE", help="accept the tofu listed there; fail only on new")
    ap.add_argument("--write-baseline", metavar="FILE", help="record today's tofu as accepted")
    ap.add_argument("-h", "--help", action="store_true")
    args = ap.parse_args()
    if args.help:
        print(__doc__)
        return 0

    cov, path, exact = load_faces()
    if "main" not in cov:
        print("glyph-audit: no main font resolved; is fontconfig present?", file=sys.stderr)
        return 2
    print("faces: " + ", ".join(f"{k}={os.path.basename(v)}" for k, v in sorted(path.items())))
    print(f"chains: prop = {' -> '.join(CHAINS['prop'])} | mono = {' -> '.join(CHAINS['mono'])}\n")

    if args.glyphs:
        chain = "mono" if args.mono else "prop"
        sets = {ch: [("<argv>", 0, chain)] for g in args.glyphs for ch in g}
    else:
        sets = scan()

    tofu, fallbackers = [], []
    rasters = {}
    for ch, sites in sorted(sets.items()):
        for chain in sorted({s[2] for s in sites}):
            face = resolve(ch, chain, cov)
            where = sites[0][0] + ":" + str(sites[0][1])
            more = f" (+{len(sites)-1} more)" if len(sites) > 1 else ""
            try:
                name = unicodedata.name(ch)
            except ValueError:
                name = "?"
            if face is None:
                tofu.append((ch, name, chain, where, more))
                continue
            if face == "fallback":
                fallbackers.append((ch, name, chain, where, more))
            for size in SIZES:
                r = raster(ch, path[face], size)
                rasters.setdefault(size, {}).setdefault(face, {})[ch] = r

    # A baseline key is (codepoint, file) — not the line, which drifts with any
    # edit above it and would churn the file for no signal.
    def key(ch, where):
        return f"U+{ord(ch):04X}\t{where.rsplit(':', 1)[0]}"

    if args.write_baseline:
        with open(args.write_baseline, "w", encoding="utf-8") as fh:
            fh.write("# Accepted tofu — glyphs no loaded font draws, that are NOT defects.\n")
            fh.write("# Written by scripts/dev/glyph-audit.py --write-baseline.\n")
            fh.write("# One `U+XXXX<tab>path` per site; the line number is deliberately\n")
            fh.write("# absent so an edit above a site does not churn this file.\n")
            for ch, _, _, where, _ in sorted(tofu, key=lambda t: (ord(t[0]), t[3])):
                fh.write(key(ch, where) + "\n")
        print(f"wrote {len(tofu)} accepted sites to {args.write_baseline}")
        return 0

    accepted = set()
    if args.baseline and os.path.exists(args.baseline):
        for line in open(args.baseline, encoding="utf-8"):
            line = line.strip()
            if line and not line.startswith("#"):
                accepted.add(line)
    new_tofu = [t for t in tofu if key(t[0], t[3]) not in accepted]

    if tofu:
        label = "TOFU — no face in the chain draws these; they render as empty boxes:"
        print(label)
        for ch, name, chain, where, more in tofu:
            mark = "NEW " if key(ch, where) not in accepted else "    "
            print(f"  {mark}{ch}  U+{ord(ch):04X} {name[:36]:38s} [{chain}] {where}{more}")
        if accepted and not new_tofu:
            print(f"  ({len(tofu)} sites, all accepted in {os.path.basename(args.baseline)})")
    else:
        print("TOFU — none.")

    if args.all and fallbackers:
        print("\nDrawn by the CJK fallback (its metrics, not ours — see skill §12):")
        for ch, name, chain, where, more in fallbackers:
            print(f"  {ch}  U+{ord(ch):04X} {name[:38]:40s} [{chain}] {where}{more}")

    def finish():
        """The exit status. A host without the fonts this audits against is
        not evidence about the repo, so it reports and passes."""
        if not exact:
            print("\nfontconfig did not return the families this audits against"
                  " (Noto Sans / DejaVu Sans Mono / Noto Sans Mono CJK, plus the"
                  "\nbundled Phosphor). Reporting only — a host's missing font"
                  " is not a finding about the repo.")
            return 0
        if args.baseline:
            if new_tofu:
                print(f"\n{len(new_tofu)} site(s) not in {args.baseline}.")
                print("Fix them, or — if the glyph is not drawn by imzero2 —"
                      " add them there WITH A REASON.")
            return 1 if new_tofu else 0
        return 1 if tofu else 0

    # Informational, and behind --all: over a whole repo this is dominated by
    # box-drawing runs and CJK samples, which are text and legitimately drawn by
    # the fallback. It earns its keep on an explicit set of marks — a vocabulary
    # you are designing — not on a sweep.
    if not args.all:
        return finish()

    print("\nOversized / collided at UI size:")
    flagged = False
    for size, byface in sorted(rasters.items()):
        for face, glyphs in sorted(byface.items()):
            seen = {}
            for ch, r in glyphs.items():
                box = ink_box(r)
                if box and box[0] > 70:
                    print(f"  {size}px [{face}] {ch} ink={box[0]} {box[1]}x{box[2]} — reads oversized")
                    flagged = True
                if r in seen and seen[r] != ch:
                    print(f"  {size}px [{face}] {ch} and {seen[r]} rasterise IDENTICALLY")
                    flagged = True
                seen[r] = ch
    if not flagged:
        print("  none.")
    return finish()


if __name__ == "__main__":
    sys.exit(main())
