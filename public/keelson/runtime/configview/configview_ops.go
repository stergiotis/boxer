package configview

import (
	"sort"
	"strings"

	"github.com/stergiotis/boxer/public/config/env"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
)

// The operations catalog (ADR-0345 M2). Queries read the env-var registry
// (ADR-0009) as the frame drew it, filtered by their own arguments and never
// by the window's filter row; commands set the view and never toggle. A
// sensitive variable's value is withheld from every caller, whatever the
// window's Reveal sensitive state (§SD4): the reveal is not a resource and no
// command sets it.

const (
	resFilter    = "filter"
	resSelection = "selection"
	resExpanded  = "expanded"

	opListVars = "list_vars"
	opGetVar   = "get_var"
	opGetView  = "get_view"

	opSetFilter      = "set_filter"
	opSelectVar      = "select_var"
	opExpandCategory = "expand_category"
)

// Result bounds (§SD2): what one inline result may carry.
const (
	opsMaxRows        = 200
	opsDefaultRows    = 100
	opsMaxValueLen    = 1024
	opsMaxSummaryLen  = 160
	opsMaxAllowedList = 64
)

// filterState is the filter resource's value: the two narrowing choices an
// agent may set. RevealSensitive is deliberately not part of it.
type filterState struct {
	Query   string
	OnlySet bool
}

func (inst *App) filterValue() (f filterState) {
	f = filterState{Query: inst.filter.Query, OnlySet: inst.filter.OnlySet}
	return
}

// applyFilter sets the filter and the checkbox's draft together, so a
// command's write is not mistaken for the person's click on the next frame.
func (inst *App) applyFilter(f filterState) {
	inst.filter.Query, inst.filter.OnlySet = f.Query, f.OnlySet
	inst.onlySetDraft = f.OnlySet
}

// noticeDrafts routes the previous frame's checkbox write-back as the
// person's gesture (ADR-0345 §SD3). The binding landed at that frame's
// sync; a command that set the filter in between also set the draft, so a
// difference here is the person's click and nothing else.
func (inst *App) noticeDrafts() {
	if inst.onlySetDraft != inst.filter.OnlySet {
		inst.setOnlySetGesture(inst.onlySetDraft)
	}
}

// expandedValue is the expanded resource's value: the open categories of
// the last built hierarchy, sorted and joined, so it is comparable.
func (inst *App) expandedValue() (v string) {
	v = strings.Join(inst.expandedCategories(), ",")
	return
}

func (inst *App) expandedCategories() (cats []string) {
	for i := range inst.navNodes {
		n := &inst.navNodes[i]
		if !n.isVar && inst.navState.IsExpanded(int32(i)) {
			cats = append(cats, n.key)
		}
	}
	sort.Strings(cats)
	return
}

// categoryNode finds the section row of cat in the last built hierarchy.
func (inst *App) categoryNode(cat string) (node int32, ok bool) {
	for i := range inst.navNodes {
		n := &inst.navNodes[i]
		if !n.isVar && n.key == cat {
			node, ok = int32(i), true
			return
		}
	}
	return
}

// setCategoryExpanded opens or closes one shown category. The tree keeps
// expansion by key, so the write survives the rebuild the next frame makes.
func (inst *App) setCategoryExpanded(cat string, open bool) (err error) {
	node, ok := inst.categoryNode(cat)
	if !ok {
		err = app.RefuseOperation("no category named " + cat + " is shown; get_view lists the shown categories")
		return
	}
	inst.navState.SetExpanded(node, open)
	return
}

// varRow is one registered variable with its value as it stood when the
// snapshot was taken.
type varRow struct {
	spec env.Spec
	raw  string
	set  bool
}

// opsSnap is what queries read: the registry and the live values as the
// frame drew them, and the view.
type opsSnap struct {
	rows     []varRow
	filter   Filter
	selected string
	expanded []string
	shown    []categoryCount
}

