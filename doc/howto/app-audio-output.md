---
type: how-to
audience: engineer writing an app that plays sound
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# How to give an app an audio output

An app asks the runtime for an output and plays through whatever the host
has behind it: the machine's sound server on a desktop, the page of the
active viewer in a remote session, the page around a browser tab
([ADR-0372](../adr/0372-audio-output-for-the-browser-hosts-a-host-provided-sink-and-a-side-lane.md)).
This covers the app's side. It does not cover decoding, the waveform widget
or the audio subtree, which are
[ADR-0208](../adr/0208-audio-waveform-player-widget.md)'s.

## When to use this recipe

Your app has a `pcm.SourceI` — a decoded recording, a synthetic signal — and
wants it heard. A tool with no runtime around it, a batch job or a test,
opens `pulsesink` directly or uses `sink.Null`; this recipe is for an app
with a manifest and a bus client.

## Prerequisites

- The app's bus client, `ctx.Bus()` at `Mount`.
- A host that serves the subject: `hostboot.Services.AudioOut` on the native
  hosts (`AllServices()` sets it), `tabhost.Services.AudioOut` in a tab
  binary. Without it the app still runs, on a silent clock.

## Steps

1. **Declare the subject in the manifest.** The bus refuses the request of
   an app that did not, and the tab report lists the subject for a tab that
   does not serve it.

   ```go
   Caps: []app.SubjectFilter{{
       Pattern:   audioout.SubjectOpen,
       Direction: app.CapDirectionPub,
       Reason:    "play the recording in the preview",
   }},
   ```

2. **Open the output off the render path.** `audioout.Open` waits on the bus
   and the device may talk to a sound server. It always returns a sink you
   own and close; `status.Reason` says why it is the device-less one when it
   is.

   ```go
   out, status := audioout.Open(bus, src)
   if !status.Audible() {
       deviceNote = "no output device: " + status.Reason
   }
   ```

   With a `track`, open it in `track.Options.NewSink`, or swap it in later
   with `track.ReplaceSink`.

3. **Drive it through `sink.SinkI`.** `Play`, `Pause`, `SeekFrame`,
   `SetRate`, `SetVolume`, and `Position` once per frame for the playhead.
   None of them waits on a device.

4. **Say when nothing is heard.** An output played by a viewer's page can be
   silent while its sink plays: nobody is connected, or the browser has not
   let the page make sound yet, which it does once the person clicks or
   presses a key there. The playhead moves all the same. Ask each frame and
   show the reason:

   ```go
   if yes, reason := audioout.Audible(out); !yes {
       c.Label("silent: " + reason).Send()
   }
   ```

## Verification

On a desktop with a sound server, the app plays. To check the remote and
tab routes without clicking, the widget gallery's waveform demo opens the
output and plays at mount when `BOXER_WAVEFORM_DEMO_AUTOPLAY=1` is set; a
browser started with `--autoplay-policy=no-user-gesture-required` then
plays it with no input.

## Troubleshooting

- **Symptom:** the app shows "no output device" on a host with sound.
  **Cause:** the manifest does not declare `audio.out.open`, or the host
  does not run the service.
  **Fix:** add the capability; check `Services.AudioOut` in the host binary.
- **Symptom:** a remote viewer hears nothing and the app says the browser
  has not enabled sound.
  **Cause:** the browser's autoplay rule.
  **Fix:** click or press a key in the page.
- **Symptom:** in Safari the first press of play is silent and the second
  plays.
  **Cause:** the page starts its audio when the app's request arrives,
  which is after the press; that browser honours a start only inside the
  press's own handler.
  **Fix:** none on the app's side; the next input starts it.
- **Symptom:** a scene run is silent on a machine with sound.
  **Cause:** a scene's launcher configures a carrier, so the output is the
  lane, and no viewer listens.
  **Fix:** none needed: the playhead moves and the readouts hold. To hear
  it, open the scene's viewer page.
- **Symptom:** a second viewer hears nothing.
  **Cause:** the audio device belongs to the active viewer only.
  **Fix:** take the session on that viewer.
- **Symptom:** the tab freezes when a recording plays.
  **Cause:** in a tab the source is read inside the worker's export, and it
  waited on something only another export can deliver.
  **Fix:** play from memory in a tab; hold the recording's bytes before
  opening the output.
