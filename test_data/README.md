# test_data

Captures, measurements, and reference material collected while exercising
the devices in this repo — spectrum-analyzer screenshots, raw IQ dumps,
scope traces, bench photos, anything worth keeping next to the code.

## Layout

Top-level grouping is per device. Inside each device folder, one
subdirectory per capture or experiment, with its own `README.md`.

```
test_data/
├── README.md                       (this file)
├── black_canyon/
│   ├── README.md
│   └── <capture-name>/
│       ├── README.md               what was measured, how, and why
│       ├── <raw data files>        .csv, .npy, .s2p, .bin, …
│       └── <pictures>              .png, .jpg, screenshots, photos
├── straps/
│   └── ...
└── whalepod/
    └── ...
```

Name capture subdirectories with a date prefix when ordering matters,
e.g. `2026-05-12_900-1800_sweep/`.

## What goes in a capture README

At minimum:

- **Date** the capture was taken
- **Device + firmware** (e.g. Straps, fw rev / serial)
- **Configuration** applied — ideally the exact client calls used, or a
  pointer to the example script in `examples/`
- **Instrument setup** — spectrum analyzer model, RBW/VBW, span, scope
  settings, cables/attenuators in the chain
- **What the data shows** — one or two sentences, plus pointers to the
  files (`spectrum.png`, `iq_raw.bin`, …)

Keep it short — the goal is to make a stranger (or future-you) able to
reproduce or interpret the measurement without guessing.

## Large files

Prefer binary captures small enough to commit directly. If a file is
large enough that committing it would bloat the repo, put a pointer in
the capture's README (S3 URL, NAS path, etc.) rather than checking it
in.
