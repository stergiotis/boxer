---
type: reference
audience: contributor
status: draft
scene:
  launch: tally
  size: 1400x1000
  requires: [clickhouse, "table:boxer.fssnap"]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# tally — the audio preview

ADR-0200's audio preview over ADR-0208's player: select a recording in a
snapshot and get a waveform and a transport. The two staging shapes are the
point, so the scene asserts both — a compressed file goes to ffmpeg through an
inherited descriptor onto anonymous memory, a WAV is sealed into the ad-hoc
dataset store and read back in-process — and then that browsing away releases
whichever was open. The readouts are what is asserted: the waveform itself is
painter output.

It needs a mount named `tally-audio` holding the recordings the trace names.
`scripts/dev/tally-audio-scene.sh --fixture` synthesises them and takes the
snapshot; the same script runs this document and then checks what no step can
see — that nothing is left in the staging directory it pointed
`BOXER_ADHOC_DIR` at.

```jsonl trace
{"do":"note","text":"tally's audio preview — a recording read out of a lading snapshot"}
{"do":"wait","value":"Mounts","role":"label","settleMs":500}
{"do":"wait","contains":"tally-audio  ·","role":"button","settleMs":1000}
{"do":"click","name":"A","role":"button","settleMs":200}
{"do":"click","contains":"tally-audio  ·","role":"button","settleMs":800}
{"do":"wait","name":"Follow latest","settleMs":600}
{"do":"note","text":"--- a flac: staged into anonymous memory, probed and decoded by ffmpeg over inherited descriptors ---"}
{"do":"click","value":"interview-take-2.flac","role":"label","pointer":true,"nth":0,"settleMs":600}
{"do":"wait","valueContains":"interview-take-2.flac  ·","role":"label","settleMs":10000,"comment":"the preview header; staging reads the whole recording out of the store"}
{"do":"wait","contains":"Play","role":"button","settleMs":8000,"comment":"the transport is up, so the track opened"}
{"do":"wait","valueContains":"2 ch · 48000 Hz","role":"label","settleMs":8000,"comment":"ffprobe agreed with the recording"}
{"do":"wait","valueContains":"· ffmpeg","role":"label","settleMs":2000,"comment":"and it took the external decoder"}
{"do":"note","text":"--- play: the playhead moves, through the device or the silent clock ---"}
{"do":"click","contains":"Play","role":"button","settleMs":1500}
{"do":"wait","valueContains":"· playing ·","role":"label","settleMs":3000}
{"do":"click","contains":"Pause","role":"button","settleMs":500}
{"do":"wait","valueContains":"· paused ·","role":"label","settleMs":3000}
{"do":"note","text":"--- the WAV: sealed into the ad-hoc dataset store, decoded in-process ---"}
{"do":"click","value":"interview-take-1.wav","role":"label","pointer":true,"nth":0,"settleMs":800}
{"do":"wait","valueContains":"interview-take-1.wav  ·","role":"label","settleMs":10000}
{"do":"wait","valueContains":"· wav","role":"label","settleMs":8000,"comment":"the native reader over the sealed staging file"}
{"do":"wait","valueContains":"2 ch · 48000 Hz","role":"label","settleMs":4000}
{"do":"click","name":"Fit","role":"button","settleMs":400}
{"do":"note","text":"--- browsing away releases the recording: the lane owns it ---"}
{"do":"click","value":"notes.md","role":"label","pointer":true,"nth":0,"settleMs":1200}
{"do":"wait","valueContains":"notes.md  ·","role":"label","settleMs":4000}
```
