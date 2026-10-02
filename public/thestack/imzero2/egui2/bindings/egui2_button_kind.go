package bindings

// ButtonKindE names what a button is for, and the IDS colours it in that
// role (ADR-0273). The colours are resolved on the client, per theme and
// per interaction state, from the semantic palette; Go names the role only.
// Mirror of the KIND_* constants in imzero2_egui/src/style/button.rs.
//
// The zero value is the secondary kind, the button the theme draws anyway,
// so every button that names no kind is a secondary one.
type ButtonKindE uint8

const (
	// ButtonKindSecondary is the ordinary button: any action that is not
	// the view's main one.
	ButtonKindSecondary ButtonKindE = 0
	// ButtonKindPrimary is the one action a view leads with — Save, Run,
	// Apply. Filled in the accent role; use at most one per group.
	ButtonKindPrimary ButtonKindE = 1
	// ButtonKindTertiary is an accent outline: an action worth pointing at
	// that is not the main one.
	ButtonKindTertiary ButtonKindE = 2
	// ButtonKindGhost has no frame until hovered — for toolbars and rows
	// of many actions.
	ButtonKindGhost ButtonKindE = 3
	// ButtonKindDanger is a destructive action, filled in the error role.
	ButtonKindDanger ButtonKindE = 4
	// ButtonKindDangerGhost is a destructive action in error-coloured text
	// with no frame until hovered — Remove in a list row.
	ButtonKindDangerGhost ButtonKindE = 5
)

// AllButtonKinds lists the kinds in declaration order.
var AllButtonKinds = []ButtonKindE{
	ButtonKindSecondary,
	ButtonKindPrimary,
	ButtonKindTertiary,
	ButtonKindGhost,
	ButtonKindDanger,
	ButtonKindDangerGhost,
}

// String returns the kind's name.
func (inst ButtonKindE) String() (s string) {
	switch inst {
	case ButtonKindPrimary:
		s = "primary"
	case ButtonKindTertiary:
		s = "tertiary"
	case ButtonKindGhost:
		s = "ghost"
	case ButtonKindDanger:
		s = "danger"
	case ButtonKindDangerGhost:
		s = "danger-ghost"
	default:
		s = "secondary"
	}
	return
}

// Kind colours the button by its role. A kind's text colour applies to
// plain text atoms; a rich-text atom with its own colour keeps it.
func (inst ButtonFluid) Kind(kind ButtonKindE) ButtonFluid {
	return inst.kind(uint8(kind))
}
