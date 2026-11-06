---
type: reference
audience: package maintainer
status: draft
---

> **Status: draft — pre-human-review.** Per-arm evidence of one run; the citable claim is the trial README's §0. Do not cite these cells on their own.

# Line-level metrics, per document and arm

Columns: body font size in px; leading as line height over body size; rows and words inside the common viewport; prose lines; median and p90 characters per full line (a full line is every line of a paragraph but its last); the share of full lines carrying 45–75 characters; measure as the p95 full-line width in em; raggedness as the mean slack of full lines against their paragraph's widest line, in paragraphs of three lines or more; widows as paragraphs of two lines or more whose last line is one word, over paragraphs; heading sizes as ratios to body; the median gap before and after a heading and between paragraphs, in line heights (a value of 1 is the ordinary line step); the WCAG contrast of body text against the page. A row marked pixels only was captured as a picture and carries no geometry.

## play-overview — `apps/play/help/overview.md`

| arm | body px | leading | rows | words | prose | cpl med | cpl p90 | 45–75 | measure em | rag | widows | headings | gap ↑H | gap H↓ | gap ¶ | contrast |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | --- | ---: | ---: | ---: | ---: |
| native | 13.0 | 1.38 | 47 | 652 | 44 | 150 | 154 | 0% | 74 | 0.00 | 0/35 | 2.00 1.69 | 3.3 | 1.6 | 1.4 | 17.1 |
| ids-chromium | 13.0 | 1.60 | 54 | 574 | 51 | 90 | 97 | 17% | 47 | 0.22 | 0/10 | 1.69 1.46 | 3.4 | 1.6 | 1.5 | 15.8 |
| ids-firefox | pixels only | | | | | | | | | | | | | | | |
| obsidian-chromium | 16.0 | 1.12 | 62 | 624 | 56 | 88 | 102 | 19% | 43 | 0.24 | 0/13 | 2.00 1.50 | 3.0 | 2.4 | 1.9 | 21.0 |
| obsidian-firefox | pixels only | | | | | | | | | | | | | | | |
| github-chromium | 16.0 | 1.59 | 43 | 536 | 41 | 108 | 116 | 19% | 56 | 0.20 | 1/10 | 2.00 1.50 | 2.4 | 2.1 | 1.6 | 15.8 |
| github-firefox | pixels only | | | | | | | | | | | | | | | |

## mdedit-blocks — `apps/mdedit/help/blocks.md`

| arm | body px | leading | rows | words | prose | cpl med | cpl p90 | 45–75 | measure em | rag | widows | headings | gap ↑H | gap H↓ | gap ¶ | contrast |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | --- | ---: | ---: | ---: | ---: |
| native | 13.0 | 1.38 | 40 | 666 | 23 | 155 | 159 | 0% | 74 | 0.02 | 0/13 | 2.00 1.69 | 2.7 | 1.6 | 1.4 | 17.1 |
| ids-chromium | 13.0 | 1.60 | 54 | 573 | 41 | 83 | 100 | 15% | 47 | 0.19 | 3/14 | 1.69 1.46 | 2.5 | 1.6 | 1.4 | 15.8 |
| ids-firefox | pixels only | | | | | | | | | | | | | | | |
| obsidian-chromium | 16.0 | 1.12 | 54 | 623 | 41 | 98 | 108 | 7% | 43 | 0.21 | 2/14 | 2.00 1.50 | 2.5 | 2.4 | 1.9 | 21.0 |
| obsidian-firefox | pixels only | | | | | | | | | | | | | | | |
| github-chromium | 16.0 | 1.50 | 39 | 484 | 22 | 111 | 119 | 15% | 55 | 0.13 | 1/9 | 2.00 1.50 | 2.2 | 2.2 | 1.7 | 15.8 |
| github-firefox | pixels only | | | | | | | | | | | | | | | |

## keelson-positioning — `doc/explanation/positioning/keelson.md`

| arm | body px | leading | rows | words | prose | cpl med | cpl p90 | 45–75 | measure em | rag | widows | headings | gap ↑H | gap H↓ | gap ¶ | contrast |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | --- | ---: | ---: | ---: | ---: |
| native | 13.0 | 1.38 | 41 | 586 | 27 | 152 | 157 | 0% | 73 | 0.01 | 0/15 | 2.00 1.69 | 2.7 | 1.6 | 1.4 | 17.1 |
| ids-chromium | 13.0 | 1.60 | 55 | 507 | 37 | 96 | 100 | 0% | 47 | 0.02 | 1/18 | 1.69 1.46 | 2.5 | 1.6 | 1.5 | 15.8 |
| ids-firefox | pixels only | | | | | | | | | | | | | | | |
| obsidian-chromium | 16.0 | 1.12 | 57 | 539 | 35 | 105 | 110 | 0% | 44 | 0.02 | 0/20 | 2.00 1.50 | 2.5 | 2.4 | 1.9 | 21.0 |
| obsidian-firefox | pixels only | | | | | | | | | | | | | | | |
| github-chromium | 16.0 | 1.50 | 46 | 498 | 30 | 113 | 116 | 0% | 55 | 0.02 | 0/17 | 2.00 1.50 | 2.2 | 2.2 | 1.7 | 15.8 |
| github-firefox | pixels only | | | | | | | | | | | | | | | |

