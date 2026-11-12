---
type: how-to
audience: someone who opens the same windows side by side, or a developer committing such a set to a book
status: draft
# reviewed-by: "@<handle>"   # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD  # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# How to save a desktop as a workstatelet, and commit one to a book

A workstatelet is a markdown document that lists windows to open and the
desktop layout they take. This page covers saving one from the open desktop,
opening it, and turning a saved one into a committed document. Why it is
shaped this way is
[ADR-0369](../adr/0369-workstatelets-a-desktops-windows-and-layout-as-a-markdown-document.md)
(proposed).

Two limits to know first. A window's state is carried as base64 of its launch
config, so a document says which apps it opens but not, to a reader, in what
state. And only what an app composes as its workingset
([ADR-0148](../adr/0148-app-workingsets.md)) is carried: an app that composes
none is reopened as a click on it would open it.

## When to use this recipe

You arrange the same few windows in every session, or you want a set of
windows to ship with a build as one launcher entry.

## Prerequisites

- A host with the persist service, for saving. Without it the Window menu
  has no save entry; committed documents open either way.
- For a committed document: a shell that mints the registered books before
  launch resolution, as `imzero2 demo` does.

## Steps

1. **Arrange the desktop.** Open the windows, and pick a layout under
   **Window ▸ Desktop layout** if you want one kept. On a floating desktop
   the document records the windows and their order, not where they sit.

2. **Save it.** **Window ▸ Save desktop as workstatelet**, type a title,
   **Save**. The line under the button says what happened. The title
   becomes the slug: `My desk` is stored as `my-desk` and launched as
   `ws-my-desk`. Saving under the same title again replaces the document;
   the launcher entry's text follows at the next start.

3. **Open it.** It is in the launcher under the title, with a
   `workstatelet` badge. A committed document can also be named at start,
   by its alias:

   ```bash
   imzero2 demo --launch ws-desktop-review
   ```

   A saved one cannot: it is minted when the store starts, after `--launch`
   has been resolved.

   Opening adds its windows to the desktop and closes nothing. If the
   document names a layout, the desktop takes it, and windows that were
   already open follow the document's in the order.

4. **Read the document back.** In a playground on the in-process tables:

   ```sql
   SELECT slug, origin, layout, windows, configured_windows, doc
   FROM keelson('workstatelets')
   ```

   `doc` is the markdown as stored.

5. **Commit it to a book.** Save `doc` as `<slug>.md` in a book directory —
   [apps/workstatelets/book](../../apps/workstatelets/book) is the in-tree
   one — add the document stanza (`type`, `audience`, `status`) and the draft
   banner as [desktop-review.md](../../apps/workstatelets/book/desktop-review.md)
   has them, and write prose around the fences. A package contributes a book
   of its own with `workstatelet.RegisterBook` from `init`.

6. **State what each config holds.** A reviewer cannot read the base64. For
   each window that carries a config, write the value out in the book's test
   and compare its encoding with the document's bytes, as
   [workstatelets_test.go](../../apps/workstatelets/workstatelets_test.go)
   does for `desktop-review`. On a mismatch that test prints the base64 to
   paste, which is also how a config is written without capturing it.

## Verification

```bash
go test -tags="$(cat ./tags)" ./apps/workstatelets/ ./public/keelson/runtime/workstatelet/
scripts/dev/scene.sh apps/workstatelets/scenes
```

The first parses every committed document and probes every config. The
second launches `desktop-review` on the headless host and saves the desktop
from the menu.

## Troubleshooting

- **Symptom:** fewer windows open than the document lists.
  **Cause:** a window was refused — its app is not in this build, the launch
  limit excludes it, its config no longer decodes, or it is a singleton that
  already has a window.
  **Fix:** the host logs one warning per skipped window with the reason.
- **Symptom:** "slug is taken by a committed workstatelet" on save.
  **Cause:** a book document has that slug, and a committed document wins.
  **Fix:** save under another title.
- **Symptom:** a window opens on a query that returns nothing.
  **Cause:** it was captured while showing an ad-hoc dataset, which lasts as
  long as the window that published it.
  **Fix:** capture windows that read durable data, or republish the dataset.
- **Symptom:** an agent or another app cannot open a workstatelet.
  **Cause:** by design; only the launcher and `--launch` open one.
