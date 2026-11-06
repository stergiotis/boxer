---
type: reference
audience: package maintainer
status: draft
---

> **Status: draft — pre-human-review.** Per-arm evidence of one run; the citable claim is the trial README's §0. Do not cite these cells on their own.

# Line-level metrics, per document and arm

Columns: body font size in px; leading as line height over body size; rows and words inside the common viewport; prose lines; list rows (prose rows inside a list item) and table rows (unsplit prose rows inside a table cell), both kept out of the paragraph metrics; median and p90 characters per full line (a full line is every line of a paragraph but its last); the share of full lines carrying 45–75 characters; measure as the p95 full-line width in em; raggedness as the mean slack of full lines against their paragraph's widest line, in paragraphs of three lines or more; widows as paragraphs of two lines or more whose last line is one word, over paragraphs; heading sizes as ratios to body; the median gap before and after a heading and between paragraphs, in line heights (a value of 1 is the ordinary line step); the WCAG contrast of body text against the page. A row marked pixels only was captured as a picture and carries no geometry.

## play-overview — `apps/play/help/overview.md`

| arm | body px | leading | rows | words | prose | list | table | cpl med | cpl p90 | 45–75 | measure em | rag | widows | headings | gap ↑H | gap H↓ | gap ¶ | contrast |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | --- | ---: | ---: | ---: | ---: |
| native | 13.0 | 1.54 | 49 | 550 | 47 | 30 | 0 | 100 | 102 | 0% | 49 | 0.02 | 0/8 | 2.00 1.69 | 2.8 | 1.4 | 1.4 | 17.1 |
| ids-chromium | 13.0 | 1.60 | 54 | 574 | 51 | 35 | 0 | 94 | 100 | 0% | 47 | 0.03 | 0/7 | 1.69 1.46 | 3.4 | 1.6 | 1.5 | 15.8 |
| ids-firefox | pixels only | | | | | | | | | | | | | | | | | |
| obsidian-chromium | 16.0 | 1.12 | 62 | 624 | 56 | 40 | 0 | 102 | 108 | 0% | 43 | 0.02 | 0/7 | 2.00 1.50 | 3.0 | 2.4 | 1.9 | 21.0 |
| obsidian-firefox | pixels only | | | | | | | | | | | | | | | | | |
| github-chromium | 16.0 | 1.58 | 43 | 536 | 41 | 26 | 0 | 116 | 117 | 0% | 56 | 0.00 | 1/7 | 2.00 1.50 | 2.4 | 2.1 | 1.6 | 15.8 |
| github-firefox | pixels only | | | | | | | | | | | | | | | | | |

## mdedit-blocks — `apps/mdedit/help/blocks.md`

| arm | body px | leading | rows | words | prose | list | table | cpl med | cpl p90 | 45–75 | measure em | rag | widows | headings | gap ↑H | gap H↓ | gap ¶ | contrast |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | --- | ---: | ---: | ---: | ---: |
| native | 13.0 | 1.54 | 48 | 514 | 30 | 0 | 10 | 103 | 108 | 0% | 50 | 0.02 | 0/9 | 2.00 1.69 | 2.5 | 1.4 | 1.5 | 17.1 |
| ids-chromium | 13.0 | 1.60 | 54 | 573 | 41 | 5 | 19 | 97 | 102 | 0% | 48 | 0.03 | 1/6 | 1.69 1.46 | 2.5 | 1.6 | 1.4 | 15.8 |
| ids-firefox | pixels only | | | | | | | | | | | | | | | | | |
| obsidian-chromium | 16.0 | 1.12 | 54 | 623 | 41 | 8 | 17 | 106 | 109 | 0% | 43 | 0.01 | 1/6 | 2.00 1.50 | 2.5 | 2.4 | 1.9 | 21.0 |
| obsidian-firefox | pixels only | | | | | | | | | | | | | | | | | |
| github-chromium | 16.0 | 1.50 | 39 | 484 | 22 | 0 | 8 | 114 | 119 | 0% | 55 | 0.02 | 0/5 | 2.00 1.50 | 2.2 | 2.2 | 1.7 | 15.8 |
| github-firefox | pixels only | | | | | | | | | | | | | | | | | |

## keelson-positioning — `doc/explanation/positioning/keelson.md`

| arm | body px | leading | rows | words | prose | list | table | cpl med | cpl p90 | 45–75 | measure em | rag | widows | headings | gap ↑H | gap H↓ | gap ¶ | contrast |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | --- | ---: | ---: | ---: | ---: |
| native | 13.0 | 1.54 | 47 | 528 | 34 | 0 | 1 | 101 | 106 | 0% | 50 | 0.02 | 1/17 | 2.00 1.69 | 2.5 | 1.4 | 1.4 | 17.1 |
| ids-chromium | 13.0 | 1.60 | 55 | 507 | 37 | 0 | 12 | 96 | 100 | 0% | 47 | 0.02 | 1/8 | 1.69 1.46 | 2.5 | 1.6 | 1.5 | 15.8 |
| ids-firefox | pixels only | | | | | | | | | | | | | | | | | |
| obsidian-chromium | 16.0 | 1.12 | 57 | 539 | 35 | 0 | 13 | 106 | 110 | 0% | 44 | 0.02 | 0/8 | 2.00 1.50 | 2.5 | 2.4 | 1.9 | 21.0 |
| obsidian-firefox | pixels only | | | | | | | | | | | | | | | | | |
| github-chromium | 16.0 | 1.50 | 46 | 498 | 30 | 0 | 10 | 113 | 116 | 0% | 55 | 0.02 | 0/8 | 2.00 1.50 | 2.2 | 2.2 | 1.7 | 15.8 |
| github-firefox | pixels only | | | | | | | | | | | | | | | | | |

