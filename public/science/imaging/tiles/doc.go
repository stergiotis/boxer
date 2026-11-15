// Package tiles holds an image as tiles and the reduced levels over it
// (ADR-0374 §SD7): level 0 cut into 512-pixel tiles, and each level a
// viewer reduces to — means, minima and maxima, exactly as the levels
// package defines them — cut the same way. A [Builder] fills a [Store] from
// rows in the order an image is read, top to bottom, holding no more than
// one 512-row band of the image besides what it has stored, and publishes
// each tile as soon as it is final; a reader asking for a tile not yet
// final waits for it. The store is the image: nothing else need hold its
// samples.
//
// # What it costs
//
// The store holds the samples, the means of every level — about a third
// more — and their minima and maxima, about as much again (ADR-0374 §SD9).
// Building costs one pass over the samples per level, as the levels package
// does. A float plane's histogram over a region reads the region twice:
// once for the range its bins divide, once to count.
package tiles