// categoryCount is one shown section with its counts.
type categoryCount struct {
	cat   string
	total int
	set   int
}

func (inst *App) opsSnapshot() (sn opsSnap) {
	specs := env.All()
	sn.rows = make([]varRow, 0, len(specs))
	for _, s := range specs {
		raw, set := lookupValue(s)
		sn.rows = append(sn.rows, varRow{spec: s, raw: raw, set: set})
	}
	sn.filter = inst.filter
	sn.selected = inst.selected
	sn.expanded = inst.expandedCategories()
	for i := range inst.navNodes {
		n := &inst.navNodes[i]
		if n.isVar {
			continue
		}
		cc := categoryCount{cat: n.key}
		for j := i + 1; j < len(inst.navNodes) && inst.navNodes[j].isVar; j++ {
			cc.total++
			if isSet(inst.navNodes[j].spec) {
				cc.set++
			}
		}
		sn.shown = append(sn.shown, cc)
	}
	return
}

// specsOf is the snapshot's specs, for the pure filter.
func (inst opsSnap) specsOf() (specs []env.Spec) {
	specs = make([]env.Spec, 0, len(inst.rows))
	for _, r := range inst.rows {
		specs = append(specs, r.spec)
	}
	return
}

func (inst opsSnap) row(name string) (r varRow, ok bool) {
	for _, c := range inst.rows {
		if c.spec.Name == name {
			r, ok = c, true
			return
		}
	}
	return
}

// filterRows applies the pure filter to the snapshot's rows: the same
// predicate the window uses, over the values the snapshot holds rather than
// a fresh lookup, so a query is a function of its snapshot.
func (inst opsSnap) filterRows(f Filter, category string) (out []varRow) {
	needle := strings.ToLower(strings.TrimSpace(f.Query))
	out = make([]varRow, 0, len(inst.rows))
	for _, r := range inst.rows {
		s := r.spec
		if category != "" && string(s.Category) != category {
			continue
		}
		if needle != "" && !strings.Contains(strings.ToLower(s.Name), needle) &&
			!strings.Contains(strings.ToLower(s.Description), needle) {
			continue
		}
		if f.OnlySet && !r.set {
			continue
		}
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].spec.Category != out[j].spec.Category {
			return out[i].spec.Category < out[j].spec.Category
		}
		return out[i].spec.Name < out[j].spec.Name
	})
	return
}

// ListVarsArgs narrows the registry; every field is optional.
type ListVarsArgs struct {
	Query    string `desc:"case-insensitive substring of the name or the description"`
	OnlySet  bool   `desc:"only variables set in the environment"`
	Category string `desc:"one category, as get_view or list_vars names it; empty keeps every category"`
	Limit    int    `desc:"rows to return, in category then name order; default 100, at most 200"`
}

// VarSummary is one variable as a row of the inspector.
type VarSummary struct {
	Name        string `desc:"the variable's name, as the environment spells it"`
	Category    string `desc:"the category it is filed under"`
	Type        string `desc:"string, bool, int64, duration, path or categorial-string"`
	Set         bool   `desc:"set and non-empty in the environment"`
	Sensitive   bool   `desc:"declared sensitive; its value and default are withheld"`
	Withheld    bool   `desc:"the value and the default were withheld because the variable is sensitive"`
	Value       string `desc:"the raw environment value when set; empty when unset or withheld; cut at 1024 characters"`
	Default     string `desc:"the declared default; empty when none or withheld"`
	CliFlag     string `desc:"the CLI flag that also sets it, when one is declared"`
	Description string `desc:"the description, cut at 160 characters; get_var has it whole"`
}

// ListVarsResult is list_vars's result.
type ListVarsResult struct {
	Matched    int          `desc:"variables that match the arguments"`
	Registered int          `desc:"variables in the registry"`
	Rows       []VarSummary `desc:"the first limit matches, by category then name"`
}

// GetVarArgs names one variable.
type GetVarArgs struct {
	Name string `desc:"the variable's name"`
}

