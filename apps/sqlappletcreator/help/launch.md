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

## Seeding a window without a caller

A scene or a demo that has no app to send the launch config sets
`BOXER_SQLAPPLETCREATOR_DRAFT` to a JSON object: `Sql` and `Endpoint` as
the launch config carries them, which take the launch config's own path,
and `Slug`, `Title`, `Summary` and `Icon`, which fill the four fields. A
caller's launch config wins over it. A value that is not that object fails
the mount, as a config that does not decode does.

## What an agent can and cannot do

An agent can open the window on a buffer through the launch config, and,
holding a grant for the window, fill and save the draft through the
catalog on the **Authoring a SQL applet** page: `set_field` for the slug,
title, summary and icon, `set_buffer` for the SQL, `save` to send it.
Export stays the person's. The store's refusal, when it comes, is the
store's: it is shown verbatim in the status line and is what `save`
settles with.
