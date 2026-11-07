---
type: reference
audience: contributor
status: draft
scene:
  launch: terrainscope
  size: 1100x1000
  stepSettleMs: 400
  requires: ["exe:python3"]
  services: [tilestub]
  env:
    BOXER_TERRAINSCOPE_POINTS: "46.9479,7.4446,46.9520,7.4700"
    SWISSTOPO_TILES_DIR: "apps/terrainscope/scenes/no-such-tiles"
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# terrainscope — a seeded sweep that cannot open its tiles

The window opens on two points from `BOXER_TERRAINSCOPE_POINTS`, so no map
click is needed, and the sweep launches on the first frame. The tile
directory is pointed at one that does not exist on purpose: the scene pins
the failure path, which every host has, rather than a result, which only a
host holding the swissALTI3D tiles has. The failure badge's sentence is read
and asserted, the Sweep and Distributions tabs are reached by name and say
where the reason is, and F1 opens the app's own help. A scene over a real
sweep is deferred to a host with the tiles (`SWISSTOPO_TILES_DIR`); the
basemap under the points comes from the tile stub, so nothing reaches the
network.

```jsonl trace
{"do":"note","text":"--- the seeded points launch a sweep, which fails on the missing tiles ---"}
{"do":"wait","valueContains":"sweep failed: elevation tiles not available","role":"label","settleMs":1500}
{"do":"read","valueContains":"sweep failed: elevation tiles not available","role":"label","pattern":"sweep failed: (?P<reason>elevation tiles not available)"}
{"do":"expect","of":"reason","is":"elevation tiles not available"}
{"do":"wait","name":"Retry","role":"button"}
{"do":"capture","text":"terrainscope-failed"}

{"do":"note","text":"--- the plot tabs point back to the status row ---"}
{"do":"click","name":"Sweep","role":"button"}
{"do":"wait","value":"The sweep failed; the reason and Retry are in the status row above.","role":"label"}
{"do":"capture","text":"terrainscope-sweep-tab"}
{"do":"click","name":"Distributions","role":"button"}
{"do":"wait","value":"The sweep failed; the reason and Retry are in the status row above.","role":"label"}
{"do":"click","name":"Map","role":"button"}

{"do":"note","text":"--- F1 opens the app's own help ---"}
{"do":"key","text":"F1","settleMs":1200}
{"do":"wait","contains":"Help center","role":"window"}
{"do":"wait","value":"Terrain line of sight","role":"label","comment":"the reader shows the app's overview, not the index"}
{"do":"capture","text":"terrainscope-help"}
```
