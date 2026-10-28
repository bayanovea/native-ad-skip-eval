#!/usr/bin/env python3
"""Renders docs/results-{light,dark}.svg from the numbers below.

The numbers are copied from `adskip rank` / `adskip score` over the runs in `runs/`
(reviewed gold, IoU 0.3, product thresholds). Re-run after changing them:
    python3 docs/charts.py
"""

from pathlib import Path

# --- data -------------------------------------------------------------------

HEADLINE = [  # held-out set, 217 videos, Sonnet 5.5, prompt v2.2
    ("52%", "of ad time removed"),
    ("2.1 s/h", "content wrongly cut"),
    ("$0.025", "per video"),
    ("217", "videos never tuned on"),
]

# (label, score, lo, hi, $/video, kind)  kind: iter | ref | rejected
ITERATIONS = [
    ("Opus 5.5 · v1 (reference)", 0.62, None, None, 0.164, "ref"),
    ("Sonnet 5.5 · v1: timestamps in the request", 0.42, 0.40, 0.43, 0.083, "iter"),
    ("v1.1: cue indices only, no timestamps", 0.28, 0.22, 0.34, 0.052, "iter"),
    ("v1.1 + cues merged into 8 s windows", 0.09, None, None, 0.046, "rejected"),
    ("v2: category rules rewritten · Batches API", 0.19, 0.19, 0.19, 0.027, "iter"),
    ("v2.1: “who paid for this piece” test", 0.43, 0.40, 0.46, 0.027, "iter"),
    ("v2.1 + extended thinking", 0.38, 0.32, 0.45, 0.028, "iter"),
    ("Haiku 4.5 · v1.1 (reference)", 0.40, None, None, 0.028, "ref"),
]

# (label, raw score, reviewed score)
SCALING = [
    ("18 videos · v2.1", -0.41, 0.43),
    ("dev-v1 · 69 videos, 40 channels · v2.1", -0.63, 0.20),
    ("dev-v1 · v2.2 (one rule added)", None, 0.31),
    ("held-out · 217 videos · v2.1", -0.61, 0.32),
    ("held-out · v2.2 (release)", -0.98, 0.40),
]

THEMES = {
    "light": dict(surface="#ffffff", text="#0b0b0b", text2="#52514e", muted="#8a8984",
                  grid="#e6e5e1", tile="#f4f4f1", blue="#2a78d6", orange="#eb6834"),
    "dark": dict(surface="#0d1117", text="#ffffff", text2="#c3c2b7", muted="#8b8a85",
                 grid="#2a2f36", tile="#161b22", blue="#3987e5", orange="#d95926"),
}

FONT = "-apple-system, BlinkMacSystemFont, 'Segoe UI', Helvetica, Arial, sans-serif"
W, H = 960, 470


def esc(s):
    return s.replace("&", "&amp;").replace("<", "&lt;").replace(">", "&gt;")


def text(x, y, s, size=12, fill=None, anchor="start", weight="normal", c=None):
    fill = fill or c["text2"]
    return (f'<text x="{x}" y="{y}" font-size="{size}" fill="{fill}" text-anchor="{anchor}" '
            f'font-weight="{weight}">{esc(s)}</text>')


