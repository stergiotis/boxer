---
type: reference
audience: contributor
status: draft
scene:
  launch: play
  size: 1920x1200
  stepSettleMs: 350
  env:
    BOXER_PLAY_WINDOW_SIZE: "1888x1100"
    BOXER_PLAY_AUTORUN: "1"
    BOXER_PLAY_FOCUS_BOWTIE: "1"
  requires: ["clickhouse"]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# bowtie

Bow-tie — the result as threats, prevention barriers in series, one top event, mitigation barriers in series and consequences (ADR-0122's 2026-11-01 update): three threats, three consequences and nine barriers from inline rows, each barrier's test status as colour, icon and label

```sql
WITH 'Ransomware encrypts core ERP' AS top_event
SELECT top_event, side, path, step, barrier, status, evidence
FROM values('side String, path String, step UInt8, barrier String, status String, evidence String',
  ('prevention', 'Phishing steals credentials',   1, 'MFA on all remote access',                'effective', 'Tested 12 Sep · 0 bypasses in 40 attempts'),
  ('prevention', 'Phishing steals credentials',   2, 'Mail filtering and sandbox',              'degraded',  'Tested 3 Aug · 6 of 50 test payloads delivered'),
  ('prevention', 'Unpatched VPN appliance',       1, 'Known-exploited patches within 14 days',  'degraded',  '6 critical patches overdue'),
  ('prevention', 'Supplier remote access abused', 1, 'Supplier access through PAM only',        'untested',  'No test recorded since Jul last year'),
  ('prevention', 'Supplier remote access abused', 2, 'Session recording and review',            'failed',    'Tested 20 Sep · 3 of 10 sessions unrecorded'),
  ('mitigation', 'Production halt over 48 h',     1, 'Offline backups, restore drill',          'degraded',  'Restore tests 92% passed, appetite 95%'),
  ('mitigation', 'Production halt over 48 h',     2, 'Manual order fallback',                   'effective', 'Exercised 14 Jun · 6 h to switch'),
  ('mitigation', 'Notification deadline missed',  1, 'Incident playbook with 24 h report step', 'effective', 'Tabletop 2 Oct · report drafted in 9 h'),
  ('mitigation', 'Data leaked for extortion',     1, 'Egress monitoring and EDR containment',   'failed',    'Red team 18 Sep · exfiltration undetected for 11 h'))
```

```jsonl trace
{"do":"wait","name":"Run","comment":"the app has mounted"}
{"do":"read","valueContains":"threats ·","role":"label","pattern":"(?P<n>\\d+) barriers","comment":"the pane folded the rows"}
{"do":"expect","of":"n","eq":9}
{"do":"sleep","settleMs":1500}
{"do":"capture","text":"38_bowtie"}
{"do":"hover","name":"Session recording and review","role":"unknown","comment":"the failed barrier, by the accessible name its hit region carries"}
{"do":"read","valueContains":"sessions unrecorded","role":"label","pattern":"(?P<tested>Tested, failed)","comment":"the tooltip carries the barrier's status and evidence"}
{"do":"capture","text":"38_bowtie_hover"}
```
