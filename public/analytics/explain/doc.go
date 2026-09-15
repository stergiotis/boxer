// Package explain reads a labelling back off the feature matrix it was made
// from (ADR-0235): given a row-major matrix and a label per row, it fits a
// threshold tree to the labels and measures, per label and feature, how the
// members' values sit against everyone else's.
//
// The two readings answer different questions. The tree is the criterion: a
// rule per leaf, a conjunction of single-feature thresholds, with the share
// of the labelling it reproduces stated as a fidelity rather than assumed —
// the post-clustering threshold-tree reading of Moshkovitz, Dasgupta, Frost
// and Rashtchian (2020), fitted as a plain CART over the labels because a
// density clustering has no centres to fit around. The contrast is the
// description: for each label and feature the probability that a member's
// value exceeds a non-member's (the Mann–Whitney AUC, rank-based and so
// indifferent to the monotone transforms the labelling was computed under)
// beside the medians of both sides in the matrix's own units.
//
// Noise — a label of −1 — is left out of the tree's fit and counted, and is
// part of the rest a contrast compares against. Results are a function of
// the input alone: candidate splits tie-break by feature then threshold,
// ranks tie by averaging, and the fit is single-threaded.
package explain