// VarDetail is get_var's result: the summary's fields, the description
// whole, and what the row's tooltips carry.
type VarDetail struct {
	Name        string   `desc:"the variable's name, as the environment spells it"`
	Category    string   `desc:"the category it is filed under"`
	Type        string   `desc:"string, bool, int64, duration, path or categorial-string"`
	Set         bool     `desc:"set and non-empty in the environment"`
	Sensitive   bool     `desc:"declared sensitive; its value and default are withheld"`
	Withheld    bool     `desc:"the value and the default were withheld because the variable is sensitive"`
	Value       string   `desc:"the raw environment value when set; empty when unset or withheld; cut at 1024 characters"`
	Default     string   `desc:"the declared default; empty when none or withheld"`
	CliFlag     string   `desc:"the CLI flag that also sets it, when one is declared"`
	Description string   `desc:"the description, whole"`
	Allowed     []string `desc:"the values a categorial-string accepts; empty otherwise"`
	Package     string   `desc:"the Go package that declared it"`
	Module      string   `desc:"the Go module that declared it"`
}

// CategoryShown is one section of the inspector's outline.
type CategoryShown struct {
	Name     string `desc:"the category"`
	Total    int    `desc:"variables shown under it"`
	Set      int    `desc:"of those, set"`
	Expanded bool   `desc:"the section is open"`
}

// View is get_view's result: the filter row, the selection and the outline
// as the person sees them. Whether sensitive values are revealed to the
// person is not reported.
type View struct {
	Query      string          `desc:"the search box"`
	OnlySet    bool            `desc:"the Only set checkbox"`
	Selected   string          `desc:"the highlighted variable's name; empty for none"`
	Matched    int             `desc:"variables the filter row keeps"`
	Categories []CategoryShown `desc:"the sections drawn, in order, with their open state; empty before the first frame"`
}

// SetFilterArgs sets the filter row whole.
type SetFilterArgs struct {
	Query   string `desc:"case-insensitive substring of the name or the description; empty matches every variable"`
	OnlySet bool   `desc:"only variables set in the environment"`
}

// SelectVarArgs highlights a variable's row.
type SelectVarArgs struct {
	Name string `desc:"a registered variable's name; empty clears the selection"`
}

// ExpandCategoryArgs opens or closes one section.
type ExpandCategoryArgs struct {
	Category string `desc:"a category get_view lists as shown"`
	Expanded bool   `desc:"open when true, closed when false"`
}

func cutText(s string, n int) (out string) {
	out = s
	if len(out) > n {
		out = out[:n] + "…"
	}
	return
}

// summarize renders one row for a caller. The sensitive rule is the
// catalog's, not the window's: the value and the default of a sensitive
// variable are withheld whatever the reveal toggle says, and an unset
// sensitive variable still says it is unset, which carries no secret.
func summarize(r varRow) (s VarSummary) {
	spec := r.spec
	s = VarSummary{Name: spec.Name, Category: string(spec.Category), Type: string(spec.Type), Set: r.set,
		Sensitive: spec.Sensitive, CliFlag: spec.CliFlagName, Description: cutText(spec.Description, opsMaxSummaryLen)}
	if spec.Sensitive {
		s.Withheld = r.set || spec.Default != ""
		return
	}
	if r.set {
		s.Value = cutText(r.raw, opsMaxValueLen)
	}
	s.Default = cutText(spec.Default, opsMaxValueLen)
	return
}

func detail(r varRow) (d VarDetail) {
	s := summarize(r)
	d = VarDetail{Name: s.Name, Category: s.Category, Type: s.Type, Set: s.Set, Sensitive: s.Sensitive,
		Withheld: s.Withheld, Value: s.Value, Default: s.Default, CliFlag: s.CliFlag,
		Description: r.spec.Description, Package: r.spec.Origin.Package, Module: r.spec.Origin.Module}
	if n := len(r.spec.Allowed); n > 0 {
		d.Allowed = append(d.Allowed, r.spec.Allowed[:min(n, opsMaxAllowedList)]...)
	}
	return
}

