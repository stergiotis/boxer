// Package ladingsql is the SQL surface of the lading store (ADR-0198 §SD7):
// two table-function macros that turn a mount id into a readable relation.
//
//	SELECT path, size FROM fs(12345) WHERE NOT is_dir ORDER BY size DESC LIMIT 20
//	SELECT path, line0, data FROM fsdata(12345) WHERE match(data, 'TODO')
//
// `fs(m)` is a mount's entries, `fsdata(m)` its blocks. Both expand — before
// the statement leaves the process — into a parenthesised subquery over the
// store's tables, so the enclosing query keeps its own WHERE, ORDER BY and
// LIMIT and nothing about the row shape has to be typed by hand.
//
// # Three things ride in every expansion
//
//   - **The completeness rule.** `fs(m)` without a snapshot means the newest
//     *complete* one, and complete means "has a root row" — which is what
//     `fssnap` holds, so the sub-select that picks it cannot pick a walk that
//     died half way (§SD6).
//   - **The logical cutoff.** `TTL` reclaims space at merge time, so a row can
//     outlive its expiry on disk. Every expansion carries `expiresAt > now()`
//     on the same column the `TTL` names, so results and disk usage can only
//     ever diverge in disk usage (§SD4).
//   - **The capability check.** A mount not visible to the caller is refused
//     here, at expansion, rather than filtered later — see [MountVisibilityI].
//
// # What the columns are
//
// Logical names, not physical ones: `path`, `snap`, `size`, `mtime`,
// `content_hash` and the rest, assembled from the entry kind's generated
// Projection so the SQL surface and the Go decode cannot drift about what a
// column means. The tree columns (`name`, `dir`, `depth`, `ext`) come off the
// materialised columns; `is_dir` and `is_symlink` off the stored node kind,
// which is why that kind is stored rather than derived from the mode bits.
package ladingsql

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/antlr4-go/antlr/v4"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// The table-function names the macros use. Lower-case like play's other client
// macros (keelson, docsearch); matching is case- and quoting-insensitive.
const (
	// FuncEntries is the entry relation: one row per node per snapshot.
	FuncEntries = "fs"
	// FuncBlocks is the block relation: one row per stored block.
	FuncBlocks = "fsdata"
	// FuncSnapshots is the snapshot index: one row per complete snapshot,
	// carrying the walk's totals and the policy it applied.
	//
	// A third relation rather than more columns on `fs()`, because the totals
	// are a different component on a different row grain: every entry row has
	// a path, only the root row has a commit record, and a projection that
	// carried both would report a default for every ordinary node.
	FuncSnapshots = "fssnap"
)

// AllSnapshots is the second argument that widens a call from one snapshot to
// every complete one of the mount — `fs(m, '*')`. It is what makes history and
// diff ordinary queries rather than a separate surface.
const AllSnapshots = "*"

// MountVisibilityI decides which mounts a statement may read.
//
// The check happens at expansion, before the statement reaches a server, and
// the reason it is here rather than in a WHERE clause is that a filter would
// answer "no rows" where a refusal answers "not yours" — and only the second
// is honest about a mount that exists.
//
// A store grant covers a store; which mounts are visible inside it is an id
// set or "every id under a tag" (ADR-0198 §SD3), and this interface is the
// seam either shape plugs into.
type MountVisibilityI interface {
	// VisibleMount reports whether the caller may read this mount. It is
	// consulted once per macro call, so it may be as expensive as a lookup but
	// not as expensive as a query.
	VisibleMount(mount identifier.TaggedId) (visible bool)
}

// VisibleAll grants every mount.
//
// For a caller that has already decided — an in-process tool holding the store
// directly, or a test. It is a named type rather than a nil default so that
// "everything is visible" is something a call site says out loud; a nil
// [Config.Visibility] refuses every mount instead, because a capability check
// that defaults to open is not one.
type VisibleAll struct{}

