package play

// play_ops_canonical.go: get_canonical, a leeway row's canonical forms for an
// agent (ADR-0289 §SD2) — the canonform digest and the canonwire
// fingerprint get_detail also returns, and the CBOR items both were taken
// over, as the Detail pane's identity strip shows them behind its CBOR
// disclosure (ADR-0219 SD5, SD6). The canonwire item is the row's whole
// content, losslessly; the canonform items are what its content identity
// counted. Like get_detail, the read builds its own driver and touches
// nothing the pane holds.

import (
	"encoding/hex"
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
	"github.com/stergiotis/boxer/public/semistructured/cbor/diag"
)

const opGetCanonical = "get_canonical"

// The items get_canonical returns.
const (
	canonicalItemsBoth      = "both"
	canonicalItemsCanonwire = "canonwire"
	canonicalItemsCanonform = "canonform"
)

// CanonicalArgs is get_canonical's argument.
type CanonicalArgs struct {
	Row   *int64 `json:",omitzero" desc:"the row to read, 0 for the first; the selection when left out, as the Detail pane draws it"`
	Node  string `json:",omitzero" desc:"the split node whose row to read; the node the Detail pane follows when left out"`
	Items string `json:",omitzero" desc:"which items to return in diagnostic notation: both (the default), canonwire or canonform; one alone gets the whole byte bound"`
}

// CanonicalReading is get_canonical's result.
type CanonicalReading struct {
	ResultId  uint64 `desc:"the result the row is of"`
	Node      string `desc:"the split node whose result was read"`
	Row       int64  `desc:"the row, from 0"`
	Rows      int64  `desc:"rows in the result"`
	FormPin   string `desc:"the parameters the canonform digest was taken under; two digests compare only under one pin"`
	Canonform string `desc:"the canonform digest, hex: the record's content identity, equal for two rows holding the same attributes and values whatever their entity id (ADR-0201)"`
	Canonwire string `desc:"the canonwire fingerprint, hex: keyed BLAKE3-256 over the record's lossless wire item (ADR-0210)"`
	WireBytes int    `desc:"the wire item's size in bytes"`
	Canonical bool   `desc:"true when the wire item passes the runtime's canonical-form check"`
	Verdict   string `json:",omitzero" desc:"why the wire item fails the check"`
	// The items, in RFC 8949 diagnostic notation with the pane's position
	// comments.
	CanonwireItem  string `json:",omitzero" desc:"the canonwire entity item — version, plains, then each tagged slot's attributes with their memberships — the row's whole content, losslessly"`
	CanonformItems string `json:",omitzero" desc:"the items the canonform digest counted: each attribute's [memberships, value], then the entity's {plains, leaf digests}"`
	ItemsCut       bool   `json:",omitzero" desc:"true when the byte bound cut an items text; a cut text ends at a line boundary with a … line"`
}

