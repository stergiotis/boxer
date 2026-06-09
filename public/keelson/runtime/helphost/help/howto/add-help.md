---
type: how-to
audience: developer
status: draft
title: Adding help docs to your app
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Adding help docs to your app

Steps:

1. Create a `help/` directory next to your `app_register.go`.
2. Author one or more `*.md` files inside it. Use ATX headings (`# `)
   and YAML frontmatter (see boxer's `doc/DOCUMENTATION_STANDARD.md`).
   Recommended starter shape:

   ```markdown
   ---
   type: explanation
   audience: end-user
   status: draft
   title: Your doc title
   ---

   # Your doc title

   Body…
   ```

3. In `app_register.go`, embed the directory and wire it into the
   manifest via `help.MustSub`:

   ```go
   import (
       "embed"
       "github.com/stergiotis/boxer/public/keelson/runtime/help"
   )

   //go:embed help
   var helpFS embed.FS

   var manifest = app.Manifest{
       // …
       Help: help.MustSub(helpFS, "help"),
   }
   ```

4. Rebuild. The `DefaultLibrary` picks up the new corpus the next
   time the Help app reads from it.

## Doc layout convention

Pick names that match the Diátaxis quadrant of the content:

- `help/overview.md` — landing page (Diátaxis `explanation`)
- `help/howto-<verb>.md` — recipes (`how-to`)
- `help/reference-<topic>.md` — exhaustive details (`reference`)
- `help/tutorial-<journey>.md` — end-to-end lesson (`tutorial`)

Nested directories work too — `help/howto/replay.md` becomes the doc
path `help/howto/replay` in the index.