// VisibleMount implements [MountVisibilityI].
func (VisibleAll) VisibleMount(identifier.TaggedId) bool { return true }

// VisibleSet grants exactly the mounts it lists.
type VisibleSet map[identifier.TaggedId]struct{}

// VisibleMount implements [MountVisibilityI].
func (inst VisibleSet) VisibleMount(mount identifier.TaggedId) (visible bool) {
	_, visible = inst[mount]
	return
}

// VisibleUnderTag grants every mount whose id carries one of the listed tags —
// the "every id under a tag" shape, which is what makes one store safe for many
// owners without an id set that has to be maintained.
type VisibleUnderTag []identifier.IdTag

// VisibleMount implements [MountVisibilityI].
func (inst VisibleUnderTag) VisibleMount(mount identifier.TaggedId) (visible bool) {
	got := mount.GetTag()
	for _, want := range inst {
		if got == want {
			return true
		}
	}
	return false
}

// Config parameterises the expansion.
type Config struct {
	// Database and the three table names. Zero values take the store's own,
	// so a caller that has not moved its tables can leave the whole struct's
	// table fields empty.
	Database  string
	MetaTable string
	DataTable string
	SnapTable string
	// Visibility decides which mounts this statement may read. Nil refuses
	// every mount — see [VisibleAll].
	Visibility MountVisibilityI
}

// ExpandPass rewrites every fs(...) and fsdata(...) table-function call into
// its subquery.
//
// A statement without either macro passes through byte-identical, and an
// expansion contains no macro call, which is what makes the pass idempotent. A
// malformed call — wrong arity, an argument that is not a mount id, a mount the
// caller may not read — errors at expansion so it never reaches a server, the
// same way `keelson()` and `docsearch()` refuse.
func ExpandPass(cfg Config) nanopass.Pass {
	cfg = cfg.withDefaults()
	return nanopass.LiftBodyPass(
		"LadingExpand",
		func(sql string) (string, error) { return expand(cfg, sql) },
		nanopass.PassProperties{
			Idempotent: true,
			Reads:      nanopass.RegionBody,
			Writes:     nanopass.RegionBody,
		},
	)
}

// Expand runs [ExpandPass] over one statement.
func Expand(cfg Config, sql string) (result string, err error) {
	return ExpandPass(cfg).Run(sql)
}

// References reports the mounts sql addresses through either macro, in
// first-appearance order and deduplicated.
//
// It states a fact about the SQL and attaches no meaning to it: a non-empty
// result says the statement reads a lading store, not that it may. Total and
// best-effort — unparseable SQL, macro-free SQL and a malformed call all yield
// nothing rather than an error, because the same statement surfaces a precise
// error when it expands. It is what a dispatcher routes on.
func References(sql string) (mounts []identifier.TaggedId) {
	pr, err := nanopass.Parse(sql)
	if err != nil {
		return nil
	}
	calls := findCalls(pr)
	if len(calls) == 0 {
		return nil
	}
	seen := make(map[identifier.TaggedId]struct{}, len(calls))
	mounts = make([]identifier.TaggedId, 0, len(calls))
	for _, c := range calls {
		mount, _, argErr := callArgs(c.node)
		if argErr != nil {
			continue
		}
		if _, dup := seen[mount]; dup {
			continue
		}
		seen[mount] = struct{}{}
		mounts = append(mounts, mount)
	}
	if len(mounts) == 0 {
		return nil
	}
	return
}

// withDefaults fills the table coordinates a caller left empty.
func (inst Config) withDefaults() (out Config) {
	out = inst
	if out.Database == "" {
		out.Database = defaultDatabase
	}
	if out.MetaTable == "" {
		out.MetaTable = defaultMetaTable
	}
	if out.DataTable == "" {
		out.DataTable = defaultDataTable
	}
	if out.SnapTable == "" {
		out.SnapTable = defaultSnapTable
	}
	return
}

func (inst Config) qualified(table string) string {
	return inst.Database + "." + table
}

