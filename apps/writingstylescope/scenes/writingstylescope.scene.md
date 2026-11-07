---
type: reference
audience: contributor
status: draft
scene:
  launch: writingstylescope
  size: 1000x940
  stepSettleMs: 400
  env:
    BOXER_WRITINGSTYLESCOPE_DOCS: '{"DocA": "---\ntype: reference\naudience: contributor\nstatus: draft\ntitle: Scene fixture — document A\n---\n\n> **Status: draft — pre-human-review.** A scene fixture: two short documents sharing one section.\n\n# Harbour log, spring\n\n## Arrival\n\nThe ferry came round the breakwater a little after six, low in the water\nwith the first load of the season: timber for the boatyard, two crates of\nseed potatoes and a mail sack that had been waiting at the railhead since\nthe thaw. The harbourmaster counted the crates twice and signed for the\nsack once, which is the order he has done it in for eleven years.\n\n## The boatyard\n\nWork on the long hull resumed the same morning. Three of the ribs had\nopened along the grain over the winter and had to be steamed and refitted\nbefore any planking could go on. The yard keeps a ledger of every rib by\nnumber, so the replacements were cut to the old dimensions without anyone\nhaving to measure the hull again, which saved most of a day.\n\n## Weather\n\nA westerly set in by noon and held for four days. The fishing boats stayed\nin, the nets were taken up to the loft to be mended, and the slipway was\nused for nothing but hauling the pilot launch out to scrape its bottom. The\nglass began to rise on the fifth morning and the boats were out before it\nwas properly light.\n\n## Accounts\n\nThe harbour dues for the quarter came to a little under what was budgeted,\nbecause two of the larger boats moved their moorings up the coast. The\ncommittee agreed to hold the rate where it is for another year and to put\nthe difference towards the new fenders on the north quay, which have been\npatched with old tyres long enough.\n", "DocB": "---\ntype: reference\naudience: contributor\nstatus: draft\ntitle: Scene fixture — document B\n---\n\n> **Status: draft — pre-human-review.** A scene fixture: two short documents sharing one section.\n\n# Notes from the quay\n\n## Morning\n\nNobody on the quay expected the fog to lift before ten, so the market\nopened late and the first buyers were the restaurant people from the town,\nwho want the best of the catch and will wait for it. Prices held steady on\nwhiting and fell a little on mackerel, of which there was more than anyone\ncould sell before the ice ran out.\n\n## The long hull\n\nWork on the long hull resumed the same morning. Three of the ribs had\nopened along the grain over the winter and had to be steamed and refitted\nbefore any planking could go on. The yard keeps a ledger of every rib by\nnumber, so the replacements were cut to the old dimensions without anyone\nhaving to measure the hull again, which saved most of a day.\n\n## Gear\n\nThe new winch on the slipway draws more current than the old one, and the\nfuse in the shed has gone twice under load. An electrician from the town\nhas been asked to look at the supply. Until then the winch is run only with\nthe shed lights off, which nobody likes on a dark afternoon.\n\n## Visitors\n\nA school party came down in the afternoon to see the lifeboat. The coxswain\nshowed them the station, let each of them sit at the wheel, and explained\nwhy the boat is kept on a slipway rather than afloat: in a hurry it is\nquicker to slide down than to row out to a mooring and climb aboard.\n"}'
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# writingstylescope — two seeded documents with one shared section

The window opens on two fixture documents carried as text in
`BOXER_WRITINGSTYLESCOPE_DOCS`, so the panes hold text of the scene's own
rather than the person's drafts, and the first sweep runs on the first
frame. Each document has four sections over the floor and two below it —
the status line above the title and the title heading, which has no text
of its own; one section of A was copied into B under another heading. The status row's summary is read and its pair count asserted, the
Matrix tab is reached by name and lists the pairs, the Distribution tab is
reached by name and its closing line is read to assert that the closest pair
sits far below the median — the copied section, detached from the bulk — and
F1 opens the app's own help.

```jsonl trace
{"do":"note","text":"--- the seeded documents are swept on the first frame ---"}
{"do":"wait","valueContains":"sections, 16 pairs","role":"label","settleMs":1500}
{"do":"read","valueContains":"sections, 16 pairs","role":"label","pattern":"(?P<rows>\\d+) × (?P<cols>\\d+) sections, (?P<pairs>\\d+) pairs"}
{"do":"expect","of":"rows","eq":4}
{"do":"expect","of":"pairs","eq":16}
{"do":"wait","valueContains":"A: 4 sections (2 below the floor)","role":"label"}
{"do":"capture","text":"writingstylescope-documents"}

{"do":"note","text":"--- the matrix tab ranks the pairs ---"}
{"do":"click","name":"Matrix","role":"button"}
{"do":"wait","value":"Closest pairs","role":"label"}
{"do":"wait","value":"The long hull","role":"label"}
{"do":"capture","text":"writingstylescope-matrix"}

{"do":"note","text":"--- the copied section is detached from the bulk ---"}
{"do":"click","name":"Distribution","role":"button"}
{"do":"wait","valueContains":"Where each pair sits among all the others.","role":"label"}
{"do":"read","valueContains":"Closest pair ","role":"label","pattern":"Closest pair (?P<best>[\\d.]+), median pair (?P<median>[\\d.]+)"}
{"do":"expect","of":"best","max":0.2}
{"do":"expect","of":"median","min":0.5}
{"do":"capture","text":"writingstylescope-distribution"}
{"do":"click","name":"Documents","role":"button"}

{"do":"note","text":"--- F1 opens the app's own help ---"}
{"do":"key","text":"F1","settleMs":1200}
{"do":"wait","contains":"Help center","role":"window"}
{"do":"wait","value":"Shared writing between two documents","role":"label","comment":"the reader shows the app's overview, not the index"}
{"do":"capture","text":"writingstylescope-help"}
```
