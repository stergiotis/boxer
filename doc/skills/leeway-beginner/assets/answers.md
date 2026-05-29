---
type: reference
audience: agent reading this skill asset
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.
### 9.2 Answers & Explanations

**Q1: B.** Leeway shreds by **Canonical Type**, not by Domain. All `int64` values are physically stored in the same `int64` section to maximize vectorization and compression.

**Q2: C.** Leeway uses the **Wildcard/Parameter** concept. The schema tag becomes `/map/_` (static), and the UUID becomes a data parameter (dynamic). This keeps the dictionary size small.

**Q3: A.** Leeway supports **Aliasing** via the `membership-card` vector. This effectively models a Graph Edge list (`Value -> [Tag A, Tag B]`), preventing data duplication.

**Q4: C.** The `value-card` defines the number of **Scalar Units** that make up one logical value. `value-card=9` tells the reader to consume the next 9 scalars to reconstruct the single matrix object.

**Q5: B.** Leeway uses **Self-Describing Physical Columns**. The column name *is* the schema. Base62 encoding is used for compact representation of bitmasks (hints/aspects).

**Q6: B.** Opaque columns in Leeway are often **projections** or materializations of shredded data into standard formats (like a flat Int64 or String column) to enable integration with standard BI tools (Tableau, PowerBI).

**Q7: B.** Co-Sections allow splitting attributes of a single logical object into different physical types (e.g., Lat/Lon in one section, H3 index in another) without duplicating the structural overhead (membership lists).

manmade:
B C A C B B B
pro
B C A C B B B