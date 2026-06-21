---
type: how-to
audience: end-user
status: draft
title: Check that keelson apps stay independent
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Check that keelson apps stay independent

keelson apps (`apps/<name>`) should not depend on each other — each should build
and be reasoned about on its own. This guide checks that invariant.

## Steps

1. Set the **view** switch to **Architecture**.
2. Read the verdict in the controls row: **✓ apps independent** means no app
   imports another app's packages; **⚠ N app→app violations** means one does.
3. If there are violations, look at the graph — the forbidden edges are drawn in
   **red**. The detail pane lists each one as
   `importing-package ▶ imported-package`.
4. Click a violation to jump to the importing package in the **Packages** view,
   where its neighbourhood and import list show exactly what pulls in the other
   app.

## What the check means

The rule flags a **direct** import edge from one `apps/<name>` package to a
*different* app's package; two packages inside the same app are fine. It keys on
the app directory, so it is independent of the **group depth** slider —
collapsing the graph coarser never hides a real violation, it only stops
colouring the now-merged edge. The detail-pane list stays authoritative.

A *transitive* coupling — app A importing a library that imports app B — is not
reddened, but it is still visible as a path through the quotient. If you need
that caught too, that is a planned extension, not a current guarantee.