// call is one macro call site with the relation it names.
type call struct {
	node *grammar1.TableFunctionExprContext
	rel  relationE
}

// expand replaces every macro call in sql.
func expand(cfg Config, sql string) (result string, err error) {
	pr, err := nanopass.Parse(sql)
	if err != nil {
		err = eh.Errorf("ladingsql: parse: %w", err)
		return
	}
	calls := findCalls(pr)
	if len(calls) == 0 {
		return sql, nil
	}
	rw := nanopass.NewRewriter(pr)
	for _, c := range calls {
		var mount identifier.TaggedId
		var snap snapshotArg
		mount, snap, err = callArgs(c.node)
		if err != nil {
			return "", err
		}
		if cfg.Visibility == nil || !cfg.Visibility.VisibleMount(mount) {
			err = eb.Build().Uint64("mount", mount.Value()).
				Errorf("ladingsql: mount %d is not visible to this caller", mount.Value())
			return "", err
		}
		var sub string
		switch c.rel {
		case relationBlocks:
			sub = cfg.blocksSubquery(mount, snap)
		case relationSnapshots:
			sub = cfg.snapshotsSubquery(mount, snap)
		default:
			sub = cfg.entriesSubquery(mount, snap)
		}
		nanopass.ReplaceNode(rw, c.node, sub)
	}
	return nanopass.GetText(rw), nil
}

// findCalls returns every fs(...) / fsdata(...) table-function call in
// document order.
//
// The predicate lives here alone so the fact extraction ([References]) and the
// rewrite can never drift about what counts as a call — a scalar `fs(1)` in a
// SELECT list is not a TableFunctionExpr and so is invisible to both.
func findCalls(pr *nanopass.ParseResult) (calls []call) {
	nodes := nanopass.FindAll(pr.Tree, func(ctx antlr.ParserRuleContext) bool {
		fn, ok := ctx.(*grammar1.TableFunctionExprContext)
		if !ok {
			return false
		}
		id := fn.Identifier()
		if id == nil {
			return false
		}
		name := nanopass.DecodeIdentifier(id.GetText())
		return relationOf(name) != relationNone
	})
	calls = make([]call, 0, len(nodes))
	for _, n := range nodes {
		fn := n.(*grammar1.TableFunctionExprContext)
		name := nanopass.DecodeIdentifier(fn.Identifier().GetText())
		calls = append(calls, call{node: fn, rel: relationOf(name)})
	}
	return
}

// relationE is which of the three relations a macro name addresses.
type relationE uint8

const (
	relationNone relationE = iota
	relationEntries
	relationBlocks
	relationSnapshots
)

func relationOf(name string) relationE {
	switch {
	case strings.EqualFold(name, FuncEntries):
		return relationEntries
	case strings.EqualFold(name, FuncBlocks):
		return relationBlocks
	case strings.EqualFold(name, FuncSnapshots):
		return relationSnapshots
	}
	return relationNone
}

// snapshotArg is which snapshot (or snapshots) a call names.
type snapshotArg struct {
	// all is fs(m, '*'): every complete snapshot of the mount.
	all bool
	// latest is fs(m): the newest complete one, resolved by the expansion.
	latest bool
	// expr is the SQL expression for one pinned snapshot, when neither.
	expr string
}

