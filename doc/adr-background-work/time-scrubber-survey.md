---
type: explanation
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Compiled 2026-09-20 as background to
> [ADR-0251](../adr/0251-a-time-strip-for-stepped-series.md), which was
> `proposed` and already built on that date. Nothing here is a decision; §8 is a
> proposal for the ADR to take or leave, and §7 says where the package stood
> when this was written — **not where it stands now**: ADR-0251 took up §8.2
> through §8.6 the same day, so the defects §7 lists are repaired and the
> conventions it calls missing are there. The ADR is authoritative; this page
> is the reasoning behind it.
>
> **Provenance — clean-room.** The page rests on product and API
> documentation, user manuals, help-centre pages, specifications, published
> papers and abstracts, changelogs and release notes, forum threads, and the
> *prose* of public issue and pull-request conversation pages. **No source
> file, repository code view, diff, commit page or package content of any
> surveyed project was read**, and where a permitted page carried code the
> behaviour is described and the code is not. The reading was done on the
> compile date by seven delegated passes — six by area, and a seventh on the
> Rerun viewer alone, the nearest relative and an open-source one, where the
> rule matters most — each held to that rule and each
> required to tag every claim as read on a fetched page, seen only in a search
> snippet, recalled, or inferred. A claim appears below as fact only where a
> pass read it on a page; snippet-only and recalled claims are marked or left
> out, and what is inferred here is marked *(inference)*. The passes found the
> page-fetch tool's summaries unreliable for values — one misreported a
> requirement of an OGC document, caught against locally extracted text — so
> **a number below is a pass's reading of a page only where it sits inside
> quotation marks**. §10 lists the pages and the incidents, one of which is a
> README opened through a repository file view by mistake.

# A time strip for a stepped series — what exists, and what it had to fix

## 1 Question and scope

ADR-0251 put a strip under the flow map: steps at their own instants on a
calendar axis, a bar and a peak per step, each step's load state, a playhead,
a loop range, and a transport whose playback waits for data. It was designed
from what the tree already had — the waveform player's input pattern, the
tick layout — and not from a look at what others ship. The question here is
whether that shows: **which parts of such a control are convention, which are
contested, what the widget lacks that a reader will reach for, and which
defects others have already reported in controls like it.**

Six areas were read: weather, ocean and GIS map viewers (§2.1); the
meteorological image loopers (§2.2); media players and editors (§2.3);
scientific visualisation, log replay and BI tools (§2.4); the interaction and
cartography literature (§4); accessibility standards and design-system
guidance (§5). Two further passes read issue trackers and changelogs for
defects only (§6), and one read everything public about the Rerun viewer's
time panel (§2.5), which is built on the GUI library this widget is drawn
with.

## 2 What the field ships

### 2.1 Map viewers

Consumer weather maps agree on the outline — a strip along the bottom of the
map, a play button at its left, the valid time as text — and say little else
in public. Windy steps by arrow key and plays at one of three named speeds;
when the speed selector was removed from the web client, users objected and
"The option for three independent playback speeds is back". Ventusky steps by
"one forecast period". earth.nullschool has no strip: `j`/`k` step, Shift
makes it "several time steps", `n` goes to now. meteoblue turns the time
readout's background blue "once the radar animation is displaying
forecast-data". RainViewer interpolates at 60 fps by default and offers "Exact
Frames" as the option. DWD's WarnWetter put observation, nowcast, forecast
and trend on one strip of differing resolutions.

The GIS controls are the most configurable and the least forecast-shaped.
ArcGIS's TimeSlider has four modes (instant, window, cumulative from either
end), *stops* that may be an explicit array of dates — its answer to irregular
steps — and one documented sentence of waiting behaviour: "the playback will
pause before advancing if the View is still updating." QGIS's Temporal
Controller added a **source timestamps** step for the same reason. Cesium
splits the job between a shuttle-ring Animation widget and a zoomable
Timeline whose gestures (wheel zoom, middle-drag pan) its own forum describes
as undiscovered. Leaflet.TimeDimension documents the only buffering policy
found among map viewers: a look-ahead `buffer`, per-layer `cacheForward` /
`cacheBackward`, and a **bounded** wait — `loadingTimeout`, "Maximum time in
milliseconds that the component will wait to apply a new time if synced
layers are not ready". kepler.gl draws its range slider over a histogram
("The bars are distribution graphs of all data points by time") with a
selectable measure. NASA Worldview stacks a data-availability bar per layer
above its timeline. ecCharts is the only product found that shows both
forecast time axes: a matrix of base time against valid time, and a message
"If you are not using the very latest base-time".

**Drawing per-step load state on the strip is rare.** Among the map viewers
the passes found it twice, neither on a timeline proper: a whole-loop progress
bar in SSEC's HAniS and shaded cached frames in Metview's frame list. Outside
them it is drawn by the editors (§2.3), by Foxglove (§2.4) and, since its
0.29, by Rerun (§2.5).

### 2.2 The meteorological loopers

IDV, McIDAS-V, AWIPS CAVE and HAniS share a vocabulary the consumer maps
dropped:

- **Dwell.** IDV has four values: forward speed, backward speed, "**First**
  how long first frame is seen. **Last** how long last frame is seen." AWIPS
  bounds first- and last-frame dwell "between zero and 2.5 seconds". HAniS:
  "pause on the last frame of a loop the additional number of milliseconds
  given".
- **Rocking** — forward, backward, or alternating — in all four.
- **Per-frame enable.** IDV draws "A series of small green boxes … The box
  corresponding to the current time is colored blue … Right click on a box to
  disable that frame in the animation. Disabled boxes will be colored red."
  HAniS has the same as `toggle`, and `skip_missing` for frames known absent.
- **What a frame means.** AWIPS load modes decide which model run fills a
  frame ("Valid time seq", "Latest", "Prognosis loop"); IDV's `Reset->To`
  says what the playhead does "when the set of times available changes".
- Stepping stops the loop (AWIPS arrows; IDV First/Last).

### 2.3 Media players and editors