// getCanonical reads one leeway row's canonical forms.
func getCanonical(r *opsResults, signals []SignalState, in CanonicalArgs) (out CanonicalReading, err error) {
	items := strings.ToLower(strings.TrimSpace(in.Items))
	switch items {
	case "":
		items = canonicalItemsBoth
	case canonicalItemsBoth, canonicalItemsCanonwire, canonicalItemsCanonform:
	default:
		return out, app.RefuseOperation("items is both, canonwire or canonform")
	}
	pane := ""
	if in.Node == "" {
		pane = detailPaneId
	}
	lr, err := r.read(pane, in.Node)
	if err != nil {
		return
	}
	defer lr.release()
	if lr.rec == nil || lr.schema == nil {
		return out, app.RefuseOperation("node " + string(lr.node) + " holds no result")
	}
	row, err := opsRow(signals, in.Row, lr.numRows)
	if err != nil {
		return
	}
	recipe, leeway := discoverCardRecipe(lr.schema)
	if !leeway {
		return out, app.RefuseOperation("the result is not leeway-shaped; canonical forms are a leeway record's")
	}
	out = CanonicalReading{ResultId: uint64(lr.id), Node: string(lr.node), Row: row, Rows: lr.numRows}
	comp, err := newIdentityComputer(recipe.table, recipe.ir, recipe.driver)
	if err != nil {
		return out, app.RefuseOperation(truncateRunes(err.Error(), 300))
	}
	vals, err := comp.row(lr.rec, row)
	if err != nil {
		return out, app.RefuseOperation(truncateRunes(err.Error(), 300))
	}
	out.FormPin = comp.pin
	out.Canonform, out.Canonwire = hex.EncodeToString(vals.canon[:]), hex.EncodeToString(vals.wire[:])
	out.WireBytes, out.Canonical = vals.wireLen, vals.wireErr == nil
	if vals.wireErr != nil {
		out.Verdict = truncateRunes(vals.wireErr.Error(), 300)
	}
	canonItems, wireItem, err := comp.rowItems(recipe.ir, lr.rec, row)
	if err != nil {
		return out, app.RefuseOperation("the row's items: " + truncateRunes(err.Error(), 300))
	}
	var cw, cf string
	if items != canonicalItemsCanonform {
		if cw, err = diag.String(wireItem, diag.Options{TagComments: true, Annotate: annotateCanonwire}); err != nil {
			return out, app.RefuseOperation("the canonwire item: " + truncateRunes(err.Error(), 300))
		}
	}
	if items != canonicalItemsCanonwire {
		if cf, err = diag.String(canonItems, diag.Options{Sequence: true, TagComments: true, Annotate: annotateCanonform}); err != nil {
			return out, app.RefuseOperation("the canonform items: " + truncateRunes(err.Error(), 300))
		}
	}
	// The wire item is the row's content, so it is cut last: the canonform
	// items get what it leaves, or half the bound when both are long.
	budget := opsSampleMaxBytes
	cwMax := max(budget/2, budget-len(cf))
	var cut1, cut2 bool
	out.CanonwireItem, cut1 = cutAtLine(cw, cwMax)
	out.CanonformItems, cut2 = cutAtLine(cf, budget-len(out.CanonwireItem))
	out.ItemsCut = cut1 || cut2
	return out, nil
}

// cutAtLine cuts s to at most maxBytes, back to its last line break, and
// marks the cut with a … line.
func cutAtLine(s string, maxBytes int) (out string, cut bool) {
	if len(s) <= maxBytes {
		return s, false
	}
	out = truncateBytes(s, max(0, maxBytes-4))
	if i := strings.LastIndexByte(out, '\n'); i > 0 {
		out = out[:i]
	}
	return out + "\n…", true
}

// opsRow is the row a row-reading operation reads: the one named, or the
// selection, checked against the result's rows.
func opsRow(signals []SignalState, explicit *int64, numRows int64) (row int64, err error) {
	row = -1
	if explicit != nil {
		row = *explicit
	} else {
		for _, s := range signals {
			if s.Name == string(signalSelection) {
				if n, perr := strconv.ParseInt(strings.TrimSpace(s.Value), 10, 64); perr == nil {
					row = n
				}
				break
			}
		}
		if row < 0 {
			return row, app.RefuseOperation("no row is selected; pass row, or set_signal selection")
		}
	}
	if row < 0 || row >= numRows {
		return row, app.RefuseOperation("row " + strconv.FormatInt(row, 10) + " is not in the result; it has " +
			strconv.FormatInt(numRows, 10) + " rows, 0 for the first")
	}
	return
}

func addCanonicalOps(s *appops.Set[*PlayLauncher, opsSnap]) {
	// Untrusted: the items are the data's.
	appops.Query(s, app.OperationSpec{Name: opGetCanonical, Version: 1,
		Summary: "read one leeway row's canonical forms: its content digest and wire fingerprint, and the CBOR items both were taken over in diagnostic notation — the canonwire item is the row's whole content, losslessly",
		Reads:   []string{opsResResult, opsResSignals, opsResPanes}, Agents: true, Untrusted: true,
		Follows: []string{"get_detail reads the same row as attributes and values; the digests agree"}},
		func(sn opsSnap, in CanonicalArgs) (CanonicalReading, error) {
			if !sn.mounted {
				return CanonicalReading{}, app.RefuseOperation("the window has not mounted")
			}
			return getCanonical(&sn.results, sn.state.Signals, in)
		})
}
