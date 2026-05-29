---
type: how-to
audience: developer
status: draft
---

# Replaying a session

Steps:

1. Stop the running app.
2. Run `pebble replay --session <id>`.
3. Compare against the gold output.

## Gotchas

The replay binary respects `IMZERO2_SCREENSHOT_DIR` exactly like the live
app — useful for diffing visual regressions across replays.