The HTML media element is the standard state model for playback that waits.
`HAVE_FUTURE_DATA` is "data for at least the current frame and the next frame
when the current playback position is at the instant in time between the two
frames" — which is ADR-0251's bracket rule. The specification separates
`waiting` ("the next frame is not available, but the user agent expects that
frame to become available in due course") from `stalled` ("data is
unexpectedly not forthcoming"), and neither is paused. For a drag it
encourages approximate seeks.

Tools name their policy for late data: Blender's Sync is *Play Every Frame*,
*Frame Dropping* or *Sync to Audio*, and the frame-rate readout "will turn red"
when playback cannot keep up; After Effects has "Cache Before Playback" and
"Skip"; mpv pauses when the cache runs dry and resumes only once a set amount
is buffered. Cached extent is drawn **on the ruler** — After Effects (green
for RAM, blue for disk), Blender (opaque, translucent, striped for invalid),
Krita, mpv's seek ranges, Shaka's three-segment seek bar.

Loop regions are where editors agree most. Audacity: "You can drag the in
point, out point, as well as the entire looping region", with *Enable looping*
separate from *Clear Loop*. Ableton: edges resize, "dragging the brace bar
horizontally moves the loop without changing its length", and the up and down
arrows shift it "in steps based on the loop brace length". Logic: grab the
middle to slide, Shift-click moves the nearer edge. Range ends are also set
from the playhead by key — `I`/`O` in Premiere and Unreal, `[`/`]` for
Unreal's playback range, one key cycling A → B → clear in mpv. Unreal and
Unity nest three ranges — playback, selection, view — with a range-slider
scrollbar for the last.

Smaller things several tools share: undo of the last seek (mpv); "play
around" the playhead without defining a range (After Effects, Final Cut,
REAPER); a first Stop returning to where play began (Krita, Logic); follow-
playhead that suspends when the reader scrolls (Ableton) or is turned off
while loop edges are tuned (Audacity's manual gives that reason); markers in
their own row with previous/next keys.

Frame-based tools (Aseprite, Krita) give each frame its own duration and draw
frames by index with the duration as a number. mpv's manual on stepping a
variable-rate video: "framestepping using seeks will probably not work
correctly" — step by index, never by a nominal interval.

### 2.4 Scientific visualisation, log replay, toolkits

ParaView, VisIt and VMD all use the word "VCR" for the transport. End
behaviours line up across tools: VisIt "looping, play once, and swing"; VMD
Loop / Once / Rock; Panel once / loop / reflect; ImageJ "oscillating". Sci-viz
tools cache and **wait** on a cold frame — OVITO: "Actual speed may be slower
if frame loading, computing, or rendering take significant time" — and ImageJ
reports the achieved frame rate in its status bar. napari holds its frame
rate and drops frames; its open issue reports a lazy stack playing
"30-31-32-42-72" and asks for a play-every-frame option.

Index against time is a first-class choice: ParaView shows time and step
index side by side, both editable; OVITO switches the timeline's labels
between them; Rerun switches the whole axis between a sequence and a
timestamp timeline over the same data, with a playback multiplier for one and
frames per second for the other; VisIt documents four ways of putting several
series on one slider. Typed values snap to the nearest step (VisIt, Slicer).

Foxglove shades buffered ranges ("The darker gray sections in the playback
bar"), has range handles with typed fields beside them, and keys that "Set
the range start/end to the current playback time". Rerun has §2.5 to itself.

Toolkits split a slider's value into a live and a settled channel: Bokeh and
Panel `value` against `value_throttled` ("throttled until mouseup"),
vis-timeline `timechange` against `timechanged`, d3-brush `brush` against
`end`, ipywidgets `continuous_update`. Streamlit commits on release only,
because every change re-runs the script.

Dwell at the end of a loop is authorable in PyMOL's frame list and named in
matplotlib's `repeat_delay`; the passes could not read Observable's or
Flourish's pages.

### 2.5 Rerun's time panel

Rerun's viewer is drawn with egui — its issues send input bugs upstream to it,
and its team maintains it *(recalled)* — and its time panel has three and a
half years of public release notes and issue prose behind it. What follows is from its documentation, its API reference prose,
its release pages from 0.2 to 0.38, and the conversation pages of the issues
and pull requests cited; the exact order of its control bar, its full key map
and the scaling of its density graph are on no page that could be read.

**What it is.** A control row — timeline selector, play, pause, step, loop, a
follow toggle, speed and frames-per-second fields, the current time as an
editable field, a help button — over a ruler, over a tree of streams with one
row each. It has a collapsed one-line form that keeps play and pause, a
scrubbable strip, the root density graph, the loop highlight and the load
indicator; the loop highlight was added there because "It's too hard to tell
what's going on right now in the minimized timeline". With one time point the
controls are hidden.

**Rows show presence, not value.** The density graph replaced per-event marks
for cost — "The time panel used to be O(N) over the number of log messages"
— and encodes event counts. Its level-of-detail fallback is, by the team's own
account, an open defect: "we sometimes fall back to rendering a whole chunk as
a line. This is very confusing to users", and zooming in does not bring the
events back because the decision is made per chunk and not per visible range.
An earlier form of the switch depended on pan position, so panning changed how
rows looked.

**Ruler for the range, body for the cursor.** From 2022 the loop selection was
a Shift-drag on the ruler; an open issue records that readers could not find
it ("It is not at all obvious how to input a time selection"). 0.27 changed
it: "Now the top bar is only for making selections, while the bottom panel is
for moving the time cursor." The selection moves by its body and resizes at
its ends; Alt+click or a context menu removes it; it has a hover tooltip; the
selection and the loop mode are separate state, and the mode button "no longer
goes into time selection loop unless there's a time selection". Creating it
lagged the pointer by a frame — "very noticeable at 60 Hz" — until pending
commands were read in the same frame.

**Loaded and not.** Since 0.30 a recording streams on demand — "whatever you
are currently viewing will be downloaded first" — and since 0.29 the panel
says so: "Each time point can have one of three states: no data / unloaded
data / loaded data", with a summary band on top that animates over the union
of unloaded ranges, kept in the collapsed form. The clock takes an
`is_buffering` input — "True if we're waiting for chunks to be downloaded, and
they are expected to come (eventually)" — and 0.34 lists "Always buffer time".
What the team paid for along the way: merely drawing the panel downloaded
chunks ("Make sure the time panel doesn't cause chunk downloads"); hover
changed what was fetched ("Don't buffer & fetch more of entities based on
what's hovered"); evicting what is furthest from the cursor threw away the
newest data of a live stream until it was limited to data that "we can
download … again". Prefetch is keyed on the cursor, the speed it would move at,
and the loop range. Seeks come in two kinds: exact, which "must apply
independent of chunk arrival order" (links, code), and clamped to what is
loaded (drags).

**Playing.** Three states — "Time doesn't move", "Time move steadily", "Follow
the latest available data" — and three loop modes, off, selection and all. No
back-and-forth mode and no dwell. Playback is a continuous clock with a speed
multiplier; frames per second apply to sequence timelines only. At the end it
stops unless more data is expected; the first version of that rule paused
playback "prematurely" while a file was still loading. Following was the
subject of a three-year argument over the space bar: pausing remembered that
it had been following, so a reader who paused to look "and it jumps to the end
of the timeline"; 0.31.3 settled it — "Spacebar toggles play/pause, never
enables following". Any explicit seek leaves following, a rule that was fixed
in 0.6 and found broken again in 0.30.1. A drag pauses playback for its
duration (0.33.1, "Temporary time pause on scrubbing", after 0.30 had removed
pausing on a drag altogether); arrow keys pause it for good (0.32).

**Stepping and keys.** Arrows stepped to the next event until 0.28, which was
too fine on dense timelines, and then by 0.1 s, with Shift 1 s; 0.38 labels
the two kinds apart — "Previous event", "Next event", "Backward 0.1s",
"Forward 1s" — and shows them with their keys in a menu. Shift while dragging
snaps to a grid. Number keys set the speed as a chord, which a reviewer found
turned 2× then 5× into 25×. Arrow keys fight egui's own focus traversal ("The
hack to override eguis built in shortcuts are quite bad").

**Time as text.** The zone is an application preference — UTC, local, seconds
since the epoch, and since 0.26 local with the zone printed, "which makes
displayed timestamp unambiguous". The number of sub-second decimals follows
the zoom and not the value, because "moving the time cursor can cause the
number of decimals to jump, which is very distracting". The time field was
made a plain edit field — "dragging would have made this a lot more
complicated" — and at first could not parse its own text, which omits the date
for today.

**Gap collapsing was removed.** The documentation still describes a "zigzag
cut"; the 0.31.0 release notes list "Remove collapsing time gaps for
performance reasons". While it existed it could not be tuned, its marker was
wider than small gaps, the loop selection could vanish when slid past one,
playback ran "faster than it happened originally", and time-series plots did
not share the cuts.

**Ranges for queries.** A view's visible time range is "strictly speaking just
a query range to the datastore", each end relative to the cursor, absolute or
infinite, and deliberately apart from the view's pan and zoom: "a camera
movement … should not change the query to the store". A view shows its range
on the time panel only while hovered, by re-publishing it every frame.

**Where it is not a model** for a stepped, lazily loaded series: load state is
per range, with no held, loading and missing per step; rows carry no values;
there is no dwell, no back-and-forth, no now line; and on a temporal timeline
there is no rate in steps.

## 3 Agreement and forks

**Agreed nearly everywhere.** First / step back / play-pause / step forward /
last beside a strip. Space toggles; Left and Right are the smallest step; Home
and End the ends; a modifier or PageUp/PageDown enlarges the step. Click on
the track seeks, drag scrubs. A text readout, usually editable. Loop, once and
a back-and-forth mode. Loop regions live on the ruler in their own band, with
edge handles and a draggable body. Hover shows a time.

**Forks.**

| Fork | Sides | Note for a stepped, lazily loaded series |
| --- | --- | --- |
| Unit of speed | fps / steps per second (napari, ImageJ, QGIS, HAniS) · multiplier of data time (Cesium, Rerun, TerriaJS) · ms per step (ArcGIS, Panel) · duration of the whole animation (ParaView's old Real Time) · unitless (VisIt, Tableau, Windy) | Both concrete sides have a documented failure: §6 rows 21 and 22 |
| Axis | true time · by index · switchable | A true axis shows uneven cadence and squeezes the dense block; an index axis gives every step the same target and hides the cadence |
| Late data | wait · drop · cache first · wait with a ceiling | Dropping on lazy data plays out of order (row 13); waiting without a ceiling can hang (row 15) |
| Shift + arrow | coarser (nullschool, MNE, Carbon) · finer (Foxglove, mpv's exact seek) | No convention; PageUp/PageDown is the unambiguous large step |
| Loop by default | on in loopers and sci-viz · off in GIS toolkits | — |
| Displayed zone | viewer's local (Windy, Ventusky) · UTC (Cesium, WMS, the met workstations) | Every tracker read has a bug from mixing the two (rows 9–11) |
| Dragging | live seek · approximate while dragging, exact on release · commit on release | The literature is unambiguous that feedback must be immediate (§4.1) |
| A move during playback | stops it (AWIPS and IDV on a step; the waveform player here) · pauses it for the drag and resumes (Rerun since 0.33.1) · leaves it running (Rerun 0.30) | Rerun tried all three in a year; a step by key stops it everywhere |
| The live edge | a button (Cesium, OpenCPN, this widget) · a third play state (Rerun, video.js) | The state needs rules for entering and leaving it, and Rerun's were wrong twice (rows 50, 51) |
| Empty time | true axis · collapsed (Rerun until 0.31, asciinema clamps) | Rerun removed it for cost after it had broken selection, playback speed and plot agreement |

## 4 The literature

### 4.1 Scrubbing

**Latency.** Matejka, Grossman and Fitzmaurice (Swift, CHI 2012): "even very
small amounts of latency can significantly degrade navigation performance";
overlaying a low-resolution copy during the drag and "snapping back to the
high resolution video when the scrubbing is completed or paused" reduced
"completion times by as much as 72% even with a relatively low latency of
500ms." Their follow-up (Swifter, 2013) names what a fast drag does when
frames outnumber pixels: frames "skip and flash too quickly to be
comprehendible". For a lazily loaded field the transferable rule is that a
drag never waits on a fetch and never blanks the picture *(inference)*.

**Precision.** Ahlberg and Shneiderman's Alphaslider study (24 subjects,
10,000 items) compared four ways of getting fine control; the two with
*predictable* granularity were "significantly faster than the Micrometer and
Acceleration interfaces (p<0.001)". OrthoZoom, the PVslider and iOS's scrubber
all put granularity on the pointer's distance *orthogonal* to the strip.
Ramos and Balakrishnan warn against magnifying targets in motor space: "the
motor location of the targets typically shifts as the targets change size".

**Snapping.** Baudisch et al. (snap-and-go, CHI 2005): stopping a dragged
object at the aligned position "rather than 'warping' them there" made
alignment "up to 138% (1D) … faster". A playhead that snaps only on release
warps.

### 4.2 Values on a control

Willett, Heer and Agrawala (Scented Widgets, InfoVis 2007; 28 participants):
readers "make up to twice as many unique discoveries" with embedded cues in
unfamiliar data. Their guidelines, verbatim: "avoid encoding scent using color
if the application already uses color to display unrelated information";
"Visualizations showing the same scent data should be scaled identically";
"Widgets should include identifiers (icons, tooltips, text, or a legend) that
indicate what the scent cues correspond to."

Heer, Kong and Agrawala (Sizing the Horizon, CHI 2009; 18 and 30 subjects):
"we found a chart height of 24 pixels … to be optimal" for comparing values
on a line chart, with error rising as height halves below it.

Jugel et al. (M4, VLDB 2014): reductions that "disregard the semantics of
visualizations … result in visualization errors". Where marks outnumber pixel
columns, a column keeps its minimum and maximum; it is never subsampled.

### 4.3 Animation and temporal legends

Tversky, Morrison and Bétrancourt (2002): "the research on the efficacy of
animated over static graphics is not encouraging … Judicious use of
interactivity may overcome both these disadvantages." Harrower (2003):
"map-readers become frustrated with maps they cannot control"; looping,
frame-by-frame stepping and speed are his three remedies for "disappearance".
Robertson et al. (2008): "Animation is the least effective form for analysis".
Boyandin et al. (2012): animation yields findings about "changes between
subsequent years", small multiples about longer periods. Griffin et al.
(2006): animated maps beat small multiples for moving clusters, and "pace and
cluster coherence interact" — there is no single right speed.

Harrower and Fabrikant (2008) on temporal legends: a linear bar communicates
"both the current moment … and the relation of that moment to the entire
dataset"; cyclic legends "foster understanding of repeated cycles (e.g.
diurnal or seasonal)". On split attention: "the moment the reader must focus
on the temporal legend they are no longer focusing on the map", with legends
"superimposed onto the map itself" as a proposed, untested remedy. On change
blindness: "observers have great difficulty noticing even large changes …
when blank images are shown in between scenes". DiBiase et al. (1992) name an
animation's variables — "scene duration, rate of change between scenes, and
scene order" — which is the vocabulary for the speed fork in §3: steps per
second fixes duration and lets rate of change vary with cadence.

No paper was found that studies a strip with mixed step lengths, and no
readable training page gave a numeric loop speed or dwell.

## 5 Accessibility and platform guidance

The WAI-ARIA slider pattern: arrows one step, Home and End the ends, "Page Up
(Optional): Increase the slider value by an amount larger than the step
change"; `aria-valuetext` where the raw value "is not user-friendly". The
multi-thumb pattern makes each range end its own slider in the tab order.

WCAG 2.2: **2.5.7 Dragging Movements** — "All functionality that uses a
dragging movement for operation can be achieved by a single pointer without
dragging", and "achieving keyboard equivalence for a dragging operation does
not automatically meet this success criterion." **2.5.8** — targets of "at
least 24 by 24 CSS pixels", a slider counting as one target and an equivalent
control elsewhere being an exception. **1.4.1** — "Color is not used as the
only visual means of conveying information". **1.4.11** — 3:1 contrast for
what identifies a component's state, against *adjacent* colours.

Carbon, Fluent, Apple's HIG and NN/g all pair a slider with an exact-entry
path. On loading: NN/g finds indicators under a second distracting; Apple:
"Avoid vague terms like loading", and on a stall "provide feedback that helps
people understand the problem".

## 6 Bugs others have already paid for

Rows are defects read on a tracker, forum or changelog page, with the
condition that triggers them. The last column is where
[the package](../../public/thestack/imzero2/egui2/widgets/timescrubber/) stood
on the compile date, from reading it; "test" means the behaviour looked right
and nothing pins it.

| # | Defect | Where | Lesson | timescrubber |
| --- | --- | --- | --- | --- |
| 1 | The last time step is dropped from an automatically derived range | QGIS #48942, #40777 | Instants are closed at both ends; a half-open range built from data extents loses the last one | closed indices; test |
| 2 | Exclusive end frame cannot be reached by scrubbing | Unreal Sequencer docs | The last step is reachable by every route | reachable; test each route |
| 3 | Exported frames differ from the on-screen ones | QGIS #42932 | One enumerator of steps for playback, prefetch, keys and export | one today; §8 adds a second (look-ahead) |
| 4 | Region playback ends the instant it begins | wavesurfer #3631 | After a seek *to* a boundary the next sample may be on the wrong side; test membership by index | index-based |
| 5 | Playhead overshoots the range end and stays there; twice as far at double speed | wavesurfer PR #4318 | Clamp on overshoot; express waits in playback time | clamps |
| 6 | Play with the playhead on the range end stops at once, or does nothing | Audacity #5421, Leaflet.TimeDimension #50 | Loop wraps, bounce turns, once restarts | handled; test |
| 7 | Steps closer together than the time type resolves are merged | ParaView Discourse 13744; deck.gl TripsLayer docs | 64-bit or integer time end to end; only the pixel is 32-bit | holds |
| 8 | A step looked up by a formatted float is never found | Plotly forum 19304 | Look up by index | by index |
| 9 | UTC dashboard, tick labels in the host's zone during DST months | Grafana #56799 | The zone is passed to every formatter; none falls back to the process zone | one `Location`; test |
| 10 | Ticks missing, or the app frozen, on a DST day — at some widths only | vis-timeline #1902, vis #2444, d3 #3011 | Step wall-clock fields, cap the loop, test across widths | the tick package has DST tests; none at strip level |
| 11 | Zone shown on the strip differs from the zone of the data, unlabelled | TerriaJS #7143, Windy forum | Say which zone the strip shows | readout prints the zone; hover and playhead labels do not |
| 12 | Two adjacent steps print the same label | QGIS #41206 | Label precision follows step spacing | **defect**: hover and playhead labels stop at the minute |
| 13 | Lazy frames play out of order when the clock is held | napari #2559 | Wait, or skip by a stated rule — never show whatever arrived | waits |
| 14 | A stale response is drawn over the current one after fast scrubbing | Cesium #6156 | Tag requests with a generation and discard old ones | the loader's; out of the widget's reach |
| 15 | A load that neither finishes nor fails hangs playback | Worldview #3688; dash.js #1261 | Every wait has a ceiling or a way out, and says what it waits for | **gap**: waits without limit, readout says only "waiting for data" |
| 16 | The wait-for-data gate never reopens because nothing repaints while waiting | Cesium #8937 | Request repaints while waiting | repaints while playing |
| 17 | Seek flooding: every pointer event issues a seek and none completes | Apple QA1820; mpv #10110 | One fetch in flight plus the newest target; act once per frame | once per frame; coalescing is the loader's |
| 18 | A user seek is clamped to what is loaded | Shaka #10593 | Clamp to what exists | holds |
| 19 | Requested 30 fps, delivered 21.7: a sleep per frame overshoots | napari #8860 | Advance by measured elapsed time | holds |
| 20 | A rate change during play has no effect until restart | Worldview #3615 | Read the rate every tick | holds |
| 21 | Continuous-clock playback shows nothing across gaps in sparse data | TerriaJS #3628 | Stepped data plays by step | plays by step |
| 22 | Constant step rate on a non-uniform bar is reported as varying speed | Windy forum 41863 | Where the axis and the playback unit differ, say so | **gap**: nothing says it |
| 23 | A play control shown twice advances once and stops, or twice | ipywidgets #3967; wavesurfer PR #4362 | Playback state belongs to one owner; advance in one place | elapsed-time advance makes a second render a no-op; test |
| 24 | Minimised window: no updates, then one large elapsed time | egui PR #3877 *(inference from its prose)* | Bound a single advance | bounded at 0.25 s |
| 25 | Looping a one-element range hangs | Foxglove 2.56.0 notes | 0, 1 and 2 steps are test cases | guarded; test |
| 26 | Enabling a loop that already contains the playhead moves the playhead | Tone.js #448 | A range containing the playhead leaves it alone | holds |
| 27 | Range edge dragged while playing: playhead outside the range for seconds | Audacity #1890 | Re-evaluate at once when the range changes | clamps on the next advance; paused it stays outside — undecided |
| 28 | Range handles at one position cannot be separated; handles swap | Base UI PR #1732; Radix #2247; MUI #35971 | Choose the handle by where it can move; state the crossing rule | no handles yet (§8) |
| 29 | Interpolation across the loop seam is meaningless | Godot #86003, #959 | In a loop, jump; do not blend last into first | jumps |
| 30 | Ping-pong skips or repeats a frame at the turn | Aseprite #4271; anime.js 4.5.0 notes | Assert the literal visiting order | test |
| 31 | A double click also fires two clicks | egui discussion #1107 (maintainer: "clicks happen directly"); Dear ImGui #8337 | Make the two handlers compatible, or do not share a region | **defect**: clearing the range by double click also seeks there and stops playback |
| 32 | A click with a pixel of wobble creates a sliver loop region | Audacity #2182 | A range drag shorter than a step is a click | **defect, variant**: a sliver drag on the band *clears* an existing range |
| 33 | Range gesture exists and readers cannot find it | Rerun #7853; Cesium forum 2358 | A second, visible entry point | **gap**: band is the only entry, 13 px, unmarked |
| 34 | Drag anchor drifts from the press origin | egui #5863 | Take the anchor at the press | handled (press origin from the sense region) |
| 35 | Release outside the widget never ends the drag, or presses what is under it | peaks.js #149; Vidstack #1226 | The drag owns the pointer until release | follows the pointer outside; test |
| 36 | Modifier + wheel ignored when the window has no focus | Rerun #7901 | A modifier is never the only route to zoom | no zoom yet |
| 37 | Wheel zoom dies after one large delta; zooming far out throws into the frame loop | Cesium #4425, #13769, #4544; Worldview #2554 | Normalise deltas, hard limits both ways, tick code cannot panic | no zoom yet |
| 38 | Playhead leaves a zoomed view during playback | Cesium #773; Rerun #11260 | Follow policy and an off-screen marker are part of zoom | no zoom yet |
| 39 | Follow-scroll moves the axis under a range edge being dragged | Audacity #3537 | Suspend follow during a drag | no zoom yet |
| 40 | Per-frame cost grows with step count | Plotly #6533; Rerun #5967 | Aggregate to pixel columns; cost follows width | batched, not aggregated; untested at high counts |
| 41 | Changing the step list resets the position | QGIS #39994; Leaflet.TimeDimension #204 | Carry the *instant* across and re-snap; never the index | **defect**: the index is kept |
| 42 | Removing the series during playback leaves the transport dead | napari #3998, #7668 | n → 0 → m is a test case | test |
| 43 | The settled value goes stale after a programmatic move | Panel #2675 | Play, keys and buttons publish the settled value too | no settled channel (§8) |
| 44 | An anti-jitter guard swallows a small explicit seek | Media Chrome #1306 | No smoothing outranks a seek | no smoothing |
| 45 | The live edge shows nulls | Grafana "Now delay" docs | "Now" may need to mean the latest step at or before now | rounds to nearest, which may be a future step — undecided |
| 46 | Sliders not keyboard-operable, values not announced | Leaflet.TimeDimension #211; Flutter #114225 | Playhead and range ends are slider nodes with a time as value text | **gap**: a painted canvas; the row's buttons and readout are the only named nodes |
| 47 | Controls re-announced every frame ("play play play") | video.js #8912 | One node per control; change its state, not its identity | play and pause share an id; label changes |
| 48 | Drawing the strip triggers loads; hovering it changes what is fetched | Rerun PR #12421; 0.31.0 and 0.34.0 notes | Drawing and hovering have no side effects | holds in the widget; a consumer that wires hover to its loader would break it |
| 49 | "Evict what is furthest from the cursor" discards a live stream's newest data | Rerun PR #12363 | Evict only what can be fetched again | the loader's |
| 50 | An explicit seek does not leave following — fixed in 0.6, found broken in 0.30.1 | Rerun #1964, #12687 | Pin the rule with a test | no following state |
| 51 | Pause remembers following, so the space bar jumps to the live edge | Rerun #12699, PR #12722 | Returning to the live edge is its own deliberate action | no following state |
| 52 | Playback pauses by itself after opening: the end was reached while data was still arriving | Rerun PR #2103, #2106 | The end means no more is coming | steps are listed whole; appended steps are undecided (row 41) |
| 53 | Play at the end of data repaints every frame for ever | Rerun #2083 | Stop the clock and the repaints together | once-through stops both; test |
| 54 | A range trails the pointer by a frame while it is created — "very noticeable at 60 Hz" | Rerun PR #11862 | Read this frame's input before drawing | painter-lane reads are a frame behind by construction; not looked at on a 60 Hz host |
| 55 | A quick tap — press and release inside one frame — does not move the cursor | Rerun PR #12476 | Handle both edges in one frame | rests on the host's click flag; test |
| 56 | A dragged quantity drifts: per-frame deltas rounded into integer time | Rerun #12193, #12295; egui PR #7708 | Accumulate from the press origin | absolute pointer position |
| 57 | The readout's decimals flicker as the cursor moves | Rerun PR #11761 | Precision from the zoom or the spacing, never from the value | fixed format; the repair of row 12 takes precision from the series' spacing |
| 58 | The editable time field cannot parse its own text | Rerun PR #11774 | Round-trip test, display to parse | no field yet (§8) |
| 59 | Fields change width on focus and shove the row | Rerun #2857, #8272 | Reserve the widest width | readout moves when controls come and go |
| 60 | Level of detail changes what a mark means, or depends on pan position | Rerun #11432, #7200, #7223 | Decide detail from the visible window; a mark keeps its meaning | none yet; constrains §8 item 21 |
| 61 | Controls "look buggy" with one time point | Rerun PR #7241 | Hide or disable | shown and inert |
| 62 | Shortcuts act while a text field has focus, or while the pointer is in another pane | Rerun #9926, #6638 | Keys need real focus | focus-scoped (ADR-0177) |
| 63 | The compact form gives no sign that a loop is active | Rerun #10823 | A compact form keeps the mode indicators | no compact form (§8) |
| 64 | Panning leaves the data entirely and an empty state replaces the surface | Rerun #12874 | Pan cannot leave the data | no zoom yet |
| 65 | A modifier released before momentum scrolling ends turns zoom into scroll | egui PR #7678 | Latch modifiers when the gesture starts | no zoom yet |
| 66 | A wait for a repaint deadline wakes a frame early and repaints in a loop until it | egui PR #8595 | — | asks for a thirtieth of a second while playing, which is not a deadline wait; relevant if it ever sleeps to the next step |

Not found on any page read, and kept as tests on suspicion alone: hairlines
shimmering at fractional display scales as the playhead moves, labels clipped
at the strip's ends, key repeat issuing a query per repeat.

## 7 Against ADR-0251 and the package

**Confirmed.** Steps at their own instants, playing by step and not by a
continuous clock (row 21). Waiting for both ends of a bracket — it is the
HTML specification's `HAVE_FUTURE_DATA`, and what ParaView, VisIt and OVITO
do; the alternative has an open complaint (row 13). Per-step load state drawn
on the strip: the editors draw cache on the ruler, the map viewers mostly do
not draw it at all. A value per step on the control (§4.2), scaled to the
series and coloured by the map's palette, which is the scented-widgets
guideline on reinforcing a semantic link. Series-wide scaling (Plotly's
documentation asks for fixed ranges across frames). Jumping at the loop seam
(row 29). Elapsed-time advance with a bound (rows 19, 24). A seek stops
playback (AWIPS, IDV). Missing steps do not hold playback (HAniS
`skip_missing`; Rerun buffers only for data "expected to come"). The press
origin decides the gesture (row 34). Keys only under focus (video.js PR #5969
and row 62 are the bugs the alternative produces). A band for the range and
the body for the playhead: Rerun had the range behind Shift-drag for three
years, could not make it findable, and moved to this split in 0.27. A true
axis with no collapsing of empty time, which Rerun built and removed (§2.5).
Absolute pointer positions, not accumulated deltas (row 56). No following
state (rows 50, 51).

**Defects found by reading the package against §6.** Rows 12, 31, 32, 41.
Two more from the reading alone: bars are scaled to the larger of value and
peak, so a series whose peaks run well above its means has its bars in the
bottom of the strip — the defect for which the ADR rejected palette-range
scaling, arriving through the peak; and with `NoSnap`, play from the middle of
a bracket skips the readiness check, which only runs near a step.

**Gaps a reader will meet.** No dwell at either end (§2.2; the last step is
fully shown for one frame before a loop wraps). A range that can be created
and cleared and nothing else (§2.3). Load state told apart by hue where the
bars already use hue for data (§4.2, WCAG 1.4.1). A drag-only range (WCAG
2.5.7). No exact entry (§5). No key for what the bar and the cap are (§4.2).
A wait with no ceiling and no named target (row 15). Nothing to say that the
playhead speeds up across a cadence change because the rate is in steps
(row 22). Nothing a loader can ask for the steps playback will need next,
while the flow layer holds two — so against a database every bracket is a
wait, and the waiting rule that is right in principle is what the reader sees
most of.

## 8 A composed strip

What follows takes the conventions of §3, the evidence of §4–§5 and the
lessons of §6, and leaves out what does not fit a series of tens to hundreds
of lazily loaded steps. It is ordered by how little there is to argue about.

### 8.1 Anatomy

```
 |< First  < Prev  > Play  Next >  Last >| rate v  Loop v | In  Out  Clear | Now |  Tue 2026-03-03 15:00 UTC · step 9 of 61 · +18 h · 3 h step
 ┌──────────────────────────────────────────────────────────────────────────────────────────────┐
 │        ▕▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▏                                   range lane: edges, body, hint   │
 │ Tue 3 ▾15:00          │ Wed 4                │ Thu 5          label row: playhead, day context│
 │ ░░░░░░░│ ┬   ┬                                                bars ≥ 24 px: mean, peak cap,   │
 │ ░past░░│ █ ┬ █ ┬   ┬     ┬                                    alternate-day shading, past     │
 │ ░░░░░░░│ █ █ █ █ ┬ █  ┬  █   ┬    ┬     ┬                     tint left of now, key at right  │
 │ ▮▮▮▮▮▮▮▮▮▮▮▮▯▯▯ ╳ ╷╷╷╷╷╷  ╷   ╷    ╷     ╷                    state lane: shape, not hue      │
 │ 00   06   12   18   00   06   12   18   00                    calendar ruler                  │
 └──────────────────────────────────────────────────────────────────────────────────────────────┘
```

### 8.2 Repairs

1. **The band does not seek.** A click there is not a seek, a drag shorter
   than one step is not a range and does not clear one, and the double click
   stays as a shortcut that now has nothing to collide with (rows 31, 32).
2. **The instant survives a change of steps.** The widget already reads every
   step's instant each frame; when they differ from the previous frame's it
   re-seeks to the nearest step to the instant it was showing (row 41). Every
   consumer gets it, which a fix in play's pane would not give.
3. **Labels as fine as the spacing** (row 12), and the zone on every label
   that can be read alone (row 11).
4. **Load state by shape.** Held: a filled notch. Idle: a short tick. Loading:
   an outlined notch, shown only once a load is a few hundred milliseconds old
   so quick ones do not flicker. Missing: a cross under the baseline. Hue stays
   the data's.
5. **The readiness check runs wherever play starts**, not only near a step.
6. **Controls that do not apply are disabled, not absent**, so the readout
   does not move when "Now" or "Clear range" come and go (row 59); with fewer
   than two steps the whole transport is disabled (row 61).
7. **Bar scale.** To decide: scale to the largest *value* and let a peak that
   exceeds the strip be drawn clipped with a mark, or keep one scale and
   accept short bars. The scented-widgets guideline argues for one scale; the
   24 px finding argues that bars in the bottom third of 51 px are under the
   height at which they can be compared.

### 8.3 Conventions to adopt

8. **Dwell.** One number, in seconds, held on the last step before a loop
   wraps and on both turns of a bounce. AWIPS's bound of 2.5 s is the only
   figure a vendor documents; the default is this repository's to choose.
9. **A range that can be edited.** Edges drag; the body slides; Esc during a
   drag restores what was there. `In` and `Out` set an end at the playhead,
   by button and by key — the non-drag route WCAG 2.5.7 asks for, and the
   second entry point row 33 asks for. Crossing rule: a band drag normalises
   its two ends; an edge drag stops at the other edge (row 28), and the edge
   that was grabbed stays the one being dragged — egui's own two-handled
   slider (PR #8580) states both, and "when the handles coincide, the side the
   pointer is on decides which one moves". The band shows a hint on hover, as
   Rerun's had to (§2.5). With the buttons as the equivalent control the band
   need not be 24 px.
10. **PageUp / PageDown** as the large step, beside Shift + arrow; a modifier
    for the next and previous day boundary. Each control's tooltip says what
    it moves by and its key: Rerun's arrows changed meaning twice before 0.38
    labelled stepping by event and jumping by a span as two things. Up and
    Down stay with focus traversal (egui PR #3641 is the bug the alternative
    produces). No digit keys for the rate: Rerun's chord turned 2× then 5×
    into 25×.
11. **The snap is visible during the drag**: the step a release would select
    is marked, and the readout gives that step's value (§4.1).
12. **A settled position beside the live one**, set on release, click, key
    and button, and at each whole step during play (row 43), so a consumer
    whose work is a query does not write its own debounce.
13. **Look-ahead.** The transport can say which steps playback needs next, in
    play order, through a loop wrap or a bounce turn — one enumerator shared
    with `Advance` (row 3) — so a loader can fetch ahead in the right
    direction. Leaflet.TimeDimension's pair (how many ahead; refill below how
    many) is the documented shape; Rerun keys its prefetch on the cursor, the
    speed it would move at and the loop range, which is this enumerator's
    input exactly. Resuming only when more than one bracket is ready (mpv's
    hysteresis) follows from it. What Rerun learned on the way binds the
    loader, not the widget: drawing and hovering fetch nothing (row 48), and
    eviction by distance from the playhead applies only to what can be fetched
    again (row 49).
14. **Waiting says what for, and for how long.** "waiting for Tue 15:00 · 4 s";
    past a ceiling the readout says so, and what happens then — skip, stop —
    is the caller's, since only the caller can declare a step missing. The
    achieved rate is shown when it falls below the chosen one (Blender,
    ImageJ, OVITO).
15. **Exact entry**: the readout's time is editable and snaps to the nearest
    step (ParaView, VisIt, Rerun, every design system in §5). Rerun's is a
    plain field applied on Enter or loss of focus, chosen over a draggable
    one; it must parse what it prints (row 58).
16. **A key**: "bar mean · cap max" with the value's name, at the end of the
    label row.

### 8.4 For a forecast

17. **Past and future.** A tint left of the now line; the readout gives the
    offset from now ("+18 h"); the Now control shows when the playhead is
    already there (video.js's live button). Which step "Now" means — nearest,
    or latest at or before — is row 45's question.
18. **Alternate-day shading** from the tick layout's day boundaries: the
    cyclic reading Harrower and Fabrikant ask of a linear bar, without needing
    a longitude for day and night.
19. **Cadence.** The readout names the bracket's length ("3 h step"), which
    answers row 22 in words. A time-proportional rate is the alternative that
    keeps DiBiase's rate of change constant; it makes coarse steps slow and
    every dwell uneven, and is left as an option to add if the words do not
    suffice.
20. **Marks** the caller supplies — a model run's start, the end of the
    analysis — drawn in the label row, with previous/next keys.

### 8.5 At scale

21. **A min–max envelope per pixel column** where steps outnumber columns,
    and the worst state per column on the state lane (§4.2, row 40). Row 60
    binds it: the switch depends on width and step count and never on
    position, and a mark keeps its meaning — the bar is still a mean, the
    column's largest, and the cap still a peak.
22. **An index axis as a caller's option**, which the axis type already has
    for steps without increasing times: every step the same width, the cadence
    change marked. It is the cheap answer to a squeezed hourly block, and the
    fork of §3 says neither axis is wrong.
23. **Zoom and pan stay deferred**, with what §6 asks of them recorded: hard
    limits both ways, deltas normalised, a route that needs no modifier, an
    edge marker for an off-screen playhead, follow that yields to the reader
    and is suspended during a drag (rows 36–39), and the literature's
    preference for an overview strip with a draggable window over any
    distortion of the axis.

### 8.6 From Rerun's panel

24. **A compact form** of one line: play and pause, the state lane with the
    playhead and the range on it, the time. ADR-0251 records the height the
    strip takes from the map as a cost, and Rerun's panel has had such a form
    since 2022 — to which it had to add the loop highlight and the load
    indicator after readers lost track of both (row 63).
25. **What a drag does to playback** is a fork (§3). A step by key or button
    stops it everywhere. For a drag ADR-0251 says stop, on the waveform
    player's reasoning; Rerun went from stopping to not pausing at all to
    pausing for the drag's duration. Its clock is continuous, so resuming from
    where the drag let go loses nothing; here a release lands on a step the
    reader chose, and resuming would leave it at once. That argues for keeping
    the ADR's rule, and it is the reader's habit from the other tool that
    argues against.
26. **No side effects from looking.** Hover, the snap preview of item 11 and
    the drawing itself read what the caller handed in and ask for nothing
    (row 48). It holds in the widget; it is worth a sentence in the package's
    contract so that a consumer does not wire hover to its loader.

### 8.7 Left out, and why

Shuttle rings and J/K/L: velocity control over a series with holes stutters.
Pointer-speed gain: slower than predictable steps (§4.1). Orthogonal-drag
granularity and fisheye axes: a few hundred steps do not need them, and the
second breaks the true axis. Dropping late steps (row 13). Playing by
continuous clock (row 21). Hover thumbnails: there is no cheap per-step
picture. Per-step disable (§2.2): cheap, but view state the widget would have
to hold and a second way of saying what a range says. A following state that
moves with the wall clock: a forecast's clock moves a step an hour, and
Rerun's record (rows 50, 51) is what the state costs to get right. Collapsing
empty time: Rerun built it and removed it (§2.5). The
run-by-valid-time matrix: a different widget, for when several runs are held.
A point probe's series on the strip ("when, here?") is the literature's
affordable relative of dragging the feature itself, and a larger piece of
work than anything above.

### 8.8 Accessibility, where it depends on the host

Rows 46 and 47 need the canvas to carry a slider node with a range, a value
and a value text. The GUI library can do it: its documentation gives a custom
widget `WidgetInfo::slider(enabled, value, label)` and, for anything richer,
direct access to the accessibility node; its two-handled slider (PR #8580)
makes "Each handle … its own focus stop" and reports each "bounded by its
neighbour". What is missing is a way for the painter lane to say so, which is
a question for the IDL and not for this package; until it can, the transport
row's named
buttons, the editable readout (item 15) and the `In`/`Out` controls are the
whole keyboard and screen-reader surface, which is the argument for building
them first. The playhead, the now line and the range edges sit over bars of
arbitrary colour, so their 3:1 contrast has to come from an outline and not
from a fixed hue.

## 9 Tests the survey asks for

By row: every route reaches the last step (1, 2); 0, 1 and 2 steps through
every control and mode (25, 42); the literal visiting order of bounce and of a
loop with a dwell (30); play pressed on the range end in each mode (6); a
range set around, and away from, a paused and a playing playhead (26, 27); a
double click, a sliver drag and a press-hold-drag on the band (31, 32); a
drag released outside the strip (35); tick generation across a DST change in
a northern and a southern zone at several widths, at strip level (10); a
display zone that differs from the host's (9); adjacent labels never equal
(12); elapsed times of zero, negative, a millisecond and ten seconds (24); the
widget rendered twice in a frame (23); the steps replaced while playing,
waiting and dragging — the instant is kept (41); a step that stays loading
(15); the look-ahead agreeing with `Advance` over a wrap and a turn (3, 13);
ten thousand steps drawn at a cost that follows the width (40); the settled
position after every kind of move (43); a press and release inside one frame
(55); hover and drawing leaving the caller's load requests unchanged (48);
the editable time parsing every form the readout prints (58); the row's
width constant across every state of its controls (59); the envelope switch
unchanged under a change of position, and a range drag looked at on a 60 Hz
host (60, 54).

## 10 Sources

Pages the passes fetched and relied on for what is stated above, by area.

**Map viewers.** developers.arcgis.com TimeSlider reference ·
doc.esri.com ArcGIS Pro time slider pages · docs.qgis.org map-view manual and
the `QgsTemporalNavigationObject` reference · cesium.com reference pages for
Animation, Timeline, ClockRange and ClockStep · community.cesium.com thread
2358 · apps.socib.es/Leaflet.TimeDimension · docs.kepler.gl playback guide ·
docs.terria.io default-timeline traits · earth.nullschool.net/about.html ·
ventusky.com/help · community.windy.com topics 5, 5296, 7844, 30212, 41863 ·
RainViewer's store release notes · content.meteoblue.com weather-maps help ·
help.predictwind.com forecast-map and forecast-replay articles ·
weather.metoffice.gov.uk forecast guide · dwd.de press release of 2018-11-26 ·
confluence.ecmwf.int ecCharts and Metview pages ·
reading-escience-centre ncWMS user guide · opencpn-manuals GRIB plugin ·
earthdata.nasa.gov Worldview articles and booklet · documentation.dataspace.
copernicus.eu Browser · support.google.com Earth "View a map over time" ·
docs.ogc.org/bp/12-111r1 *(text extracted and checked)* ·
geoserver.readthedocs.io WMS time.

**Loopers.** ssec.wisc.edu/hanis · docs.unidata.ucar.edu IDV Time Animation ·
ssec.wisc.edu McIDAS-V Animation and TimeMatching · unidata.github.io/awips2
D2D perspective · vlab.noaa.gov AWIPS fundamentals.

**Media and editors.** html.spec.whatwg.org media · MDN HTMLMediaElement and
TimeRanges guide · w3.org APG slider, multi-thumb slider and media seek
slider example · support.google.com YouTube shortcuts and chapters ·
mpv.io/manual/stable · Apple support pages for QuickTime Player, Final Cut Pro
and Logic Pro · helpx.adobe.com Premiere shortcuts and After Effects
previewing · docs.blender.org 4.2 timeline and markers ·
docs.unity3d.com Timeline 1.8 playback controls · dev.epicgames.com Sequencer
editor and workflow tips · manual.audacityteam.org timeline, scrubbing ·
ableton.com manual, arrangement view · a third-party text conversion of the
REAPER user guide · aseprite.org docs · docs.krita.org animation timeline ·
legacy.videojs.org live guide · shaka-player-demo UI customisation ·
docs.jwplayer.com preview thumbnails.

**Sci-viz, replay, toolkits.** docs.paraview.org animation (latest, 5.10) ·
VisIt manual, animation basics and database correlations · napari preferences
and NAP-4 · imagej.net user guide §28 · ovito.org animation settings ·
ks.uiuc.edu VMD user guide · pymolwiki Mset · slicer.readthedocs.io sequences ·
mne.tools plot_raw · rerun.io timeline reference, viewer configuration,
timelines concept, 0.27 release post, 0.38 changelog, ref.rerun.io time-panel
blueprint · docs.foxglove.dev playback, plot panel, changelogs 2.49–3.1 ·
perfetto.dev UI · grafana.com dashboards and trace docs · elastic.co Kibana
time filter · help.tableau.com Pages shelf · plotly.com animation pages and
slider reference · vega.github.io signals and event streams · d3js.org
d3-brush · panel.holoviz.org Player · docs.bokeh.org Slider · ipywidgets
widget list and events · docs.streamlit.io select_slider · matplotlib widgets
API · deck.gl TripsLayer · visjs.github.io vis-timeline · docs.asciinema.org
player.

**Rerun and egui.** rerun.io docs: timeline reference, viewer overview,
navigating the viewer, timelines concept, fixed-window plot, limit-ram,
callbacks, time-series view and time-range type references, MCP page ·
rerun.io changesets 0.34–0.38 · blog posts for 0.11, 0.16, 0.26, 0.27 ·
github.com/rerun-io/rerun release pages for every tag from 0.2.0 to 0.38.1 ·
ref.rerun.io Python blueprint, components, notebook and experimental
references · docs.rs item pages (names and doc comments) for the viewer's
time-control, time-view, range-highlight and prefetch-cursor types, and for
egui's `Response`, `Sense`, `WidgetInfo`, `Context`, `EventFilter`, `Memory` ·
issue and pull-request conversation pages of rerun-io/rerun by the numbers
cited in §2.5 and §6, and of emilk/egui: 1271, 2502, 3272, 4776, 4831, 4942,
5136, 5433, 7500, 7678, 7708, 7776, 7804, 8180, 8181, 8285, 8339, 8396, 8580,
8595 · egui release pages.

**Literature.** Full text: Ahlberg & Shneiderman 1994 (UMD report) · Ramos &
Balakrishnan 2003 · Matejka et al. 2013 · Appert & Fekete 2006 · Karrer et al.
2008 · Heer, Kong & Agrawala 2009 · Willett, Heer & Agrawala 2007 · Heer &
Robertson 2007 · Tversky, Morrison & Bétrancourt 2002 · Harrower & Fabrikant
2008 · Harrower 2003. Abstracts, verbatim through the OpenAlex API: Matejka et
al. 2012 · Baudisch et al. 2005 · Jugel et al. 2014 · Robertson et al. 2008 ·
Griffin et al. 2006 · Boyandin et al. 2012 · Archambault et al. 2011 · Fish et
al. 2011 · DiBiase et al. 1992 · Kraak, Edsall & MacEachren 1997.

**Standards and guidance.** w3.org WCAG 2.2 Understanding pages for 1.4.1,
1.4.11, 2.2.2, 2.3.3, 2.5.7, 2.5.8 · Media Accessibility User Requirements ·
WAI media-player page · ableplayer.github.io · nngroup.com on sliders,
skeleton screens and progress indicators · carbondesignsystem.com slider ·
Fluent 2 slider usage · Apple HIG sliders, loading, progress indicators.

**Defects.** Issue, discussion and pull-request conversation pages under
github.com for qgis/QGIS, CesiumGS/cesium, socib/Leaflet.TimeDimension,
keplergl/kepler.gl, TerriaJS/terriajs, nasa-gibs/worldview, napari/napari,
rerun-io/rerun, visjs/vis-timeline, almende/vis, d3/d3 and d3-scale,
grafana/grafana, plotly/plotly.js, holoviz/panel, jupyter-widgets/ipywidgets,
cruise-automation/webviz, facontidavide/PlotJuggler, katspaugh/wavesurfer.js,
video-dev/hls.js, Dash-Industry-Forum/dash.js, shaka-project/shaka-player,
videojs/video.js, muxinc/media-chrome, vidstack/player, cookpete/react-player,
bbc/peaks.js, mpv-player/mpv, audacity/audacity, godotengine/godot,
aseprite/aseprite, airbnb/lottie-web, Tonejs/Tone.js, emilk/egui (issues,
discussions, release notes), ocornut/imgui, radix-ui/primitives,
mui/material-ui and base-ui, flutter/flutter — by the numbers cited in §6 ·
discourse.paraview.org threads 190, 3939, 13744 · community.plotly.com 19304 ·
gsap.com forum 21905 · forum.qt.io 144891 · Apple Technical Q&A QA1820 ·
anime.js and Plyr release notes.

**Incidents.** One pass opened the Leaflet.TimeDimension README through a
repository file view by mistake; it is prose, the pass reports that none of
its claims rest on it, and the two options it knew only from a search
snippet of that README are not used here. One pass read the ARIA media-seek
*example* page, which embeds the example's own source; the key table and the
value-text note are what was taken. Several issue pages quote their project's
internal code and the fetch summaries echoed fragments; none is reproduced.
Pull requests were read at their conversation tab for prose only; the Rerun
pass fetched those pages whole and stripped code and table blocks locally
before reading, so review hunks were never displayed to it, and deleted the
raw pages afterwards. File paths in review-thread headers, commit titles, and
internal names mentioned in issue prose were visible to it and are not
reproduced. For Rerun releases from 0.30 on, the notes cite commits and not
pull requests, so a release-note line is all there is — which is where its
lazy loading was built. Two of Rerun's own pages disagree with its release
notes (gap collapsing; viewing more than fits in memory); §2.5 follows the
release notes.
Unreachable on the compile date, and so absent: Esri's community and release
pages (ArcGIS TimeSlider defects), ParaView's GitLab tracker, Blender's
tracker, Observable's Scrubber page, Material Design's slider page, the
DaVinci Resolve manual. No documentation was found for zoom.earth, Windfinder
or SailGrib.
