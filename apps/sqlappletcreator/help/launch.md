---
type: reference
audience: end-user
status: draft
title: Opening the creator from another app
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Opening the creator from another app

The creator is opened over `windowhost.open` (ADR-0135 §SD7) with a
launch config of kind `appletCreate`, targeting the app id
`github.com/stergiotis/boxer/apps/sqlappletcreator`. The contract is the
leaf package `apps/sqlappletcreator/appletcreatecfg`, which a caller
imports without importing the creator; play's **Save as applet…** button
is the caller that exists.

## The config

| Field | Meaning |
| --- | --- |
| `Sql` | the buffer the editor opens with; empty is valid and gives an empty editor |
| `Endpoint` | the endpoint the buffer was authored against: empty or `default` for the configured ClickHouse target, `introspection` for the in-process keelson endpoint |

Slug, title, summary and icon are not carried: they are entered in the
window. `Endpoint` is part of the buffer's reproduction context (ADR-0132
§SD1): a buffer authored against the introspection endpoint, where a bare
`keelson('…')` is the dialect, must reopen there, so that value alone
stamps `endpoint: introspection` into the composed document, and the form
says so under the editor.

Each open is a fresh window (factory dispatch), so several buffers can be
authored side by side. The host validates the claimed kind at the
boundary; a config that then fails to decode fails the mount rather than
opening an empty editor, because that is a defect and not a plain open.

## What an agent can and cannot do

There is no operations catalog on the creator, so nothing fills the slug
or presses Save on an open window; the person does both. An agent that
wants a buffer saved as an applet asks for the window to be opened on that
buffer through the launch config and leaves the rest to the person. The
store's refusal, when it comes, is the store's and is shown verbatim in
the status line.
