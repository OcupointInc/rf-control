#!/usr/bin/env python3
"""Render rf_cable_map.csv as a labeled input/output cable diagram.

Layout (left -> right):
  * 3 input cables, 8 RF inputs each  (CH1..CH24)
  * 12 RF frontend slots             (2 ch in -> 2x UHF + 1 combined VHF out)
  * 5 output cables, 8 RF outputs each
Signals are color-coded by frontend so the customer can trace a channel
from its input pin, through its frontend, to the output cable pin.
"""
import csv
import os
import re

import matplotlib
matplotlib.use("Agg")
import matplotlib.pyplot as plt
from matplotlib.patches import FancyBboxPatch

BASE = os.path.dirname(os.path.abspath(__file__))
CSV = os.path.join(BASE, "rf_cable_map.csv")
OUT = os.path.join(BASE, "rf_cable_map.png")

# ---------------------------------------------------------------- parse CSV
inputs = []   # (channel, cable_no)
outputs = []  # (signal,  cable_no)
with open(CSV, newline="") as f:
    for row in csv.DictReader(f):
        ic = row["Input Channel"].strip()
        oc = row["Output Channel"].strip()
        if ic:
            inputs.append((ic, int(row["Input Cable No."])))
        if oc:
            outputs.append((oc, int(row["Output Cable No."])))


def frontend_of(label):
    """Return frontend index 1..12 for a CH / UHF / VHF label, or None for spare."""
    if label in ("-", ""):
        return None
    m = re.match(r"(?:CH|UHF)(\d+)", label)
    if m:
        ch = int(m.group(1))
        return (ch - 1) // 2 + 1
    m = re.match(r"VHF(\d+)/(\d+)", label)
    if m:
        return (int(m.group(1)) - 1) // 2 + 1
    return None


# group by cable number, preserving order
def group(pairs):
    out = {}
    for label, cable in pairs:
        out.setdefault(cable, []).append(label)
    return [out[k] for k in sorted(out)]


in_groups = group(inputs)
out_groups = group(outputs)

# ----------------------------------------------------------------- colors
cmap = plt.get_cmap("tab20")
FE_COLOR = {i: cmap((i - 1) % 20) for i in range(1, 13)}
SPARE = "#b8b8b8"


def tint(color, amt=0.78):
    """Lighten a color toward white for soft pin backgrounds."""
    import matplotlib.colors as mc
    r, g, b = mc.to_rgb(color)
    return (r + (1 - r) * amt, g + (1 - g) * amt, b + (1 - b) * amt)


# ----------------------------------------------------------------- layout
def layout(groups, y_top, y_bot, gap_abs):
    """Lay groups top->bottom with an absolute y gap between groups so the
    cable title always has clear room regardless of row spacing."""
    n_items = sum(len(g) for g in groups)
    n_gaps = max(len(groups) - 1, 0)
    step = (y_top - y_bot - n_gaps * gap_abs) / n_items
    pos, cur = [], y_top - step / 2
    for g in groups:
        gp = []
        for _ in g:
            gp.append(cur)
            cur -= step
        pos.append(gp)
        cur -= gap_abs
    return pos, step


Y_TOP, Y_BOT = 94, 6
in_pos, in_step = layout(in_groups, Y_TOP, Y_BOT, gap_abs=6.5)
out_pos, out_step = layout(out_groups, Y_TOP, Y_BOT, gap_abs=5.0)

# frontend y = average y of its two input channel pins
ch_y = {}
for gi, g in enumerate(in_groups):
    for li, label in enumerate(g):
        ch_y[label] = in_pos[gi][li]
fe_y = {}
for fe in range(1, 13):
    ys = [ch_y[f"CH{2*fe-1}"], ch_y[f"CH{2*fe}"]]
    fe_y[fe] = sum(ys) / 2

# ------------------------------------------------------------- geometry x
IN_X, IN_W = 9.0, 11.0          # input pin left, width
FE_X, FE_W = 41.0, 18.0         # frontend module left, width
OUT_X, OUT_W = 80.0, 13.0       # output pin left, width

fig, ax = plt.subplots(figsize=(17, 13))
ax.set_xlim(0, 100)
ax.set_ylim(0, 102)
ax.axis("off")


def pin(x, y, w, h, text, face, edge="#444", fs=8.5, weight="normal", tcol="#111"):
    ax.add_patch(FancyBboxPatch(
        (x, y - h / 2), w, h,
        boxstyle="round,pad=0.02,rounding_size=0.6",
        linewidth=0.8, edgecolor=edge, facecolor=face, zorder=3))
    ax.text(x + w / 2, y, text, ha="center", va="center",
            fontsize=fs, fontweight=weight, color=tcol, zorder=4)


