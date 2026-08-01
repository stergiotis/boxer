---
type: reference
audience: contributor
status: draft
generated: true
generator: go test, TestMembershipRoleClassifierGeneration
---

> **Status: draft — pre-human-review.** Machine-generated demo artifact;
> regenerate via `go test`, do not edit.

# anchor — membership role classification (DefaultClassifier)

| section | membership | role | param treatment |
|---|---|---|---|
| `symbol` | low-card ref 5 (drone model) | primary | none |
| `geoPoint` | high-card ref 999042 (customer) | primary | none |
| `symbol` | verbatim `/status/live` (under path prefix) | primary | none |
| `symbol` | verbatim `unit` (no path prefix) | secondary | none |
| `foreignKey` | low-card ref 7 on the linking section | primary | none |
| `symbol` | mixed ref 5 + params (the fourth anchor membership spec) | primary | identity |
