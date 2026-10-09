// Package inscribe is the overlay agents point things out to the person on
// (ADR-0297): a component of the window host that paints highlights, notes,
// arrows, numbered steps and spotlights above every window, in one visual
// language whichever agent asked.
//
// Agents name targets by Anchor — a window, a rect relative to a window, or
// a rect of the viewport — and never by coordinates they computed. The
// window host resolves every anchor each frame (ResolverI), so a mark
// follows its window. One Scene holds every task's marks, keyed by
// task and id; Layout places them together, and Overlay draws them through
// the absolute overlay, after every window, so captures never contain them.
package inscribe
