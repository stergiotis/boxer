
### Pijul / Patch Theory: Pitfalls & Patterns

#### 8. The "Graph Context Ambiguity" Pitfall (Edit Distance)
* **The Symptom:** Two users concurrently edit *different* lines in a file, but pulling the changes unexpectedly results in a structural conflict instead of a clean commutative merge.
* **The Cause:** Unlike Git, which merges based on heuristic line numbers, Pijul models files mathematically as a directed graph of characters/lines. To merge two independent patches, Pijul relies on "context nodes" (the unaffected lines immediately surrounding an edit) to anchor the changes. If a file is extremely small (e.g., 4 lines) and edits happen too close together, the surrounding context nodes overlap. Pijul's algorithm acts conservatively: if it cannot mathematically guarantee the structural independence of the edits, it safely forces a conflict.
* **The Pattern:** **Ensure Sufficient Graph Context**. When designing flat-file data structures for Event Sourcing via Pijul, recognize that proximity matters.
    * In testing: Add padding lines between keys to give the algorithm unambiguous anchor points.
    * In production: If using flat KV files, group related volatile keys or separate them with static structural boundaries (like section headers or empty lines) to provide robust context anchors for the patch graph.