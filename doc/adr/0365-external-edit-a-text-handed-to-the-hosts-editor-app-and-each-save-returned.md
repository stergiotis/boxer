---
type: adr
status: accepted
date: 2026-11-09
reviewed-by: "p@stergiotis"
reviewed-date: 2026-11-12
---

# ADR-0365: External edit — a text handed to the host's editor app, each save returned

## Context

A window that holds a text a person writes (play's SQL buffer, the chat's
draft, a downstream app's query editor) offers the editor the window draws.
A person who works in a terminal editor wants that editor for a longer
text, and wants the result back in the window without copying.

A downstream host built this as a host service plus a terminal app that
runs the editor in a sandbox: the app offers the text, the service keeps
it as a file in a folder of its own and watches it, the terminal learns
where the file is, and every save returns to the window that offered it.
The subjects and wire types lived in that host, so a boxer app could not
declare the capabilities to use it: the bus admits only declared subjects,
and a boxer app cannot name a downstream package. Moving the contract here
lets boxer's own apps offer the gesture while the service and the editor
stay the host's.

## Decision

### SD1 — The contract is boxer's; the service and the editor are the host's

`public/keelson/runtime/externaledit` declares the subjects
(`externaledit.offer`, `.resolve`, `.wait`, `.close`), the wire types, the
bounds (4 MiB a text, 30 s a held wait), the client calls and two
capability helpers: `ClientCaps` for an app that hands texts over (offer,
wait, close and the window open) and `EditorCaps` for the editor (resolve).
The service that keeps the file and the app that edits it are not in
boxer; a host without them shows no gesture.

### SD2 — The editor is opened by launch kind

`externaledit/launchcfg` declares the kind `externalEditLaunch`, carrying
the edit's id. The offering app opens it with `windowhost.RequestOpenKind`
([ADR-0361](./0361-the-playground-is-a-launch-kind-the-runtime-owns-and-the-host-resolves.md) §SD2), so the host decides which app
edits; an app whose manifest declares the kind is an editor. `Editor()`
names the one editor the host registers (or designates), and the gesture
is offered only when there is exactly one.

### SD3 — The offering window owns the edit; the person's own edit wins

Only the window that offered a text receives its saves and may end it; only
an editor learns where the file is. `Editing` holds one window's edit on
the render goroutine with the bus calls off it. A save replaces the
window's text only while the text is still what was handed over or last
taken; otherwise it waits, and the person takes it or keeps their own. A
save carries the bytes, their digest, the session that wrote it and
whether an agent could type there; it is the file's content after a write,
not proof of who typed it.

### SD4 — One gesture, drawn by one widget

`widgets/editexternally` draws *Edit in <editor>*, then while the text is
handed over its status, *Stop editing* and the take-or-keep choice. play's
Editor toolbar and the chat's composer use it; a window whose text is
bound to an input overrides the binding with a taken save.

## Alternatives

- **A Go hook play and chat call, served on the host's own bus.** Fewer
  boxer changes, but the offer would be attributed to the host instead of
  the app, and the app would reach subjects it never declared: the
  governance the capability check exists for.
- **The editor named by the offering app.** It ties every app to one
  editor and repeats what launch-kind designation already does.

## Consequences

- play declares three more capabilities, the chat three. An app that wants
  the gesture adds `ClientCaps` and one `editexternally.Render` call.
- A host that registers no editor app sees nothing new.
- The membership `extEditLaunchId` joins the keelson vocabulary.

## Deferred

- The service itself in boxer, for hosts that want the gesture without
  writing one — trigger: a second host that needs it.
- An operation for agents to hand a text over — trigger: an agent workflow
  that needs a person to edit a text it drafted.

## Status

Accepted 2026-11-12.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers.

## References

- [ADR-0135](./0135-app-launch-requests.md) — launch requests and configs.
- [ADR-0361](./0361-the-playground-is-a-launch-kind-the-runtime-owns-and-the-host-resolves.md) — opening by launch kind and designation.
- [ADR-0026](./0026-app-runtime-and-capability-subjects.md) — capability subjects.
