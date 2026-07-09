// Package pager is a reusable client-side paginator over an in-memory dataset.
// It owns no data — the caller feeds it a total via Configure and reads back
// (start, end) page bounds via Range. Extracted from the play app's pager
// (apps/play/play_pager.go) and generalised: the page-size buckets, the unit
// noun ("rows" → "fields", …), and whether the page-size selector shows are all
// configurable, so it fits both large virtualised data tables and short item
// lists (e.g. a handful of editor rows).
package pager

import (
	"fmt"
	"strconv"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// Pager paginates an in-memory dataset. Construct with New, (re)bind the total
// each frame with Configure, draw the bar with Render, and read the visible
// slice with Range.
type Pager struct {
	ids               *c.WidgetIdStack
	total             int64
	pageSize          int64
	currentPage       int64
	pageLabels        []string
	jumpValue         uint64 // mirrors currentPage+1 (1-based); r9 U64 databinding target of the jump DragValue
	lastSentJumpValue uint64 // what jumpValue held right after SendRespVal — distinguishes a user drag from Sync's echo

	pageSizeOptions []int64
	unit            string // plural noun for the range annotation ("rows", "fields")
	showSizeCombo   bool
}

const pagerWindowSize = 7 // numbered buttons shown at once

var defaultPageSizeOptions = []int64{50, 100, 500, 1000, 10000}

// New creates a pager with an initial page size. ids must be a fresh
// WidgetIdStack dedicated to the pager — Render Reset()s it each frame.
func New(ids *c.WidgetIdStack, initialPageSize int64) *Pager {
	if initialPageSize <= 0 {
		initialPageSize = 100
	}
	return &Pager{
		ids:             ids,
		pageSize:        initialPageSize,
		pageSizeOptions: defaultPageSizeOptions,
		unit:            "rows",
		showSizeCombo:   true,
	}
}

// WithPageSizeOptions sets the buckets offered by the page-size selector.
func (inst *Pager) WithPageSizeOptions(opts []int64) *Pager {
	if len(opts) > 0 {
		inst.pageSizeOptions = opts
	}
	return inst
}

// WithUnit sets the plural noun used in the range annotation ("rows 1–50 of N"
// → e.g. "fields 1–6 of N").
func (inst *Pager) WithUnit(plural string) *Pager {
	if plural != "" {
		inst.unit = plural
	}
	return inst
}

// WithPageSizeCombo toggles the page-size selector. Hide it when the page size
// is fixed — e.g. a bounded list whose page can't usefully grow (no row
// virtualisation), where the data-table buckets would be misleading.
func (inst *Pager) WithPageSizeCombo(show bool) *Pager {
	inst.showSizeCombo = show
	return inst
}

// Configure (re)binds the pager to a new total. Clamps the current page and
// rebuilds the page-label cache when the total or page size changed.
func (inst *Pager) Configure(total int64) {
	if total < 0 {
		total = 0
	}
	numPages := max(inst.numPagesFor(total, inst.pageSize), 1)
	if inst.currentPage >= numPages {
		inst.currentPage = numPages - 1
	}
	if inst.total == total && len(inst.pageLabels) == int(numPages) {
		return
	}
	inst.total = total
	inst.pageLabels = growStrings(inst.pageLabels, int(numPages))
	for i := range numPages {
		inst.pageLabels[i] = strconv.FormatInt(i+1, 10)
	}
}

// Reset snaps back to page 0, preserving the page size.
func (inst *Pager) Reset() {
	inst.currentPage = 0
	inst.jumpValue = 0
	inst.lastSentJumpValue = 0
}

// GoToLast jumps to the final page (e.g. after appending an item). Seeds the
// jump databinding so the next Render honours the target instead of reverting
// to the pre-jump value.
func (inst *Pager) GoToLast() {
	inst.currentPage = inst.NumPages() - 1
	inst.jumpValue = uint64(inst.currentPage + 1)
	inst.lastSentJumpValue = 0
}

// Range returns [start, end) indices for the current page, clamped to total.
func (inst *Pager) Range() (start, end int64) {
	start = inst.currentPage * inst.pageSize
	end = min(inst.total, start+inst.pageSize)
	return
}

// CurrentPage returns the 0-indexed current page.
func (inst *Pager) CurrentPage() int64 { return inst.currentPage }

// PageSize returns the active page size.
func (inst *Pager) PageSize() int64 { return inst.pageSize }

// NumPages returns the page count (≥1, even when total==0 for a "1 / 1" display).
func (inst *Pager) NumPages() int64 {
	return max(inst.numPagesFor(inst.total, inst.pageSize), 1)
}

func (inst *Pager) numPagesFor(total, size int64) int64 {
	if total <= 0 || size <= 0 {
		return 0
	}
	return (total + size - 1) / size
}

// Render draws the pager bar; returns true if the page or page size changed.
func (inst *Pager) Render() bool {
	ids := inst.ids
	ids.Reset()

	numPages := inst.NumPages()
	maxPageIncl := numPages - 1

	// The jump DragValue is bound via r9 U64: end-of-frame Sync writes Rust's
	// view of jumpValue back every frame. When the user dragged, that's the
	// dragged value; otherwise it's the value we sent. Comparing against
	// lastSentJumpValue tells the two apart, so the jumpValue→currentPage
	// derive only fires on a real edit — deriving unconditionally would revert
	// a button-driven page change from the previous frame.
	if inst.jumpValue == 0 {
		inst.jumpValue = 1
	}
	if inst.jumpValue != inst.lastSentJumpValue {
		inst.currentPage = clamp(int64(inst.jumpValue)-1, 0, maxPageIncl)
	} else {
		inst.currentPage = clamp(inst.currentPage, 0, maxPageIncl)
	}

	prevPage := inst.currentPage
	prevSize := inst.pageSize

	for range c.Horizontal().KeepIter() {
		// Jump-to-page input. Sync jumpValue from currentPage right before
		// sending (so a previous-frame button change is what shows) and record
		// it in lastSentJumpValue for the derive at the top of the next Render.
		inst.jumpValue = uint64(inst.currentPage + 1)
		inst.lastSentJumpValue = inst.jumpValue
		c.DragValueU64(ids.PrepareStr("pJump"), inst.jumpValue).
			Speed(0.25).
			Prefix("→ ").
			SendRespVal(&inst.jumpValue)

		c.Separator().Vertical().Send()

		if navBtn(ids, "pFirst", "«") {
			inst.currentPage = 0
		}
		if navBtn(ids, "pPrev", "‹") {
			inst.currentPage = max(0, inst.currentPage-1)
		}

		// Windowed numbered buttons with ellipsis.
		cur := inst.currentPage
		low := max(0, cur-int64(pagerWindowSize-1)/2)
		high := min(maxPageIncl, low+int64(pagerWindowSize-1))
		if high-low < int64(pagerWindowSize-1) {
			low = max(0, high-int64(pagerWindowSize-1))
		}
		if low > 0 {
			c.Label("…").Send()
		}
		for i := low; i <= high; i++ {
			label := "?"
			if i >= 0 && int(i) < len(inst.pageLabels) {
				label = inst.pageLabels[i]
			}
			selected := i == cur
			// Full-height (not .Small()) so the numbered buttons match the
			// jump DragValue and the page-size ComboBox; a uniform control
			// height keeps the centered row from reading as ragged — short
			// buttons floating among tall controls (imzero2 SKILL.md,
			// "Ragged Control Row").
			if c.Button(ids.PrepareSeq(uint64(0x1000+i)),
				c.Atoms().Text(label).Keep()).
				Selected(selected).
				SendResp().HasPrimaryClicked() {
				inst.currentPage = i
			}
		}
		if high < maxPageIncl {
			c.Label("…").Send()
		}

		if navBtn(ids, "pNext", "›") {
			inst.currentPage = min(maxPageIncl, inst.currentPage+1)
		}
		if navBtn(ids, "pLast", "»") {
			inst.currentPage = maxPageIncl
		}

		c.Separator().Vertical().Send()

		pageTag := fmt.Sprintf("page %d / %d", inst.currentPage+1, numPages)
		for rt := range c.RichTextLabel(pageTag) {
			rt.Weak()
		}

		if inst.showSizeCombo {
			c.Separator().Vertical().Send()
			pageSizeLabel := fmt.Sprintf("%d / page", inst.pageSize)
			for range c.ComboBox(ids.PrepareStr("pSize"),
				c.WidgetText().Text(inst.unit+"/page").Keep(),
				c.WidgetText().Text(pageSizeLabel).Keep()).
				KeepIter() {
				for i, opt := range inst.pageSizeOptions {
					selected := opt == inst.pageSize
					if c.Button(ids.PrepareSeq(uint64(0x2000+i)),
						c.Atoms().Text(strconv.FormatInt(opt, 10)).Keep()).
						Frame(false).
						Selected(selected).
						SendResp().HasPrimaryClicked() {
						inst.pageSize = opt
					}
				}
			}
		}

		c.Separator().Vertical().Send()

		// Range annotation. The dash is UTF-8 U+2013.
		start, end := inst.Range()
		annotation := fmt.Sprintf("%s %d–%d of %d", inst.unit, start+1, end, inst.total)
		if inst.total == 0 {
			annotation = "no " + inst.unit
		}
		for rt := range c.RichTextLabel(annotation) {
			rt.Weak()
		}
	}

	// If pageSize changed, keep the previously-visible row in view.
	if inst.pageSize != prevSize {
		mid := prevPage*prevSize + prevSize/2
		inst.currentPage = clamp(mid/inst.pageSize, 0, inst.NumPages()-1)
		inst.Configure(inst.total)
	}
	return inst.currentPage != prevPage || inst.pageSize != prevSize
}

// navBtn draws a first/prev/next/last stepper. Full-height (not .Small()) so
// it shares the row's control height with the jump DragValue, the numbered
// buttons and the page-size ComboBox — see the "Ragged Control Row" note in
// imzero2 SKILL.md.
func navBtn(ids *c.WidgetIdStack, id, label string) bool {
	return c.Button(ids.PrepareStr(id), c.Atoms().Text(label).Keep()).
		SendResp().HasPrimaryClicked()
}

func clamp(v, lo, hi int64) int64 {
	if hi < lo {
		return lo
	}
	return max(lo, min(hi, v))
}

func growStrings(s []string, n int) []string {
	if cap(s) >= n {
		return s[:n]
	}
	return make([]string, n)
}
