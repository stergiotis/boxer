// Package launcher is the surface a person uses to find and start an app
// (ADR-0214). It owns every launcher view: the two-pane browse-and-search
// component, and the reduced "Apps ▾" menu.
//
// # One component, several mount points
//
// The component renders the same way wherever it is mounted (§SD2): the
// windowhost's empty-state pane when no window is open, and the launcher's
// own window at any other time. Before ADR-0214 those were two surfaces with
// different abilities — only the pane could search, and it disappeared at the
// first open — kept in step by a shared predicate function. They drifted. One
// component with one state value is the structural fix.
//
// # The row, and why it virtualises
//
// A row is icon, Display over a dimmed Manifest.Summary, and badges that only
// appear when they say something (§SD5). The list renders through
// egui_table's visible-range query, so per-frame cost tracks window height
// rather than corpus size — the app corpus grows at authoring speed, one
// registration per committed applet document, so a per-row cost would need
// revisiting on a schedule nobody controls.
//
// # Dependency direction
//
// This package does not import windowhost (§SD3). The windowhost renders the
// component for its empty-state pane, so the dependency must run one way; what
// the launcher needs of a host — open-or-raise, and which apps have windows —
// it declares as [HostI] and receives at construction. windowhost supplies an
// adapter.
package launcher