func knownCategory(sn opsSnap, cat string) (ok bool) {
	for _, r := range sn.rows {
		if string(r.spec.Category) == cat {
			return true
		}
	}
	return
}

func categoryNameList(sn opsSnap) (s string) {
	seen := map[string]bool{}
	names := make([]string, 0, 8)
	for _, r := range sn.rows {
		c := string(r.spec.Category)
		if !seen[c] {
			seen[c] = true
			names = append(names, c)
		}
	}
	sort.Strings(names)
	s = strings.Join(names, ", ")
	return
}

var ops = func() (s *appops.Set[*App, opsSnap]) {
	s = appops.NewSet((*App).opsSnapshot)

	s.Resource(resFilter, "the filter row: search text and Only set; not the Reveal sensitive toggle",
		func(inst *App) any { return inst.filterValue() })
	s.Restorable(resFilter, func(inst *App, v any) bool {
		f, ok := v.(filterState)
		if ok {
			inst.applyFilter(f)
		}
		return ok
	})
	s.Editing(resFilter, func(inst *App) bool { return appops.WidgetEditing(inst.queryH) })
	s.Resource(resSelection, "the highlighted variable's name; empty for none", func(inst *App) any { return inst.selected })
	s.Restorable(resSelection, func(inst *App, v any) bool {
		name, ok := v.(string)
		if ok {
			inst.selected = name
		}
		return ok
	})
	s.Resource(resExpanded, "the open categories of the outline, sorted and comma-joined",
		func(inst *App) any { return inst.expandedValue() })
	s.Restorable(resExpanded, func(inst *App, v any) bool {
		joined, ok := v.(string)
		if !ok {
			return false
		}
		want := map[string]bool{}
		for _, c := range strings.Split(joined, ",") {
			if c != "" {
				want[c] = true
			}
		}
		for i := range inst.navNodes {
			if !inst.navNodes[i].isVar {
				inst.navState.SetExpanded(int32(i), want[inst.navNodes[i].key])
			}
		}
		return true
	})

	// --- queries ---

	appops.Query(s, app.OperationSpec{Name: opListVars, Version: 1,
		Summary: "list registered environment variables matching a substring, a category and Only set, with values; sensitive values withheld; at most 200",
		Agents:  true, Untrusted: true, Fork: app.OperationForkReadShared},
		func(sn opsSnap, in ListVarsArgs) (out ListVarsResult, err error) {
			if in.Category != "" && !knownCategory(sn, in.Category) {
				err = app.RefuseOperation("category is one of " + categoryNameList(sn))
				return
			}
			limit := in.Limit
			switch {
			case limit < 0:
				err = app.RefuseOperation("limit is not negative")
				return
			case limit == 0:
				limit = opsDefaultRows
			case limit > opsMaxRows:
				limit = opsMaxRows
			}
			rows := sn.filterRows(Filter{Query: in.Query, OnlySet: in.OnlySet}, in.Category)
			out.Matched, out.Registered = len(rows), len(sn.rows)
			if len(rows) > limit {
				rows = rows[:limit]
			}
			out.Rows = make([]VarSummary, 0, len(rows))
			for _, r := range rows {
				out.Rows = append(out.Rows, summarize(r))
			}
			return
		})

	appops.Query(s, app.OperationSpec{Name: opGetVar, Version: 1,
		Summary: "read one environment variable: its declaration, whether it is set, and its value unless sensitive",
		Agents:  true, Untrusted: true, Fork: app.OperationForkReadShared},
		func(sn opsSnap, in GetVarArgs) (out VarDetail, err error) {
			r, ok := sn.row(in.Name)
			if !ok {
				err = app.RefuseOperation("no variable named " + in.Name + " is registered; list_vars names them")
				return
			}
			out = detail(r)
			return
		})

	appops.Query(s, app.OperationSpec{Name: opGetView, Version: 1,
		Summary: "read the filter row, the selection and the outline's sections with their open state",
		Agents:  true, Untrusted: true, Fork: app.OperationForkReadShared},
		func(sn opsSnap, _ appops.None) (out View, err error) {
			out = View{Query: sn.filter.Query, OnlySet: sn.filter.OnlySet, Selected: sn.selected}
			out.Matched = len(sn.filterRows(sn.filter, ""))
			open := map[string]bool{}
			for _, c := range sn.expanded {
				open[c] = true
			}
			for _, cc := range sn.shown {
				out.Categories = append(out.Categories, CategoryShown{Name: cc.cat, Total: cc.total, Set: cc.set, Expanded: open[cc.cat]})
			}
			return
		})

	// --- commands ---

	appops.Command(s, app.OperationSpec{Name: opSetFilter, Version: 1,
		Summary: "set the filter row whole: search text and Only set",
		Effect:  app.OperationEffectView, Writes: []string{resFilter}, Agents: true,
		Gesture: "the search box, the Only set checkbox", Rebuild: app.OperationRebuildVolatile},
		func(inst *App, _ app.OperationCall, in SetFilterArgs) (out appops.None, err error) {
			inst.applyFilter(filterState{Query: in.Query, OnlySet: in.OnlySet})
			return
		})

	appops.Command(s, app.OperationSpec{Name: opSelectVar, Version: 1,
		Summary: "highlight a variable's row; an empty name clears the selection",
		Effect:  app.OperationEffectView, Writes: []string{resSelection}, Agents: true,
		Gesture: "a click on a variable row", Rebuild: app.OperationRebuildVolatile},
		func(inst *App, _ app.OperationCall, in SelectVarArgs) (out appops.None, err error) {
			if in.Name != "" {
				if _, ok := env.LookupVar(in.Name); !ok {
					err = app.RefuseOperation("no variable named " + in.Name + " is registered; list_vars names them")
					return
				}
			}
			inst.selected = in.Name
			return
		})

	appops.Command(s, app.OperationSpec{Name: opExpandCategory, Version: 1,
		Summary: "open or close one section of the outline",
		Effect:  app.OperationEffectView, Writes: []string{resExpanded}, Agents: true,
		Gesture: "a section's disclosure control, a click on its row", Rebuild: app.OperationRebuildVolatile},
		func(inst *App, _ app.OperationCall, in ExpandCategoryArgs) (out appops.None, err error) {
			err = inst.setCategoryExpanded(in.Category, in.Expanded)
			return
		})

	return
}()

