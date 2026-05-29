//go:build llm_generated_opus47

package widgets

import (
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
)

// Package-level helpers that resolve the IDS spacing tokens at the
// active density per ADR-0032 §SD2. The widgets-package demos are
// stateless registry callbacks (no per-instance App struct to stash
// the active density on), so each helper reads
// styletokens.DensityFromEnv() inline. env reads are cheap and the
// dozen-or-so calls per demo frame don't show up against the egui
// repaint cost.
//
// Naming intentionally mirrors the styletokens accessors with the
// `Padding`/`Gap` prefixes shortened so call sites stay legible inside
// chained widget builders.

func padHair() (v float32)     { v = styletokens.PaddingHair(styletokens.DensityFromEnv()); return }
func padInner() (v float32)    { v = styletokens.PaddingInner(styletokens.DensityFromEnv()); return }
func padDefault() (v float32)  { v = styletokens.PaddingDefault(styletokens.DensityFromEnv()); return }
func padOuter() (v float32)    { v = styletokens.PaddingOuter(styletokens.DensityFromEnv()); return }
func gapInline() (v float32)   { v = styletokens.GapInline(styletokens.DensityFromEnv()); return }
func gapItems() (v float32)    { v = styletokens.GapItems(styletokens.DensityFromEnv()); return }
func gapSections() (v float32) { v = styletokens.GapSections(styletokens.DensityFromEnv()); return }
