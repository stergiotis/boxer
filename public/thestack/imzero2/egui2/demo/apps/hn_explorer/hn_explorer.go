package hn_explorer

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/config/env"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	runtimeapp "github.com/stergiotis/boxer/public/keelson/runtime/app"
)

// ClickHouseURL is the hn_explorer-specific ClickHouse URL override; the
// upstream demo dataset exposes a separate cluster than the generic
// CLICKHOUSE_URL family.
var ClickHouseURL = env.NewString(env.Spec{
	Name:        "HN_EXPLORER_CLICKHOUSE_URL",
	Description: "ClickHouse URL backing the HN explorer demo; empty disables the demo's live queries",
	Category:    env.CategoryDatabase,
})

var clickHouseUrl = ClickHouseURL.Get()

// --- 1. The Async Arrow Store ---

type ArrowStore struct {
	mu     sync.RWMutex
	record arrow.RecordBatch

	// Column Accessors
	colID    *array.Uint64
	colTitle *array.String
	colType  *array.String
	colBy    *array.String
	colScore *array.Int64
	colText  *array.String

	rowCount int

	// State
	isLoading atomic.Bool
	lastError error
	loadTime  time.Duration
}

func NewArrowStore() *ArrowStore {
	return &ArrowStore{}
}

func (inst *ArrowStore) IsLoading() bool { return inst.isLoading.Load() }