// Operations serves the catalog for this window (app.OperationsAppI).
func (inst *App) Operations() (h app.OperationsHandlerI) { return ops.Bind(inst) }

// gestureOr routes the person's control through the catalog (ADR-0345
// §SD3), so the command log sees it as theirs, and applies it directly
// where no host serves the catalog (tour, tests).
func gestureOr[In any](inst *App, op string, in In, direct func()) {
	if inst.frameCtx != nil {
		if _, err := appops.Gesture[In, appops.None](inst.frameCtx, op, in); err == nil {
			return
		}
	}
	direct()
}

func (inst *App) setOnlySetGesture(onlySet bool) {
	f := inst.filterValue()
	f.OnlySet = onlySet
	gestureOr(inst, opSetFilter, SetFilterArgs{Query: f.Query, OnlySet: f.OnlySet}, func() { inst.applyFilter(f) })
}

func (inst *App) selectVarGesture(name string) {
	gestureOr(inst, opSelectVar, SelectVarArgs{Name: name}, func() { inst.selected = name })
}

func (inst *App) expandCategoryGesture(cat string, open bool) {
	gestureOr(inst, opExpandCategory, ExpandCategoryArgs{Category: cat, Expanded: open},
		func() { _ = inst.setCategoryExpanded(cat, open) })
}