## why-boxer — `doc/explanation/why-boxer.md`

| arm | body px | leading | rows | words | prose | cpl med | cpl p90 | 45–75 | measure em | rag | widows | headings | gap ↑H | gap H↓ | gap ¶ | contrast |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | --- | ---: | ---: | ---: | ---: |
| native | 13.0 | 1.38 | 53 | 931 | 48 | 152 | 157 | 0% | 73 | 0.05 | 1/14 | 2.00 1.69 1.38 | 2.7 | 1.6 | 1.4 | 17.1 |
| ids-chromium | 13.0 | 1.60 | 55 | 711 | 51 | 96 | 102 | 0% | 47 | 0.04 | 0/9 | 1.69 1.46 1.23 | 2.5 | 1.6 | 1.5 | 15.8 |
| ids-firefox | pixels only | | | | | | | | | | | | | | | |
| obsidian-chromium | 16.0 | 1.12 | 58 | 745 | 53 | 104 | 109 | 2% | 44 | 0.05 | 4/9 | 2.00 1.50 1.16 | 2.5 | 2.4 | 1.9 | 21.0 |
| obsidian-firefox | pixels only | | | | | | | | | | | | | | | |
| github-chromium | 16.0 | 1.50 | 45 | 665 | 41 | 114 | 118 | 0% | 56 | 0.03 | 0/8 | 2.00 1.50 1.25 | 2.4 | 2.2 | 1.7 | 15.8 |
| github-firefox | pixels only | | | | | | | | | | | | | | | |

## adr-0334 — `doc/adr/0334-mdedit-blocks-over-the-source.md`

| arm | body px | leading | rows | words | prose | cpl med | cpl p90 | 45–75 | measure em | rag | widows | headings | gap ↑H | gap H↓ | gap ¶ | contrast |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | --- | ---: | ---: | ---: | ---: |
| native | 13.0 | 1.38 | 45 | 645 | 36 | 150 | 155 | 0% | 73 | 0.02 | 0/23 | 2.00 1.69 1.23 | 2.7 | 1.7 | 1.4 | 17.1 |
| ids-chromium | 13.0 | 1.60 | 52 | 597 | 47 | 92 | 98 | 11% | 46 | 0.15 | 0/11 | 1.69 1.46 | 2.5 | 1.6 | 1.5 | 15.8 |
| ids-firefox | pixels only | | | | | | | | | | | | | | | |
| obsidian-chromium | 16.0 | 1.12 | 54 | 606 | 45 | 92 | 104 | 9% | 42 | 0.16 | 0/11 | 2.00 1.50 | 2.5 | 2.2 | 1.9 | 21.0 |
| obsidian-firefox | pixels only | | | | | | | | | | | | | | | |
| github-chromium | 16.0 | 1.50 | 42 | 557 | 37 | 109 | 115 | 0% | 55 | 0.12 | 0/10 | 2.00 1.50 | 2.2 | 2.0 | 1.7 | 15.8 |
| github-firefox | pixels only | | | | | | | | | | | | | | | |

## stress — `doc/trials/markdown-rendering-quality/harness/corpus/stress.md`

| arm | body px | leading | rows | words | prose | cpl med | cpl p90 | 45–75 | measure em | rag | widows | headings | gap ↑H | gap H↓ | gap ¶ | contrast |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | --- | ---: | ---: | ---: | ---: |
| native | 13.0 | 1.38 | 43 | 416 | 33 | 151 | 156 | 0% | 73 | 0.01 | 0/25 | 2.00 1.69 | 2.7 | 1.6 | 1.4 | 17.1 |
| ids-chromium | 13.0 | 1.80 | 53 | 389 | 40 | 89 | 99 | 4% | 47 | 0.44 | 2/12 | 1.69 1.46 | 2.2 | 1.4 | 1.3 | 15.8 |
| ids-firefox | pixels only | | | | | | | | | | | | | | | |
| obsidian-chromium | 16.0 | 1.12 | 56 | 411 | 40 | 28 | 107 | 3% | 44 | 0.51 | 1/10 | 2.00 1.50 | 2.5 | 2.2 | 2.0 | 21.0 |
| obsidian-firefox | pixels only | | | | | | | | | | | | | | | |
| github-chromium | 16.0 | 1.75 | 42 | 361 | 36 | 27 | 117 | 3% | 55 | 0.49 | 2/7 | 2.00 1.50 | 1.9 | 1.7 | 1.5 | 15.8 |
| github-firefox | pixels only | | | | | | | | | | | | | | | |

