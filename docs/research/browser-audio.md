# Browser audio feasibility — 2026-10-01

Sound is feasible without moving audio through Go, gRPC or the terminal graphics
protocol when Chromium runs on the user's machine. Chromium can use the host's
normal audio output. This investigation does not enable audio in Termium yet.

## Source evidence

The installed Puppeteer launcher adds `--mute-audio` for headless launches.
Termium launches `headless: true` without filtering that default. Puppeteer's
[official launch documentation](https://pptr.dev/next/api/puppeteer.puppeteernode.launch)
explicitly demonstrates `ignoreDefaultArgs: ['--mute-audio']`.

For an eventual implementation, remove only that default flag and provide an
explicit mute control. Preserve normal autoplay and user-gesture requirements;
there is no need to grant microphone access or disable autoplay protections.
Audio playback is separate from audio capture.

[WSLg](https://github.com/microsoft/wslg) provides an audio server for Linux apps.
An SSH session does not imply audio forwarding: unmuting a browser on a remote
server would play on that server's configured output. Remote sound would need a
separate audio transport and client playback, not a Sixel/Kitty extension.

## Local proof

Host: harpe, Linux under WSL, PulseAudio server `unix:/mnt/wslg/PulseServer`.
A private null sink was created for this test; the browser's `PULSE_SINK` selected
it. Only that sink's monitor was recorded. No default device was changed and no
sound was played through the user's speakers. The sink was unloaded afterward.

Two fresh headless Chromium launches used identical pages. A real CDP-dispatched
button click started a low-gain Web Audio oscillator; AudioContext reported
`running` in both. Raw mono PCM was observed through `parec` at 48 kHz, signed
16-bit little endian. Each run waited 1.8 seconds after the click; recording also
included startup/shutdown, so sample totals are not a latency measurement.

| Launch | Recorded samples | Nonzero samples | Peak absolute PCM amplitude |
|---|---:|---:|---:|
| Default Puppeteer mute | 38,841 | 0 | 0 |
| Remove `--mute-audio` | 38,795 | 4,452 | 1,639 |

This proves audio reaches a host audio sink here. It does not verify physical
speakers, codecs on every site, native macOS output, synchronization with Termium's
rendered frames, or remote playback. Before enabling by default, test those local
platform paths, playback/pause across tabs, browser shutdown, and mute behavior.
No performance claim is made.

Probe and raw log: `/tmp/termium-audio-20261001/{probe.cjs,result.log}` on harpe.
The probe uses only synthetic audio and a dedicated sink.
