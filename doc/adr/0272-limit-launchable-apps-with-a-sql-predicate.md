---
type: adr
status: accepted
date: 2026-10-02
reviewed-by: "p@stergiotis"
reviewed-date: 2026-10-02
---

# ADR-0272: Limit the launchable apps with a SQL predicate

## Context

A host process can open a window of any app it links. An operator who
hands a host to someone, or lets a chat model ask for windows
([ADR-0269](./0269-app-operations-a-command-query-contract-agents-drive-under-a-task-grant.md)),
may want only some of those apps to open.

Windows are opened along several paths: the launcher and its menu, F1 for
help, launch requests from other apps
([ADR-0135](./0135-app-launch-requests.md)), task grants, and boot seeds.
`keelson.apps` already describes every registered app in columns that a
predicate can name: `id`, `kind`, `topics`, `keywords`, `caps` and so on
([ADR-0158](./0158-app-classification-topics-keywords-kind.md) §SD8).

## Decision

### SD1 — The limit is a `WHERE` over `keelson.apps`

`KEELSON_LAUNCHABLE_APPS_WHERE` holds a ClickHouse boolean expression over
the columns of `keelson.apps`, for example:

```sh
KEELSON_LAUNCHABLE_APPS_WHERE="kind = 'app' AND (shell OR has(topics, 'data'))"
```

When it is unset, every app can be launched.

### SD2 — Evaluated once, at boot

Hostboot runs `SELECT id FROM apps WHERE (<expr>)` on clickhouse-local
before the first window opens. An app that is registered at that point and
not selected becomes unlaunchable. **An app registered later, such as an
applet from the applet store, can be launched.** The limit covers the apps
the process booted with. It is not a rule applied to each new registration.

When the variable is unset, nothing is evaluated and clickhouse-local is
not used. When it is set and cannot be evaluated (clickhouse-local is
missing, the expression doesn't parse, the query fails), the host does not
boot. Running without a limit the operator asked for would be the worse
failure.

### SD3 — Enforced at one point, honoured at the others

`windowhost.Inst.OpenWithConfig` is the path every open takes, and it
refuses an unlaunchable app with a named error. Launch requests and agent
launches report that refusal to their caller. Screenshot mode does not go
through the window host, so it checks its `--launch` apps itself.

The surfaces that offer apps do not show unlaunchable ones: the launcher's
list and menu, its Help and Inspect actions, the agent's `describe`, and
the apps a task grant can name. These are for consistency. The boundary is
the window host.

### SD4 — Shell apps are labelled, not exempt

The launcher and help are opened by the host's own chrome. They carry
`Manifest.Shell`, which shows up as the `shell` column, and the limit
treats them like any other app. An operator who wants to keep them writes
`... OR shell`. A `launchable` column shows the effect of the limit with an
ordinary query.

## Consequences

- A limit that leaves out the launcher removes its window and its menu
  entry. The launcher still shows as the empty-state pane. Pressing F1
  without help logs the refusal.
- `keelson.apps` changes from static to live freshness, because
  `launchable` changes when the limit is set. Snapshots are no longer
  cached across queries.
- The expression is spliced into the statement. It has the trust of any
  other environment variable the process reads.

## Rejected options

- **Evaluate on each launch.** That would put a SQL round trip on the open
  path, which the render goroutine and agent calls wait on. Evaluating once
  covers the apps known at boot. That is the set the operator can see when
  writing the expression.
- **A list of ids.** A list cannot say "no demos" or "only data apps", and
  it has to change whenever an app is renamed.
- **Exempt the shell apps.** That would hide a policy inside the host that
  the operator can't see or change. A label keeps the choice in the
  expression.