def render(c):
    out = [f'<svg xmlns="http://www.w3.org/2000/svg" width="{W}" height="{H}" viewBox="0 0 {W} {H}" '
           f'font-family="{FONT}">',
           f'<rect width="{W}" height="{H}" fill="{c["surface"]}"/>']

    # headline tiles
    tw, gap, x0, y0 = 220, 12, 16, 16
    for i, (big, small) in enumerate(HEADLINE):
        x = x0 + i * (tw + gap)
        out.append(f'<rect x="{x}" y="{y0}" width="{tw}" height="66" rx="6" fill="{c["tile"]}"/>')
        out.append(text(x + 14, y0 + 36, big, 24, c["text"], weight="600", c=c))
        out.append(text(x + 14, y0 + 55, small, 12, c=c))
    out.append(text(x0, 108, "Held-out set (frozen before the first prompt) · Claude Sonnet 5.5 · "
                    "prompt v2.2 · reviewed gold", 11, c["muted"], c=c))

    # panel A: iterations
    ax, ay, aw = 16, 140, 560
    out.append(text(ax, ay, "Prompt and request-format iterations", 13, c["text"], weight="600", c=c))
    out.append(text(ax, ay + 16, "18-video dev set · SCORE with min–max over repeated runs · reviewed gold", 11,
                    c["muted"], c=c))
    lx, px0, px1, cx = ax, ax + 290, ax + 470, ax + aw - 10   # labels | plot | cost column
    row0, pitch = ay + 50, 30

    def sx(v):
        return px0 + (v + 0.1) / 0.8 * (px1 - px0)

    out.append(text(cx, row0 - 18, "$ / video", 10, c["muted"], anchor="end", c=c))
    for v in (0, 0.2, 0.4, 0.6):
        x = sx(v)
        out.append(f'<line x1="{x:.1f}" y1="{row0 - 12}" x2="{x:.1f}" y2="{row0 + pitch * len(ITERATIONS) - 14}" '
                   f'stroke="{c["grid"]}" stroke-width="1"/>')
        out.append(text(x, row0 - 18, f"{v:.1f}", 10, c["muted"], anchor="middle", c=c))
    for i, (label, s, lo, hi, cost, kind) in enumerate(ITERATIONS):
        y = row0 + i * pitch
        col = c["blue"] if kind == "iter" else (c["orange"] if kind == "rejected" else c["muted"])
        out.append(text(lx, y + 4, label, 11, c["text2"] if kind != "ref" else c["muted"], c=c))
        if lo is not None and hi > lo:
            out.append(f'<line x1="{sx(lo):.1f}" y1="{y}" x2="{sx(hi):.1f}" y2="{y}" stroke="{col}" '
                       f'stroke-width="2" stroke-linecap="round"/>')
        fill = col if kind != "ref" else c["surface"]
        out.append(f'<circle cx="{sx(s):.1f}" cy="{y}" r="5" fill="{fill}" stroke="{col}" stroke-width="2"/>')
        out.append(text(sx(s) + (10 if lo is None or hi is None else (sx(hi) - sx(s) + 10)), y + 4, f"{s:.2f}",
                        11, c["text"], weight="600", c=c))
        out.append(text(cx, y + 4, f"{cost:.3f}", 11, c["text2"], anchor="end", c=c))
    ly = row0 + pitch * len(ITERATIONS) + 2
    out.append(f'<circle cx="{lx + 5}" cy="{ly}" r="4" fill="{c["blue"]}"/>')
    out.append(text(lx + 14, ly + 4, "kept", 10, c["muted"], c=c))
    out.append(f'<circle cx="{lx + 55}" cy="{ly}" r="4" fill="{c["orange"]}"/>')
    out.append(text(lx + 64, ly + 4, "rejected: 4× more content cut", 10, c["muted"], c=c))
    out.append(f'<circle cx="{lx + 225}" cy="{ly}" r="4" fill="{c["surface"]}" stroke="{c["muted"]}" stroke-width="2"/>')
    out.append(text(lx + 234, ly + 4, "other model, for reference", 10, c["muted"], c=c))

    # panel B: scaling, raw vs reviewed
    bx, by, bw = 608, 140, 336
    out.append(text(bx, by, "Same detector, bigger sets: raw labels vs reviewed", 13, c["text"], weight="600", c=c))
    out.append(text(bx, by + 16, "SCORE against raw crowd labels and after an eye check", 11, c["muted"], c=c))
    qx0, qx1 = bx + 10, bx + bw - 10
    rowb0, pitchb = by + 62, 44

    def bxs(v):
        return qx0 + (v + 1.0) / 1.6 * (qx1 - qx0)

    for v in (-1.0, -0.5, 0, 0.5):
        x = bxs(v)
        out.append(f'<line x1="{x:.1f}" y1="{rowb0 - 24}" x2="{x:.1f}" y2="{rowb0 + pitchb * len(SCALING) - 26}" '
                   f'stroke="{c["grid"]}" stroke-width="1"/>')
        out.append(text(x, rowb0 - 30, f"{v:.1f}", 10, c["muted"], anchor="middle", c=c))
    for i, (label, raw, rev) in enumerate(SCALING):
        y = rowb0 + i * pitchb
        out.append(text(qx0, y - 12, label, 11, c["text2"], c=c))
        if raw is not None:
            out.append(f'<line x1="{bxs(raw):.1f}" y1="{y}" x2="{bxs(rev):.1f}" y2="{y}" stroke="{c["grid"]}" '
                       f'stroke-width="2"/>')
            out.append(f'<circle cx="{bxs(raw):.1f}" cy="{y}" r="5" fill="{c["surface"]}" stroke="{c["muted"]}" '
                       f'stroke-width="2"/>')
            out.append(text(bxs(raw) - 10, y + 4, f"{raw:.2f}", 11, c["muted"], anchor="end", c=c))
        out.append(f'<circle cx="{bxs(rev):.1f}" cy="{y}" r="5" fill="{c["blue"]}"/>')
        out.append(text(bxs(rev) + 10, y + 4, f"{rev:.2f}", 11, c["text"], weight="600", c=c))
    ly = rowb0 + pitchb * len(SCALING) - 14
    out.append(f'<circle cx="{qx0 + 5}" cy="{ly}" r="4" fill="{c["surface"]}" stroke="{c["muted"]}" stroke-width="2"/>')
    out.append(text(qx0 + 14, ly + 4, "raw crowd labels", 10, c["muted"], c=c))
    out.append(f'<circle cx="{qx0 + 125}" cy="{ly}" r="4" fill="{c["blue"]}"/>')
    out.append(text(qx0 + 134, ly + 4, "reviewed gold", 10, c["muted"], c=c))

    out.append(text(16, H - 12, "SCORE = (ad seconds removed − 10 × content seconds wrongly removed) / all ad seconds. "
                    "1 = perfect, 0 = no detector, negative = costs viewers more than it saves.", 10, c["muted"], c=c))
    out.append("</svg>")
    return "\n".join(out)


if __name__ == "__main__":
    here = Path(__file__).parent
    for name, theme in THEMES.items():
        (here / f"results-{name}.svg").write_text(render(theme))
        print(f"wrote docs/results-{name}.svg")