// callArgs reads a macro call's arguments.
//
// The mount is a uint64 id — the application's own tagged id, which the store
// never interprets (§SD3). It may be written as a bare integer or as a quoted
// string holding a decimal or 0x-prefixed one, because a 20-digit literal is
// awkward to type and hex is how these ids are usually read.
//
// The snapshot, when given, is either '*' or an instant. A *string* is a
// datetime literal; a *number* is Unix nanoseconds — and those take different
// conversions, which is not a stylistic choice: `toDateTime64(n, 9)` reads a
// plain number as *seconds* whatever the scale says, so nanoseconds handed to
// it saturate to the year 2262 and the predicate silently matches nothing.
func callArgs(fn *grammar1.TableFunctionExprContext) (mount identifier.TaggedId, snap snapshotArg, err error) {
	al := fn.TableArgList()
	if al == nil {
		err = eh.Errorf("ladingsql: %s() needs a mount id", callName(fn))
		return
	}
	args := al.AllTableArgExpr()
	if len(args) < 1 || len(args) > 2 {
		err = eb.Build().Int("args", len(args)).
			Errorf("ladingsql: %s() takes a mount id and an optional snapshot, got %d arguments", callName(fn), len(args))
		return
	}

	raw, isString, ok := literalOf(args[0])
	if !ok {
		err = eh.Errorf("ladingsql: %s() mount must be a literal id, not an expression", callName(fn))
		return
	}
	value, perr := parseMountID(raw, isString)
	if perr != nil {
		err = eh.Errorf("ladingsql: %s(): %w", callName(fn), perr)
		return
	}
	mount = value
	if !mount.IsValid() {
		err = eb.Build().Uint64("mount", mount.Value()).
			Errorf("ladingsql: %s() mount id is not a valid tagged id", callName(fn))
		return
	}

	if len(args) == 1 {
		snap.latest = true
		return
	}
	raw, isString, ok = literalOf(args[1])
	if !ok {
		err = eh.Errorf("ladingsql: %s() snapshot must be a literal, not an expression", callName(fn))
		return
	}
	switch {
	case isString && raw == AllSnapshots:
		snap.all = true
	case isString:
		snap.expr = "toDateTime64(" + quoteLiteral(raw) + ", 9, 'UTC')"
	default:
		var nanos uint64
		nanos, err = strconv.ParseUint(raw, 10, 64)
		if err != nil {
			err = eb.Build().Str("arg", raw).
				Errorf("ladingsql: %s() snapshot must be '*', a datetime literal or Unix nanoseconds", callName(fn))
			return
		}
		snap.expr = fmt.Sprintf("fromUnixTimestamp64Nano(toInt64(%d), 'UTC')", nanos)
	}
	return
}

// parseMountID reads a mount id written bare, quoted, decimal or hexadecimal.
func parseMountID(raw string, isString bool) (mount identifier.TaggedId, err error) {
	text := strings.TrimSpace(raw)
	base := 10
	if lower := strings.ToLower(text); strings.HasPrefix(lower, "0x") {
		text, base = lower[2:], 16
	}
	value, perr := strconv.ParseUint(text, base, 64)
	if perr != nil {
		if isString {
			err = eb.Build().Str("arg", raw).Errorf("mount id %q is not a number; a mount is addressed by its id, and name-as-sugar is not implemented", raw)
			return
		}
		err = eb.Build().Str("arg", raw).Errorf("mount id %q is not a number", raw)
		return
	}
	mount = identifier.TaggedId(value)
	return
}

// literalOf reads one table argument as a literal, reporting whether it was
// quoted.
func literalOf(arg grammar1.ITableArgExprContext) (raw string, isString bool, ok bool) {
	lit := arg.Literal()
	if lit == nil {
		return "", false, false
	}
	text := lit.GetText()
	if len(text) >= 2 && text[0] == '\'' && text[len(text)-1] == '\'' {
		return unquoteLiteral(text[1 : len(text)-1]), true, true
	}
	return text, false, true
}

func callName(fn *grammar1.TableFunctionExprContext) string {
	return nanopass.DecodeIdentifier(fn.Identifier().GetText())
}

// unquoteLiteral undoes the escaping a ClickHouse single-quoted literal
// carries, so an argument is compared as the value the author wrote.
func unquoteLiteral(s string) string {
	return strings.NewReplacer(`\\`, `\`, `\'`, `'`).Replace(s)
}

// quoteLiteral renders a ClickHouse string literal.
func quoteLiteral(s string) string {
	return "'" + strings.NewReplacer(`\`, `\\`, `'`, `\'`, "\x00", `\0`).Replace(s) + "'"
}