# -------------------------------------------------- input cables + pins
ph_in = in_step * 0.74
for gi, g in enumerate(in_groups):
    cable = gi + 1
    ys = in_pos[gi]
    top, bot = ys[0] + in_step / 2, ys[-1] - in_step / 2
    ax.add_patch(FancyBboxPatch(
        (IN_X - 2.2, bot - 0.8), IN_W + 4.4, (top - bot) + 1.6,
        boxstyle="round,pad=0.1,rounding_size=1.2",
        linewidth=1.6, edgecolor="#222", facecolor="#f4f4f4", zorder=1))
    ax.text(IN_X + IN_W / 2, top + 3.0, f"INPUT CABLE {cable}",
            ha="center", va="center", fontsize=10.5, fontweight="bold",
            color="#222", zorder=2)
    for li, label in enumerate(g):
        fe = frontend_of(label)
        pin(IN_X, ys[li], IN_W, ph_in, label,
            tint(FE_COLOR[fe]), fs=8.5, weight="bold")
        # connector line input pin -> frontend
        ax.plot([IN_X + IN_W, FE_X], [ys[li], fe_y[fe]],
                color=FE_COLOR[fe], lw=1.0, alpha=0.65, zorder=2)

# ------------------------------------------------------- frontend slots
fe_h = in_step * 1.55
for fe in range(1, 13):
    cy = fe_y[fe]
    col = FE_COLOR[fe]
    ax.add_patch(FancyBboxPatch(
        (FE_X, cy - fe_h / 2), FE_W, fe_h,
        boxstyle="round,pad=0.05,rounding_size=0.8",
        linewidth=1.4, edgecolor=col, facecolor=tint(col, 0.88), zorder=3))
    ax.text(FE_X + FE_W / 2, cy + fe_h * 0.30, f"FRONTEND {fe}",
            ha="center", va="center", fontsize=8.5, fontweight="bold",
            color="#222", zorder=4)
    ax.text(FE_X + FE_W / 2, cy - fe_h * 0.04,
            f"in: CH{2*fe-1} · CH{2*fe}",
            ha="center", va="center", fontsize=7.8, color="#333", zorder=4)
    ax.text(FE_X + FE_W / 2, cy - fe_h * 0.34,
            f"out: UHF{2*fe-1}, UHF{2*fe}, VHF{2*fe-1}/{2*fe}",
            ha="center", va="center", fontsize=7.0, color="#333", zorder=4)

# -------------------------------------------------- output cables + pins
ph_out = out_step * 0.74
for gi, g in enumerate(out_groups):
    cable = gi + 1
    ys = out_pos[gi]
    top, bot = ys[0] + out_step / 2, ys[-1] - out_step / 2
    ax.add_patch(FancyBboxPatch(
        (OUT_X - 2.2, bot - 0.8), OUT_W + 4.4, (top - bot) + 1.6,
        boxstyle="round,pad=0.1,rounding_size=1.2",
        linewidth=1.6, edgecolor="#222", facecolor="#f4f4f4", zorder=1))
    ax.text(OUT_X + OUT_W / 2, top + 2.6, f"OUTPUT CABLE {cable}",
            ha="center", va="center", fontsize=10.5, fontweight="bold",
            color="#222", zorder=2)
    for li, label in enumerate(g):
        fe = frontend_of(label)
        if fe is None:
            pin(OUT_X, ys[li], OUT_W, ph_out, "spare",
                tint(SPARE), edge="#999", fs=8, tcol="#666")
            continue
        col = FE_COLOR[fe]
        pin(OUT_X, ys[li], OUT_W, ph_out, label, tint(col), fs=8.5, weight="bold")
        ax.plot([FE_X + FE_W, OUT_X], [fe_y[fe], ys[li]],
                color=col, lw=0.9, alpha=0.55, zorder=2)

# ------------------------------------------------------------- pin numbers
for gi, g in enumerate(in_groups):
    for li in range(len(g)):
        ax.text(IN_X - 3.0, in_pos[gi][li], str(li + 1), ha="right",
                va="center", fontsize=6.5, color="#888")
for gi, g in enumerate(out_groups):
    for li in range(len(g)):
        ax.text(OUT_X + OUT_W + 3.0, out_pos[gi][li], str(li + 1), ha="left",
                va="center", fontsize=6.5, color="#888")

# ------------------------------------------------------------------ title
ax.text(50, 100.5, "Whalepod RF Cable Map", ha="center", va="center",
        fontsize=17, fontweight="bold", color="#111")
ax.text(50, 97.6,
        "3 input cables (8 inputs each)  →  12 RF frontends (2 in / 3 out)"
        "  →  5 output cables (8 outputs each)",
        ha="center", va="center", fontsize=10, color="#555")

plt.tight_layout()
fig.savefig(OUT, dpi=150, bbox_inches="tight", facecolor="white")
print("wrote", OUT)
