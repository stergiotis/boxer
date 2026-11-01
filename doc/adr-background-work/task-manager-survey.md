---
type: explanation
audience: whoever extends the app center's Running tab (ADR-0348)
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.
> Compiled 2026-10-31; nothing here is a decision. **[V]**: the primary page
> was read that day. Everything else came from search snippets, press
> coverage or memory, and these products change yearly — re-check before
> relying on a row.

# Task managers — what they show and what they let a person do

The Running tab
([ADR-0348](../adr/0348-a-running-page-in-the-app-center-open-windows-what-they-cost-raise-and-close.md))
lists a host's windows, their share of the render loop and the tasks they
own, and closes what a filter shows. This page asks what other task managers
offer beyond that. The closest analogues are the ones inside an application
— browsers, IDEs, language runtimes — because, like boxer, they attribute
cost to units that share one process.

## 1. The systems

| System | A row is | Measures | Softer than ending | Alerts | Diagnose from the row |
| --- | --- | --- | --- | --- | --- |
| Windows Task Manager | app, expands to processes | CPU, memory, disk, net, GPU | efficiency mode (per process only) | — | — |
| macOS Activity Monitor | process | CPU, memory, energy impact, 12 h power | App Nap (by the OS) | — | sample |
| htop | process, optional tree | CPU, memory, I/O | renice | — | — |
| Android 13 "Active apps" | app with a foreground service | presence only | — | long-running service notice | — |
| Chrome | tab, extension, subframe | memory, CPU, net | Memory Saver discards idle tabs | performance issue alerts | — |
| Firefox about:processes | process, tabs beneath | memory, CPU, threads | — | — | profile N seconds |
| VS Code | extension | activation time, CPU | disable | unresponsive extension host [V] | auto-profile, names the extension [V] |
| JetBrains Activity Monitor | subsystem, plugin | CPU share | — | — | — |
| Eclipse Progress view | job | progress | — | — | — |
| Erlang Observer | actor | reductions, memory, queue length | — | — | trace |
| tokio-console | async task | busy, scheduled, idle, polls | — | lint warnings (lost waker) | — |
| Unreal `stat unit` | thread stage | ms per frame | — | — | Unreal Insights |
| **boxer Running tab** | window, its tasks beneath | Frame p50/p95, messages, tasks, oldest task | — | — | — |

A few details that matter for boxer:

- **Ending.** Android's Stop ends the whole app with no callback; the app
  reads `REASON_USER_REQUESTED` on its next start [V]. Eclipse's cancel is
  a request with "no guarantee" the job stops. Erlang's `kill` cannot be
  refused. Chrome lets a person end the browser process itself.
- **Exemptions.** Android shows core apps (dialer, camera) without Stop;
  Windows greys efficiency mode out on system processes.
- **Bulk.** htop filters (`F4`), tags (`Space`) and signals every tagged
  process (`F9`) — the general form of boxer's filter plus Close all.
- **Hiding.** Eclipse hides "system" jobs; GNOME's background-apps list
  shows only apps without a window.
- **The observer's own cost.** Erlang advises observing from another node;
  Pharo's process browser shows stale stacks under a runaway process.

## 2. Patterns

1. **The row is what a person acts on** — a tab, not a renderer process.
2. **Relative figures** — energy impact has no unit; Unreal's rule is "the
   largest stage is the bottleneck". People look for *which one*, not *how
   much*.
3. **Something softer than ending** — throttle, nap, discard, sleep. Browsers
   apply it automatically to what is out of view, with an exclusion list.
4. **An alert that leads to the manager** — Chrome, Android and VS Code name
   the culprit unprompted and open the remedy.
5. **Figures where the unit already is** — Chrome's tab hover card shows
   memory; JetBrains puts background tasks in the status bar.
6. **"Why" from the row** — Firefox and VS Code profile the one unit in
   question.

## 3. Where the Running tab stands (2026-10-31)

It has patterns 1 and 2, htop's filter-and-act, and a cooperative close that
records its reason (closer to Android than to a kill). It lacks 3–6 and any
history. Two gaps come from sharing one process:

- **Background-task CPU is unattributed.** The table measures each window's
  `Frame`; tasks run on other goroutines. No code in the tree sets pprof
  labels. Labels set with `pprof.Do` carry over to child goroutines and
  appear in CPU profiles, so labelling a window's work would split a
  profile by window.
- **Memory is not attributable.** All windows share one Go heap, and heap
  profiles carry no labels. None of the in-process managers above report
  memory per unit either, except Erlang, whose actors have separate heaps.

## 4. Options, not weighed

| | Option | Cost |
| --- | --- | --- |
| O1 | Label each window's `Frame` and tasks; split a sampled CPU profile by window via [pprof-profiles-as-data](./pprof-profiles-as-data.md) | a label per task start; statistical |
| O2 | "Profile this window for N seconds" | needs O1 |
| O3 | Notice when a window's p95 stays high, linking to the filtered tab | a threshold that does not cry wolf |
| O4 | Draw collapsed or covered windows less often | apps whose `Frame` does work slow too; needs exclusions. Not checked whether collapsed windows run `Frame` today |
| O5 | Show a window's cost in its own chrome | screen space; noisy unless smoothed |
| O6 | Record frame-time quantiles in `boxer.facts` for history | write volume, a retention choice |

## Sources

Read [V]:
[Android FGS Task Manager](https://developer.android.com/about/versions/13/changes/fgs-manager) ·
[VS Code: extension causes high CPU load](https://github.com/microsoft/vscode/wiki/Explain-extension-causes-high-cpu-load)

Snippets and secondary sources:
[AOSP Task Manager](https://source.android.com/docs/core/display/task-manager) ·
[Activity Monitor energy](https://support.apple.com/en-us/ht203184) ·
[htop(1)](https://man7.org/linux/man-pages/man1/htop.1.html) ·
[Process Explorer](https://en.wikipedia.org/wiki/Process_Explorer) ·
[Windows efficiency mode](https://maketecheasier.com/what-is-efficiency-mode-windows) ·
[GNOME 44 background apps](https://www.omglinux.com/gnome-shell-background-apps-ui/) ·
[Chrome Task Manager](https://maketecheasier.com/use-chrome-task-manager-windows-macos/) ·
[Chrome tab memory](https://9to5google.com/2023/11/07/chrome-tab-memory-usage/) ·
[Chrome performance alerts](https://9to5google.com/2024/10/29/google-chrome-performance-issue-alerts/) ·
[Firefox Task Manager](https://support.mozilla.org/en-US/kb/task-manager-tabs-or-extensions-are-slowing-firefox) ·
[Firefox about:processes strings](https://searchfox.org/mozilla-central/source/toolkit/locales/en-US/toolkit/about/aboutProcesses.ftl) ·
[Edge performance features](https://support.microsoft.com/en-gb/topic/learn-about-performance-features-in-microsoft-edge-7b36f363-2119-448a-8de6-375cfd88ab25) ·
[JetBrains Activity Monitor](https://jetbrains.com/help/idea/activity-monitor.html) ·
[Eclipse Jobs API](https://eclipse.org/articles/Article-Concurrency/jobs-api.html) ·
[Erlang Observer](https://www.erlang.org/doc/apps/observer/observer_ug) ·
[tokio-console warnings](https://docs.rs/crate/tokio-console/latest/source/src/warnings.rs) ·
[Pharo Process Browser](https://eng.libretexts.org/Bookshelves/Computer_Science/Programming_Languages/Book%3A_Pharo_by_Example_5.0_(Ducasse_Zagidulin_Hess_and_Chloupis)/07%3A_Some_of_the_Key_Tools_of_the_Pharo_Environment/7.05%3A_The_Process_Browser) ·
[Unreal stat unit](https://docs.aws.amazon.com/gameliftstreams/latest/developerguide/profiling-ue.html)

From memory, not looked up: Erlang per-actor heaps and untrappable `kill`;
`pprof.Do` label inheritance; heap profiles without labels; Activity
Monitor's sample action.