## why-boxer — `doc/explanation/why-boxer.md`

| arm | body px | leading | rows | words | prose | list | table | cpl med | cpl p90 | 45–75 | measure em | rag | widows | headings | gap ↑H | gap H↓ | gap ¶ | contrast |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | --- | ---: | ---: | ---: | ---: |
| native | 13.0 | 1.54 | 52 | 694 | 48 | 0 | 0 | 101 | 107 | 0% | 50 | 0.03 | 1/10 | 2.00 1.69 1.38 | 2.5 | 1.4 | 1.4 | 17.1 |
| ids-chromium | 13.0 | 1.60 | 55 | 711 | 51 | 4 | 0 | 96 | 102 | 0% | 47 | 0.02 | 0/8 | 1.69 1.46 1.23 | 2.5 | 1.6 | 1.5 | 15.8 |
| ids-firefox | pixels only | | | | | | | | | | | | | | | | | |
| obsidian-chromium | 16.0 | 1.12 | 58 | 745 | 53 | 7 | 0 | 105 | 109 | 0% | 44 | 0.03 | 4/8 | 2.00 1.50 1.16 | 2.5 | 2.4 | 1.9 | 21.0 |
| obsidian-firefox | pixels only | | | | | | | | | | | | | | | | | |
| github-chromium | 16.0 | 1.50 | 45 | 665 | 41 | 0 | 0 | 114 | 118 | 0% | 56 | 0.03 | 0/8 | 2.00 1.50 1.25 | 2.4 | 2.2 | 1.7 | 15.8 |
| github-firefox | pixels only | | | | | | | | | | | | | | | | | |

## adr-0334 — `doc/adr/0334-mdedit-blocks-over-the-source.md`

| arm | body px | leading | rows | words | prose | list | table | cpl med | cpl p90 | 45–75 | measure em | rag | widows | headings | gap ↑H | gap H↓ | gap ¶ | contrast |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | --- | ---: | ---: | ---: | ---: |
| native | 13.0 | 1.54 | 48 | 585 | 43 | 24 | 0 | 102 | 104 | 0% | 50 | 0.03 | 0/9 | 2.00 1.69 | 2.5 | 1.8 | 1.4 | 17.1 |
| ids-chromium | 13.0 | 1.60 | 52 | 597 | 47 | 28 | 0 | 97 | 99 | 0% | 47 | 0.02 | 0/8 | 1.69 1.46 | 2.5 | 1.6 | 1.5 | 15.8 |
| ids-firefox | pixels only | | | | | | | | | | | | | | | | | |
| obsidian-chromium | 16.0 | 1.13 | 54 | 606 | 45 | 27 | 0 | 103 | 107 | 0% | 44 | 0.04 | 0/8 | 2.00 1.50 | 2.5 | 2.2 | 1.9 | 21.0 |
| obsidian-firefox | pixels only | | | | | | | | | | | | | | | | | |
| github-chromium | 16.0 | 1.50 | 42 | 557 | 37 | 21 | 0 | 114 | 117 | 0% | 55 | 0.01 | 0/7 | 2.00 1.50 | 2.2 | 2.0 | 1.7 | 15.8 |
| github-firefox | pixels only | | | | | | | | | | | | | | | | | |

## stress — `doc/trials/markdown-rendering-quality/harness/corpus/stress.md`

| arm | body px | leading | rows | words | prose | list | table | cpl med | cpl p90 | 45–75 | measure em | rag | widows | headings | gap ↑H | gap H↓ | gap ¶ | contrast |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | --- | ---: | ---: | ---: | ---: |
| native | 13.0 | 1.54 | 45 | 395 | 39 | 21 | 0 | 102 | 107 | 0% | 49 | 0.04 | 0/6 | 2.00 1.69 | 2.5 | 1.4 | 1.4 | 17.1 |
| ids-chromium | 13.0 | 1.60 | 53 | 389 | 40 | 22 | 2 | 96 | 100 | 0% | 47 | 0.03 | 0/4 | 1.69 1.46 | 2.5 | 1.6 | 1.5 | 15.8 |
| ids-firefox | pixels only | | | | | | | | | | | | | | | | | |
| obsidian-chromium | 16.0 | 1.12 | 56 | 411 | 40 | 21 | 3 | 105 | 109 | 0% | 44 | 0.04 | 0/4 | 2.00 1.50 | 2.5 | 2.2 | 2.0 | 21.0 |
| obsidian-firefox | pixels only | | | | | | | | | | | | | | | | | |
| github-chromium | 16.0 | 1.50 | 42 | 361 | 36 | 21 | 0 | 115 | 118 | 0% | 56 | 0.02 | 1/4 | 2.00 1.50 | 2.2 | 2.0 | 1.7 | 15.8 |
| github-firefox | pixels only | | | | | | | | | | | | | | | | | |

