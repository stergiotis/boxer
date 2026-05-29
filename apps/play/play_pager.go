//go:build llm_generated_opus47

package play

import (
	"fmt"
	"strconv"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// Pager is a client-side paginator over an in-memory dataset. It owns no data
// itself — the caller feeds it a row total via Configure() and reads back
// (start, end) bounds via Range() for the current page. Adapted from the
// imzero1 DataPagingConfig in the attic, translated to egui2 mechanics.
type Pager struct {
	ids         *c.WidgetIdStack
	total       int64
	pageSize    int64
	currentPage int64
	pageLabels  []string
	// jumpValue mirrors currentPage+1 (human-readable 1-based) between frames
	// and is the r9 U64 databinding target of the DragValueU64 jump input.
	jumpValue uint64
	// lastSentJumpValue is what jumpValue held immediately after the most
	// recent SendRespVal. End-of-frame Sync writes Rust's view of the value
	// back into jumpValue: when the user didn't drag, that's exactly the
	// value we sent, so jumpValue == lastSentJumpValue means "no drag this
	// frame" and the derive-from-jumpValue step at the top of Render must
	// be skipped — otherwise it would clobber any button-driven currentPage
	// update from the previous frame.
	lastSentJumpValue uint64
}

// pageSizeOptions are the buckets shown in the page-size selector. 10 000 is
// a safety cap for "practically all" — egui_table still virtualises within
// the page so even the max is cheap to render.
var pageSizeOptions = []int64{50, 100, 500, 1000, 10000}

const pagerWindowSize = 7 // numbered buttons shown at once (attic default)

// NewPager creates a pager with an initial page size. ids is expected to be
// a fresh WidgetIdStack dedicated to the pager scope.
func NewPager(ids *c.WidgetIdStack, initialPageSize int64) *Pager {
	if initialPageSize <= 0 {
		initialPageSize = 100
	}
	return &Pager{
		ids:      ids,
		pageSize: initialPageSize,
	}
}

// Configure (re)binds the pager to a new row total. If the total or page size
// changes, the page-label cache is rebuilt. The current page is clamped into
// [0, numPages).
func (inst *Pager) Configure(total int64) {
	if total < 0 {
		total = 0
	}
	numPages := inst.numPagesFor(total, inst.pageSize)
	if numPages < 1 {
		numPages = 1 // always at least one "page 1" even for empty results
	}
	if inst.currentPage >= numPages {
		inst.currentPage = numPages - 1
	}
	if inst.total == total && len(inst.pageLabels) == int(numPages) {
		return
	}
	inst.total = total
	inst.pageLabels = growStrings(inst.pageLabels, int(numPages))
	for i := int64(0); i < numPages; i++ {
		inst.pageLabels[i] = strconv.FormatInt(i+1, 10)
	}
}

// Reset snaps back to page 0, preserving the current page size. Call on new
// query results to avoid landing on a page past the end of the new dataset.
// jumpValue is cleared alongside currentPage so the derive-from-jumpValue
// step at the top of Render re-initialises it to 1.
func (inst *Pager) Reset() {
	inst.currentPage = 0
	inst.jumpValue = 0
	inst.lastSentJumpValue = 0
}

// Range returns [start, end) row indices for the current page, already
// clamped to total.
func (inst *Pager) Range() (start, end int64) {
	start = inst.currentPage * inst.pageSize
	end = min64(inst.total, start+inst.pageSize)
	return
}

// CurrentPage returns the 0-indexed current page.
func (inst *Pager) CurrentPage() int64 { return inst.currentPage }

// PageSize returns the active page size.
func (inst *Pager) PageSize() int64 { return inst.pageSize }

// NumPages returns the total number of pages (≥1, even when total==0 for a
// nicer "1 / 1" display).
func (inst *Pager) NumPages() int64 {
	n := inst.numPagesFor(inst.total, inst.pageSize)
	if n < 1 {
		n = 1
	}
	return n
}

func (inst *Pager) numPagesFor(total, size int64) int64 {
	if total <= 0 || size <= 0 {
		return 0
	}
	return (total + size - 1) / size
}

// Render draws the pager bar. Returns true if the page (or page size) changed
// this frame — useful for invalidating cell caches, though the caller usually
// just re-renders regardless.
func (inst *Pager) Render() bool {
	ids := inst.ids
	ids.Reset()

	numPages := inst.NumPages()
	maxPageIncl := numPages - 1

	// The DragValue input is bound via r9 U64: end-of-frame Sync writes
	// Rust's view of jumpValue back into &inst.jumpValue every frame. When
	// the user dragged the widget, that's the dragged value; when they
	// didn't, it's the value we sent at SendRespVal time. We can tell the
	// two apart by comparing against lastSentJumpValue and only apply the
	// jumpValue→currentPage derive when the user really edited it. If we
	// derived unconditionally, a button-driven currentPage change from the
	// previous frame would be reverted here on the next frame, because
	// jumpValue would still hold the pre-button-click value.
	if inst.jumpValue == 0 {
		inst.jumpValue = 1
	}
	if inst.jumpValue != inst.lastSentJumpValue {
		inst.currentPage = clamp64(int64(inst.jumpValue)-1, 0, maxPageIncl)
	} else {
		inst.currentPage = clamp64(inst.currentPage, 0, maxPageIncl)
	}

	prevPage := inst.currentPage
	prevSize := inst.pageSize

	for range c.Horizontal().KeepIter() {
		// Jump-to-page drag/type input — kept leftmost so its width changes
		// (value 1 → 12345 grows a few pixels per digit) only push widgets
		// to the right. The draggable hotspot stays anchored under the
		// cursor, which is the control the user is actively interacting with.
		//
		// Sync jumpValue from currentPage *right before* sending so the
		// previous frame's button-driven currentPage change is what the
		// widget displays. Record the sent value in lastSentJumpValue —
		// the derive step at the top of the next Render compares against
		// it to tell a real user drag (Sync wrote a new value) from a
		// no-op write-back of what we sent (Sync echoed the same value).
		inst.jumpValue = uint64(inst.currentPage + 1)
		inst.lastSentJumpValue = inst.jumpValue
		c.DragValueU64(ids.PrepareStr("pJump"), inst.jumpValue).
			Speed(0.25).
			Prefix("→ ").
			SendRespVal(&inst.jumpValue)

		c.Separator().Vertical().Send()

		// « first
		if smallBtn(ids, "pFirst", "«") {
			inst.currentPage = 0
		}
		// ‹ prev
		if smallBtn(ids, "pPrev", "‹") {
			inst.currentPage = max64(0, inst.currentPage-1)
		}

		// Windowed numbered buttons with ellipsis.
		cur := inst.currentPage
		low := max64(0, cur-int64(pagerWindowSize-1)/2)
		high := min64(maxPageIncl, low+int64(pagerWindowSize-1))
		if high-low < int64(pagerWindowSize-1) {
			low = max64(0, high-int64(pagerWindowSize-1))
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
			if c.Button(ids.PrepareSeq(uint64(0x1000+i)),
				c.Atoms().Text(label).Keep()).
				Small().
				Selected(selected).
				SendResp().HasPrimaryClicked() {
				inst.currentPage = i
			}
		}
		if high < maxPageIncl {
			c.Label("…").Send()
		}

		// › next
		if smallBtn(ids, "pNext", "›") {
			inst.currentPage = min64(maxPageIncl, inst.currentPage+1)
		}
		// » last
		if smallBtn(ids, "pLast", "»") {
			inst.currentPage = maxPageIncl
		}

		c.Separator().Vertical().Send()

		// Current-page label (also helps when ellipsis hides the active
		// number from the windowed button row).
		pageTag := fmt.Sprintf("page %d / %d", inst.currentPage+1, numPages)
		for rt := range c.RichTextLabel(pageTag) {
			rt.Weak()
		}

		c.Separator().Vertical().Send()

		// Page-size ComboBox.
		pageSizeLabel := fmt.Sprintf("%d / page", inst.pageSize)
		for range c.ComboBox(ids.PrepareStr("pSize"),
			c.WidgetText().Text("rows/page").Keep(),
			c.WidgetText().Text(pageSizeLabel).Keep()).
			KeepIter() {
			for i, opt := range pageSizeOptions {
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

		c.Separator().Vertical().Send()

		// Range annotation ("rows A–B of N"). The ellipsis is UTF-8 U+2013.
		start, end := inst.Range()
		annotation := fmt.Sprintf("rows %d–%d of %d",
			start+1, end, inst.total)
		if inst.total == 0 {
			annotation = "no rows"
		}
		for rt := range c.RichTextLabel(annotation) {
			rt.Weak()
		}
	}

	// If pageSize changed, keep the currently-visible row in view by
	// recomputing currentPage from the previous page's midpoint.
	if inst.pageSize != prevSize {
		mid := prevPage*prevSize + prevSize/2
		inst.currentPage = clamp64(mid/inst.pageSize, 0, inst.NumPages()-1)
		// Rebuild labels for the new page count.
		inst.Configure(inst.total)
	}
	return inst.currentPage != prevPage || inst.pageSize != prevSize
}

func smallBtn(ids *c.WidgetIdStack, id, label string) bool {
	return c.Button(ids.PrepareStr(id), c.Atoms().Text(label).Keep()).
		Small().
		SendResp().HasPrimaryClicked()
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func min64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

func clamp64(v, lo, hi int64) int64 {
	if hi < lo {
		return lo
	}
	return max64(lo, min64(hi, v))
}

func growStrings(s []string, n int) []string {
	if cap(s) >= n {
		return s[:n]
	}
	return make([]string, n)
}