func (inst *ArrowStore) LoadFromClickHouse() {
	if inst.isLoading.Swap(true) {
		return
	}

	inst.mu.Lock()
	inst.lastError = nil
	inst.mu.Unlock()

	go func() {
		defer inst.isLoading.Store(false)

		start := time.Now()
		log.Info().Msg("Starting Fetch from ClickHouse...")

		// Fetch 50k items to demonstrate performance
		query := `
			SELECT id, title, toString(type), by, score, text
			FROM stylometrics.hn_items
			WHERE title != ''
			ORDER BY time DESC
			LIMIT 5000
			FORMAT ArrowStream
		`

		req, _ := http.NewRequest("POST", clickHouseUrl, strings.NewReader(query))
		client := &http.Client{Timeout: 60 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			inst.setError(err)
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != 200 {
			inst.setError(eh.Errorf("clickhouse status %d", resp.StatusCode))
			return
		}

		alloc := memory.NewGoAllocator()
		rdr, err := ipc.NewReader(resp.Body, ipc.WithAllocator(alloc))
		if err != nil {
			inst.setError(err)
			return
		}
		defer rdr.Release()

		// Read Batch
		if rdr.Next() {
			rec := rdr.Record()
			rec.Retain()

			inst.mu.Lock()
			if inst.record != nil {
				inst.record.Release()
			}

			inst.record = rec
			inst.rowCount = int(rec.NumRows())

			// Map Columns
			inst.colID = rec.Column(0).(*array.Uint64)
			inst.colTitle = rec.Column(1).(*array.String)
			inst.colType = rec.Column(2).(*array.String)
			inst.colBy = rec.Column(3).(*array.String)
			inst.colScore = rec.Column(4).(*array.Int64)
			inst.colText = rec.Column(5).(*array.String)

			inst.loadTime = time.Since(start)
			inst.mu.Unlock()

			log.Info().Int("rows", inst.rowCount).Dur("loadTime", inst.loadTime).Msg("loading from clickhouse completed")
		}
	}()
}

func (inst *ArrowStore) setError(err error) {
	inst.mu.Lock()
	inst.lastError = err
	inst.mu.Unlock()
	log.Warn().Err(err).Msg("an error occurred")
}

// GetView generates indices based on filters.
func (inst *ArrowStore) GetView(filterText string, typeFilter string, sortMode string) []int {
	inst.mu.RLock()
	defer inst.mu.RUnlock()

	if inst.record == nil {
		return []int{}
	}

	indices := make([]int, 0, 1000) // Capacity hint

	// Filter Loop (Columnar scan)
	for i := 0; i < inst.rowCount; i++ {
		// 1. Check Type
		if typeFilter != "all" {
			if inst.colType.Value(i) != typeFilter {
				continue
			}
		}

		// 2. Check Text
		if filterText != "" {
			// Case-insensitive check on Title
			if !strings.Contains(strings.ToLower(inst.colTitle.Value(i)), filterText) {
				continue
			}
		}

		indices = append(indices, i)
	}

	// Sort Indices
	sort.Slice(indices, func(i, j int) bool {
		idxA, idxB := indices[i], indices[j]
		if sortMode == "score" {
			return inst.colScore.Value(idxA) > inst.colScore.Value(idxB)
		}
		return inst.colID.Value(idxA) > inst.colID.Value(idxB) // Time desc (proxy via ID)
	})

	return indices
}

// --- 2. Application State ---

// arrowStore is a process-wide query cache shared across every open
// hn_explorer window: ClickHouse round-trips are expensive (≥5k rows,
// ~seconds), so sharing the result avoids the N-windows-N-fetches
// thundering-herd. Per-window UI state lives on *App below.
var arrowStore = NewArrowStore()

// App is the per-window hn_explorer instance. The registry's factory
// ctor allocates a fresh App per Open() so two windows have
// independent filter selections, sort modes, current view modes, and
// row selections.
type App struct {
	// ids is the per-instance WidgetIdStack the host pre-prepares
	// with a window-unique salt every frame (windowhost wraps Frame
	// in c.IdScope keyed on the window key). Captured from
	// MountCtx.Ids() at Mount time. The fresh fallback stack the
	// ctor allocates keeps tests usable without a Mount call.
	ids *c.WidgetIdStack

	// density is read once at newApp; the IDS overlay is applied
	// once at Rust startup with the same env var, so a runtime
	// toggle here would diverge from the visible state. ADR-0032 §SD2.
	density styletokens.DensityE

	currentMode string // "stream" | "focus" | "kanban"
	filterText  string
	filterType  string // "all" | "story" | "job" | "poll"
	sortMode    string // "time" | "score"

	selectedIdx int // index into arrowStore; -1 = no selection
}

var _ runtimeapp.AppI = (*App)(nil)

func newApp() (inst *App) {
	inst = &App{
		ids:         c.NewWidgetIdStack(),
		density:     styletokens.DensityFromEnv(),
		currentMode: "stream",
		filterType:  "all",
		sortMode:    "time",
		selectedIdx: -1,
	}
	return
}

func (inst *App) Manifest() (m runtimeapp.Manifest) { m = manifest; return }
func (inst *App) Mount(ctx runtimeapp.MountContextI) (err error) {
	inst.ids = ctx.Ids()
	return
}
func (inst *App) Unmount(ctx runtimeapp.MountContextI) (err error) { return }

// Frame draws the HN explorer body. The host has already pre-pushed
// a window-unique salt onto inst.ids via c.IdScope, so widget ids
// the renderer derives from inst.ids are scoped under that salt.
func (inst *App) Frame(ctx runtimeapp.FrameContextI) (err error) {
	inst.renderWindow()
	return
}

// --- 3. Render Loop ---

// renderWindow draws the HN explorer body into the caller's UI scope.
// Per ADR-0026 Amendment 2026-05-12, the host wraps this in a runtime-
// created c.Window using Manifest.WindowTitle/Icon; the body uses only
// *Inside panel variants.
func (inst *App) renderWindow() {
	// 1. Top Toolbar
	for range c.PanelTopInside(inst.ids.PrepareStr("top")).DefaultSize(45).KeepIter() {
		inst.renderToolbar()
	}

	// 2. Bottom Status Bar
	for range c.PanelBottomInside(inst.ids.PrepareStr("btm")).DefaultSize(24).Resizable(false).KeepIter() {
		inst.renderStatusBar()
	}

	// 3. Main Content
	viewIndices := arrowStore.GetView(strings.ToLower(inst.filterText), inst.filterType, inst.sortMode)

	switch inst.currentMode {
	case "stream":
		inst.renderStream(viewIndices)
	case "focus":
		// Split View
		for range c.PanelLeftInside(inst.ids.PrepareStr("sidebar")).DefaultSize(350).Resizable(true).KeepIter() {
			inst.renderFocusList(viewIndices)
		}
		inst.renderFocusDetail()
	case "kanban":
		inst.renderKanban(viewIndices)
	}
}

// --- 4. Components ---

func (inst *App) renderToolbar() {
	for range c.Horizontal().KeepIter() {
		// A. Data Control
		if arrowStore.IsLoading() {
			c.Spinner().Size(16).Send()
		} else {
			if c.Button(inst.ids.PrepareStr("reload"), c.Atoms().Text("Reload").Keep()).Small().SendResp().HasPrimaryClicked() {
				arrowStore.LoadFromClickHouse()
			}
		}

		c.Separator().Vertical().Send()

		// B. Mode Switcher
		modeBtn := func(mode, label string) {
			if c.Button(inst.ids.PrepareStr("m_"+mode), c.Atoms().Text(label).Keep()).
				Selected(inst.currentMode == mode).
				SendResp().HasPrimaryClicked() {
				inst.currentMode = mode
			}
		}
		modeBtn("stream", "Stream")
		modeBtn("focus", "Focus")
		modeBtn("kanban", "Kanban")

		c.Separator().Vertical().Send()

		// C. Filters
		// Type Filter (Mock Combo Box logic using buttons for simplicity in this demo)
		// A real implementation would use c.ComboBox
		c.Label("Type:").Send()
		if c.Button(inst.ids.PrepareStr("f_all"), c.Atoms().Text(inst.filterType).Keep()).SendResp().HasPrimaryClicked() {
			// Cycle types for demo
			switch inst.filterType {
			case "all":
				inst.filterType = "story"
			case "story":
				inst.filterType = "job"
			case "job":
				inst.filterType = "poll"
			case "poll":
				inst.filterType = "all"
			}
		}

		c.Label("Search:").Send()
		c.TextEdit(inst.ids.PrepareStr("search"), inst.filterText, false).DesiredWidth(120).SendRespVal(&inst.filterText)
	}
}

func (inst *App) renderStatusBar() {
	arrowStore.mu.RLock()
	total := arrowStore.rowCount
	time := arrowStore.loadTime
	err := arrowStore.lastError
	arrowStore.mu.RUnlock()

	for range c.Horizontal().KeepIter() {
		if err != nil {
			c.Label(fmt.Sprintf("Error: %v", err)).Send()
		} else {
			c.Label(fmt.Sprintf("Total Rows: %d", total)).Send()
			c.Separator().Vertical().Send()
			c.Label(fmt.Sprintf("Load Time: %v", time)).Send()
			c.Separator().Vertical().Send()
			if inst.selectedIdx >= 0 {
				c.Label(fmt.Sprintf("Selected Index: %d", inst.selectedIdx)).Send()
			} else {
				c.Label("No Selection").Send()
			}
		}
	}
}

// --- 5. Views ---

func (inst *App) renderStream(indices []int) {
	arrowStore.mu.RLock()
	defer arrowStore.mu.RUnlock()

	for range c.ScrollArea().Vscroll(true).KeepIter() {
		// SAFETY CAP: Don't render 50k items. Render top 200.
		renderCount := 0
		maxRender := 200

		for _, idx := range indices {
			if renderCount >= maxRender {
				c.Label(fmt.Sprintf("... and %d more items (filter to see more)", len(indices)-maxRender)).Send()
				break
			}
			renderCount++

			id := arrowStore.colID.Value(idx)

			for range c.IdScope(inst.ids.PrepareSeq(id)) {
				inst.renderStreamCard(idx)
				c.Separator().Horizontal().Send()
			}
		}
	}
}

func (inst *App) renderStreamCard(idx int) {
	// Column Access
	title := arrowStore.colTitle.Value(idx)
	score := arrowStore.colScore.Value(idx)
	by := arrowStore.colBy.Value(idx)
	typ := arrowStore.colType.Value(idx)

	for range c.Horizontal().KeepIter() {
		// 1. Score Badge
		c.Button(inst.ids.PrepareStr("sc"), c.Atoms().Text(fmt.Sprintf("%d", score)).Keep()).
			Frame(true).
			Send() // Using Button as a badge (frame=true, non-interactive)

		// 2. Body
		for range c.Vertical().KeepIter() {
			// Title Button
			if c.Button(inst.ids.PrepareStr("ti"), c.Atoms().Text(title).Keep()).
				Frame(false).Wrap().
				SendResp().HasPrimaryClicked() {
				inst.selectedIdx = idx
				inst.currentMode = "focus"
			}

			// Meta Row
			for range c.Horizontal().KeepIter() {
				// Use Small Button (frame=false) instead of Label.Small()
				c.Button(inst.ids.PrepareStr("m1"), c.Atoms().Text(typ).Keep()).Small().Frame(false).Send()
				c.Label("|").Send()
				c.Button(inst.ids.PrepareStr("m2"), c.Atoms().Text(by).Keep()).Small().Frame(false).Send()
			}
		}
	}
}

func (inst *App) renderFocusList(indices []int) {
	arrowStore.mu.RLock()
	defer arrowStore.mu.RUnlock()

	for range c.ScrollArea().Vscroll(true).KeepIter() {
		// Cap the sidebar list too
		count := 0
		limit := 500

		for _, idx := range indices {
			if count >= limit {
				break
			}
			count++

			id := arrowStore.colID.Value(idx)
			title := arrowStore.colTitle.Value(idx)

			for range c.IdScope(inst.ids.PrepareSeq(id)) {
				if c.Button(inst.ids.PrepareStr("r"), c.Atoms().Text(title).Keep()).
					Frame(inst.selectedIdx == idx).
					Selected(inst.selectedIdx == idx).
					Wrap().
					SendResp().HasPrimaryClicked() {
					inst.selectedIdx = idx
				}
				c.Separator().Horizontal().Send()
			}
		}
	}
}

func (inst *App) renderFocusDetail() {
	if inst.selectedIdx < 0 {
		for range c.VerticalCentered().KeepIter() {
			c.Label("Select an item to view details.").Send()
		}
		return
	}

	arrowStore.mu.RLock()
	defer arrowStore.mu.RUnlock()

	// Safe bounds check
	if inst.selectedIdx >= arrowStore.rowCount {
		inst.selectedIdx = -1
		return
	}

	title := arrowStore.colTitle.Value(inst.selectedIdx)
	text := arrowStore.colText.Value(inst.selectedIdx)
	score := arrowStore.colScore.Value(inst.selectedIdx)

	for range c.ScrollArea().Vscroll(true).KeepIter() {
		for range c.Vertical().KeepIter() {
			c.AddSpace(styletokens.GapSections(inst.density))
			c.Label(title).Wrap().Send() // Header
			c.Separator().Horizontal().Send()

			c.Label(fmt.Sprintf("Score: %d", score)).Send()
			c.AddSpace(styletokens.GapItems(inst.density))

			c.Label(text).Wrap().Send()
		}
	}
}

func (inst *App) renderKanban(indices []int) {
	// Bucketize indices based on Type
	// In Arrow/Dataframe land, we'd use a GroupBy. Here we do it manually.
	arrowStore.mu.RLock()
	var stories, jobs, polls []int
	for _, idx := range indices {
		t := arrowStore.colType.Value(idx)
		if t == "story" {
			stories = append(stories, idx)
		}
		if t == "job" {
			jobs = append(jobs, idx)
		}
		if t == "poll" || t == "pollopt" {
			polls = append(polls, idx)
		}
	}
	arrowStore.mu.RUnlock()

	// Layout
	for range c.PanelLeftInside(inst.ids.PrepareStr("col_s")).Resizable(true).DefaultSize(300).KeepIter() {
		inst.renderKanbanCol("Stories", stories)
	}
	for range c.PanelLeftInside(inst.ids.PrepareStr("col_j")).Resizable(true).DefaultSize(300).KeepIter() {
		inst.renderKanbanCol("Jobs", jobs)
	}
	inst.renderKanbanCol("Polls", polls)
}

func (inst *App) renderKanbanCol(title string, indices []int) {
	arrowStore.mu.RLock()
	defer arrowStore.mu.RUnlock()

	for range c.Vertical().KeepIter() {
		c.Label(title).Send()
		c.Separator().Horizontal().Send()

		for range c.ScrollArea().Vscroll(true).KeepIter() {
			count := 0
			limit := 100 // Lower limit for Kanban columns

			for _, idx := range indices {
				if count >= limit {
					break
				}
				count++

				id := arrowStore.colID.Value(idx)
				title := arrowStore.colTitle.Value(idx)

				for range c.IdScope(inst.ids.PrepareSeq(id)) {
					if c.Button(inst.ids.PrepareStr("k_c"), c.Atoms().Text(title).Keep()).
						Frame(true).Wrap().
						SendResp().HasPrimaryClicked() {
						inst.selectedIdx = idx
						inst.currentMode = "focus"
					}
					c.AddSpace(styletokens.GapInline(inst.density))
				}
			}
		}
	}
}
