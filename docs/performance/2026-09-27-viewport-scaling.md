# Viewport scaling on ea — 2026-09-27

Same build (`2beb236`), fixture, browser and 24 FPS cap. Two sequential passes, reversing viewport and renderer order. Each workload: 5s warmup then 15s measurement. Canvas animation, drained PTY, Sixel/JPEG/Websafe and Kitty/PNG defaults. Ranges below span two runs, not confidence intervals. Writes/s is pipeline output, not visible terminal FPS. 1920×1072 approximates 1080p because terminal cells are 16 pixels tall.

## Findings

- Increasing the viewport from 720p to 4K processes nine times as many pixels.
  Sixel output fell about sevenfold (roughly 16 to 2.27 writes/s), with preparation
  rising from about 44 to 350 ms. Preparation plus the existing adaptive pacing
  dominate this workload at the larger sizes.
- Kitty output stayed near 15 writes/s through 1440p, then fell to 10 at 4K.
  Its capture p50 rose from about 65 to 99 ms; client preparation remained near
  1 ms at 4K. Browser capture is the largest measured stage on this path.
- Client peak RSS grew from 129–130 to 777 MiB for Sixel and 87–91 to
  157–167 MiB for Kitty. These are observed process peaks, not arena capacity
  measurements; the sweep does not isolate which allocations contribute each byte.
- None of these runs reaches the 24 FPS ceiling. Both reverse-order passes show
  the same broad scaling. Viewport growth can explain a substantial frame-rate
  drop, but this experiment does not establish the cause of a user's subjective
  regression or compare software versions.
- These are each renderer's normal defaults, not a controlled JPEG-versus-PNG
  comparison. Kitty PNG decoding/display inside a real terminal and Sixel
  parsing/display there are both excluded from the drained-PTY measurements.

## Sixel

| Viewport | Pixels (M) | Writes/s | Capture p50 (ms) | Prepare p50 (ms) | Prepare p95 (ms) | Total CPU (% one core) | Client peak RSS (MiB) |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 1280×720 | 0.92 | 15.93–16.07 | 56.14–56.83 | 42.99–45.06 | 58.22–58.32 | 125.40–128.07 | 129.13–130.13 |
| 1920×1072 | 2.06 | 8.47–8.60 | 62.43–69.83 | 91.07–91.90 | 102.45–108.68 | 132.90–137.70 | 222.58–222.59 |
| 2560×1440 | 3.69 | 4.87–4.93 | 88.92–91.81 | 158.95–163.21 | 174.18–179.53 | 135.63–135.83 | 388.52–390.46 |
| 3840×2160 | 8.29 | 2.27 | 136.08–142.96 | 348.91–351.62 | 373.08–383.14 | 133.75–135.96 | 776.61–777.05 |

| Viewport | Capture p95 (ms) | Frame age p95 (ms) | Client CPU (ms/write) | Allocated MiB/write | Allocated MiB/s | GC/s | Graphics MiB/write |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 1280×720 | 67.02–67.09 | 121.82–124.73 | 48.96–50.59 | 10.03–10.07 | 159.78–161.76 | 3.13–3.20 | 2.85 |
| 1920×1072 | 79.58–83.48 | 185.36–192.42 | 98.60–101.18 | 21.75–21.89 | 184.15–188.27 | 1.87–1.93 | 6.31–6.32 |
| 2560×1440 | 104.06–110.22 | 281.30–300.74 | 170.81–174.25 | 38.40–38.73 | 186.90–191.04 | 0.93–1.00 | 11.27 |
| 3840×2160 | 156.77–160.37 | 552.88–571.08 | 374.71–377.35 | 85.04–86.79 | 192.76–196.72 | 0.47 | 25.25–25.26 |

## Kitty

| Viewport | Pixels (M) | Writes/s | Capture p50 (ms) | Prepare p50 (ms) | Prepare p95 (ms) | Total CPU (% one core) | Client peak RSS (MiB) |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 1280×720 | 0.92 | 15.27–15.40 | 65.31–65.32 | 0.42–0.51 | 0.61–0.63 | 57.47–57.97 | 86.93–90.89 |
| 1920×1072 | 2.06 | 15.00 | 65.52–65.57 | 0.33–0.48 | 0.83–0.89 | 68.63–74.46 | 98.95–100.95 |
| 2560×1440 | 3.69 | 14.87–14.93 | 65.63–65.81 | 0.75 | 1.08–1.16 | 96.51–101.65 | 105.04–107.02 |
| 3840×2160 | 8.29 | 10.00 | 98.98–99.07 | 1.03–1.04 | 1.54–1.55 | 109.01–109.13 | 157.05–167.12 |

| Viewport | Capture p95 (ms) | Frame age p95 (ms) | Client CPU (ms/write) | Allocated MiB/write | Allocated MiB/s | GC/s | Graphics MiB/write |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 1280×720 | 68.28–68.29 | 69.96–70.04 | 2.58–2.60 | 0.55 | 8.43–8.44 | 0.20 | 0.56 |
| 1920×1072 | 70.31–70.83 | 72.01–73.21 | 2.62–3.11 | 0.81–0.83 | 12.14–12.40 | 0.27 | 0.87 |
| 2560×1440 | 71.58–72.16 | 75.48–75.67 | 4.22–4.24 | 1.11–1.14 | 16.47–17.01 | 0.27–0.33 | 1.21 |
| 3840×2160 | 104.41–110.74 | 109.43–115.24 | 6.60–6.73 | 1.87–2.17 | 18.72–21.67 | 0.27 | 1.91 |

## Validation and interpretation

All 16 reports complete and actual captured dimensions verified. Recorded error/drop/superseded/arena-exhaustion counters: none.
Capture, prepare and output stages overlap; do not add their p50 values or invert their sum to estimate FPS. Total CPU includes client, Node and Chromium; 100% means one core. Peak RSS is client-only, not total memory or steady-state retained heap. The dense animated canvas is a stress workload, not a universal prediction for ordinary web pages.

Evidence: `/tmp/termium-viewport-results`, raw per-workload reports/logs and `conditions.log`; remote `/tmp/termium-viewport-ea-I7TgMm`. Analysis script: `/tmp/termium-viewport-comparison/analyze.py`.
