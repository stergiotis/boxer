package chat

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/antlr4-go/antlr/v4"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/analysis"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// Stops (ADR-0370, proposed): a conversation may carry named conditions
// that hold its turn, end it, end the conversation or close its window. A
// stop is one SELECT over four relations of the run (chat_stops_eval.go);
// the rows it returns are its evidence, and it fires on rows the person has
// not acknowledged (chat_stops_fold.go). The named kinds below are
// spellings of statements: nothing evaluates a stop but the engine.
//
// A stop grants nothing and is never shown to the model. It is no security
// control: what a model may do is the grant's and the ceiling's.

// stopActionE is what a stop does when it fires, weakest first. Several
// stops firing at one check combine by maximum.
type stopActionE uint8

const (
	stopActionNone stopActionE = iota
	// stopActionPause holds the turn; the window offers Continue and Stop.
	stopActionPause
	// stopActionAsk holds the turn and asks in the host's queue as well.
	stopActionAsk
	// stopActionStop ends the turn as stopped; the conversation stays open.
	stopActionStop
	// stopActionEnd ends the turn and closes the conversation's run.
	stopActionEnd
	// stopActionClose ends the conversation and closes its window.
	stopActionClose
)

var stopActionNames = map[stopActionE]string{
	stopActionPause: "pause", stopActionAsk: "ask", stopActionStop: "stop", stopActionEnd: "end", stopActionClose: "close",
}

// allStopActions lists the actions a stop may name, weakest first.
var allStopActions = []stopActionE{stopActionPause, stopActionAsk, stopActionStop, stopActionEnd, stopActionClose}

func (inst stopActionE) String() string {
	if s, ok := stopActionNames[inst]; ok {
		return s
	}
	return "none"
}

// holds says the action keeps the turn waiting on the person.
func (inst stopActionE) holds() bool { return inst == stopActionPause || inst == stopActionAsk }

// describe is the action in the words the Stops tab uses.
func (inst stopActionE) describe() string {
	switch inst {
	case stopActionPause:
		return "holds the turn until you continue it here"
	case stopActionAsk:
		return "holds the turn and asks you, here and in the queue"
	case stopActionStop:
		return "ends the turn; the conversation stays open"
	case stopActionEnd:
		return "ends the conversation; the window stays"
	case stopActionClose:
		return "ends the conversation and closes the window"
	}
	return ""
}

func parseStopAction(s string) (a stopActionE, ok bool) {
	for k, name := range stopActionNames {
		if name == s {
			return k, true
		}
	}
	return
}

// Stop is a stop as it is declared: a name, an action, and exactly one
// kind. A chatlet's frontmatter and the Stops tab both write this shape.
type Stop struct {
	// Name is how the record and the person name the stop: lower-case
	// letters, digits and dashes.
	Name string `json:"name"`
	// Then is pause, ask, stop, end or close.
	Then string `json:"then"`
	// After is a duration of the turn's working time, as time.ParseDuration
	// reads it: the stop fires each time that much has passed.
	After string `json:"after,omitempty"`
	// ArtefactBytes fires each time the artefact has grown by that many
	// bytes, counted from empty.
	ArtefactBytes int `json:"artefact_bytes,omitempty"`
	// ArtefactMatch is a pattern; each match in the artefact's text fires.
	ArtefactMatch string `json:"artefact_match,omitempty"`
	// JournalMatch is a pattern over the bodies of this turn's entries of
	// the kinds In names; In defaults to the model's replies and the tool
	// results, and never includes the person's own messages unless named.
	JournalMatch string   `json:"journal_match,omitempty"`
	In           []string `json:"in,omitempty"`
	// ToolMatch is a pattern over the names of the tool calls this turn's
	// replies ask for; ArgumentsMatch, when set, must match the call's
	// arguments as well.
	ToolMatch      string `json:"tool_match,omitempty"`
	ArgumentsMatch string `json:"arguments_match,omitempty"`
	// SQL is the statement itself.
	SQL string `json:"sql,omitempty"`
}

// The bounds on a conversation's stops (ADR-0370 §SD7).
const (
	maxStops       = 16
	maxStopName    = 40
	maxStopSQL     = 4096
	maxStopPattern = 512
	// maxStopRows is how many rows a stop may return: its rows are its
	// identity, so more than a check can carry is an error and not a
	// truncation.
	maxStopRows = 256
	// maxStopShown is how many new rows a firing shows and records in full.
	maxStopShown = 8
	// stopMatchClip bounds the text a named kind puts in a row.
	stopMatchClip = 200
	// minStopAfter keeps a time box from firing at every check.
	minStopAfter = time.Second
)

// The relations a stop's statement may read (chat_stops_eval.go).
const (
	stopRelJournal   = "journal"
	stopRelToolcalls = "toolcalls"
	stopRelArtefact  = "artefact"
	stopRelClock     = "clock"
)

var stopRelations = []string{stopRelJournal, stopRelToolcalls, stopRelArtefact, stopRelClock}

var stopNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// stopOriginE is who declared a stop: a preset's are fixed for the window,
// the person's are theirs to remove.
type stopOriginE uint8

const (
	stopOriginPerson stopOriginE = iota
	stopOriginPreset
)

// stop is a declared stop, constructed: its action, and the statement its
// kind spells.
type stop struct {
	spec   Stop
	then   stopActionE
	kind   string
	origin stopOriginE
	// what says in words when the stop fires, for the person.
	what string
	sql  string
}

// journalKindsDefault are the entries a journal_match reads when In names
// none: what the model wrote and what its tools returned. A match on a
// sentinel word must not fire on the person's own message.
var journalKindsDefault = []string{"reply", "outcome"}

// journalKindsAllowed are the kinds In may name: the entries that carry
// words of the conversation.
var journalKindsAllowed = []string{"user", "reply", "outcome", "note", "completed"}

// compile constructs a stop, refusing a declaration that names no kind or
// several, an unknown action, a pattern that does not compile, or a
// statement SD7 does not admit.
func (inst Stop) compile(origin stopOriginE) (s stop, err error) {
	fail := func(what string) (stop, error) {
		return stop{}, eb.Build().Str("stop", inst.Name).Errorf("stop: %s", what)
	}
	if !stopNameRe.MatchString(inst.Name) || len(inst.Name) > maxStopName {
		return fail("a name is lower-case letters, digits and dashes, at most " + strconv.Itoa(maxStopName))
	}
	then, ok := parseStopAction(inst.Then)
	if !ok {
		return fail("then must be pause, ask, stop, end or close")
	}
	s = stop{spec: inst, then: then, origin: origin}
	kinds := 0
	for _, set := range []bool{inst.After != "", inst.ArtefactBytes != 0, inst.ArtefactMatch != "", inst.JournalMatch != "", inst.ToolMatch != "", strings.TrimSpace(inst.SQL) != ""} {
		if set {
			kinds++
		}
	}
	if kinds != 1 {
		return fail("names exactly one of after, artefact_bytes, artefact_match, journal_match, tool_match and sql")
	}
	if inst.JournalMatch == "" && len(inst.In) > 0 {
		return fail("in belongs to journal_match")
	}
	if inst.ToolMatch == "" && inst.ArgumentsMatch != "" {
		return fail("arguments_match belongs to tool_match")
	}
	switch {
	case inst.After != "":
		d, derr := time.ParseDuration(inst.After)
		if derr != nil || d < minStopAfter {
			return fail("after is a duration of at least " + minStopAfter.String() + ", such as 90s or 15m")
		}
		ms := strconv.FormatInt(d.Milliseconds(), 10)
		s.kind, s.what = "after", "each "+d.String()+" of the turn's working time"
		s.sql = "SELECT turn,\n       intDiv(working_ms, " + ms + ") AS periods\nFROM clock\nWHERE working_ms >= " + ms
	case inst.ArtefactBytes != 0:
		if inst.ArtefactBytes < 0 {
			return fail("artefact_bytes is a positive number")
		}
		n := strconv.Itoa(inst.ArtefactBytes)
		s.kind, s.what = "artefact_bytes", "each "+n+" bytes the artefact reaches"
		s.sql = "SELECT intDiv(bytes, " + n + ") AS steps\nFROM artefact\nWHERE bytes >= " + n
	case inst.ArtefactMatch != "":
		if err = checkStopPattern(inst.Name, inst.ArtefactMatch); err != nil {
			return
		}
		re := sqlString(inst.ArtefactMatch)
		s.kind, s.what = "artefact_match", "each match of "+inst.ArtefactMatch+" in the artefact"
		s.sql = "SELECT n,\n       substring(m[n], 1, " + strconv.Itoa(stopMatchClip) + ") AS match\nFROM (\n  SELECT extractAll(text, " + re + ") AS m\n  FROM artefact\n)\nARRAY JOIN arrayEnumerate(m) AS n"
	case inst.JournalMatch != "":
		if err = checkStopPattern(inst.Name, inst.JournalMatch); err != nil {
			return
		}
		kindsIn := inst.In
		if len(kindsIn) == 0 {
			kindsIn = journalKindsDefault
		}
		quoted := make([]string, 0, len(kindsIn))
		for _, k := range kindsIn {
			if !slices.Contains(journalKindsAllowed, k) {
				return fail("in names " + strconv.Quote(k) + "; the kinds are " + strings.Join(journalKindsAllowed, ", "))
			}
			quoted = append(quoted, sqlString(k))
		}
		re := sqlString(inst.JournalMatch)
		s.kind, s.what = "journal_match", "each "+strings.Join(kindsIn, " or ")+" of the turn that matches "+inst.JournalMatch
		s.sql = "SELECT ordinal, kind,\n       substring(extract(body, " + re + "), 1, " + strconv.Itoa(stopMatchClip) + ") AS match\nFROM journal" +
			"\nWHERE turn = (SELECT turn FROM clock)\n  AND kind IN (" + strings.Join(quoted, ", ") + ")\n  AND match(body, " + re + ")"
	case inst.ToolMatch != "":
		if err = checkStopPattern(inst.Name, inst.ToolMatch); err != nil {
			return
		}
		s.kind, s.what = "tool_match", "each tool call of the turn named like "+inst.ToolMatch
		s.sql = "SELECT ordinal, position, name,\n       substring(arguments, 1, " + strconv.Itoa(stopMatchClip) + ") AS arguments\nFROM toolcalls" +
			"\nWHERE turn = (SELECT turn FROM clock)\n  AND match(name, " + sqlString(inst.ToolMatch) + ")"
		if inst.ArgumentsMatch != "" {
			if err = checkStopPattern(inst.Name, inst.ArgumentsMatch); err != nil {
				return
			}
			s.what += " with arguments like " + inst.ArgumentsMatch
			s.sql += "\n  AND match(arguments, " + sqlString(inst.ArgumentsMatch) + ")"
		}
	default:
		s.kind, s.what = "sql", "the rows its statement returns"
		s.sql = cleanStopSQL(inst.SQL)
	}
	if err = vetStopSQL(inst.Name, s.sql); err != nil {
		return stop{}, err
	}
	return
}

// cleanStopSQL is a statement as a stop keeps it: without the space around
// it and without a closing semicolon, since a check wraps it.
func cleanStopSQL(sql string) string {
	return strings.TrimRight(strings.TrimSpace(sql), "; \t\r\n")
}

// checkStopPattern refuses a pattern that is too long or does not compile.
// The engine evaluates patterns, and Go's syntax is close to its own: a
// pattern Go refuses is refused here, and one only the engine refuses fails
// its check, which stops the turn (ADR-0370 §SD4).
func checkStopPattern(name string, pattern string) (err error) {
	if len(pattern) > maxStopPattern {
		return eb.Build().Str("stop", name).Int("max", maxStopPattern).Errorf("stop: the pattern is too long")
	}
	if _, cerr := regexp.Compile(pattern); cerr != nil {
		return eb.Build().Str("stop", name).Str("pattern", pattern).Errorf("stop: the pattern does not compile: %w", cerr)
	}
	return
}

// stopUnstable are functions whose value differs from one evaluation to
// the next: a stop's rows are its identity (ADR-0370 §SD3), so a statement
// calling one fires on every check. The clock is a relation for that
// reason. The list is what is known to matter, not every such function.
var stopUnstable = []string{
	"now", "now64", "nowinblock", "today", "yesterday", "utctimestamp", "utc_timestamp", "current_timestamp", "current_date", "curdate",
	"rand", "rand32", "rand64", "randcanonical", "randconstant", "randuniform", "randnormal",
	"generateuuidv4", "generateuuidv7", "generateulid", "generatesnowflakeid",
	"sleep", "sleepeachrow",
}

// vetStopSQL admits a statement (ADR-0370 §SD7): one SELECT the security
// classifier of ADR-0132 §SD5 calls a plain read, reading no table but the
// four relations and the names it defines itself. The local worker is an
// engine that can read files and fetch URLs; this is what keeps a stop from
// doing either.
func vetStopSQL(name string, sql string) (err error) {
	fail := func(format string, args ...any) error {
		return eb.Build().Str("stop", name).Errorf("stop: "+format, args...)
	}
	if strings.TrimSpace(sql) == "" {
		return fail("the statement is empty")
	}
	if len(sql) > maxStopSQL {
		return fail("the statement is longer than %d bytes", maxStopSQL)
	}
	if n := len(analysis.SplitStatements(sql)); n != 1 {
		return fail("one statement, not %d", n)
	}
	pr, perr := nanopass.Parse(sql)
	if perr != nil {
		return fail("the statement does not parse as a SELECT: %w", perr)
	}
	class, witnesses, cerr := analysis.ClassifyQuerySecurity(pr)
	if cerr != nil {
		return fail("the statement cannot be classified: %w", cerr)
	}
	if class != analysis.QuerySecurityRead {
		why := class.String()
		if len(witnesses) > 0 {
			why = witnesses[0].Describe() + " " + witnesses[0].Name
		}
		return fail("the statement must only read the run's relations: %s", why)
	}
	// A SETTINGS clause changes how the engine runs the statement; a stop
	// has no use for one, and refusing it leaves the classifier's verdict
	// the whole account of what the statement can reach.
	if len(nanopass.FindAll(pr.Tree, func(ctx antlr.ParserRuleContext) bool { _, is := ctx.(*grammar1.SettingsClauseContext); return is })) > 0 {
		return fail("the statement may carry no SETTINGS clause")
	}
	// The check reads the rows as it asks for them; a FORMAT of the
	// statement's own would only fail later, in the engine.
	if len(nanopass.FindAll(pr.Tree, func(ctx antlr.ParserRuleContext) bool {
		q, is := ctx.(*grammar1.QueryStmtContext)
		return is && q.FORMAT() != nil
	})) > 0 {
		return fail("the statement may carry no FORMAT clause")
	}
	scopes, serr := nanopass.BuildScopes(pr, "")
	if serr != nil {
		return fail("the statement's tables cannot be resolved: %w", serr)
	}
	defined := make(map[string]struct{})
	for _, sc := range nanopass.FlattenScopes(scopes) {
		for _, cte := range sc.CTEDefs {
			defined[cte.Name] = struct{}{}
		}
	}
	for _, ref := range analysis.ExtractTables(pr) {
		_, own := defined[ref.Table]
		if ref.Database != "" || (!own && !slices.Contains(stopRelations, ref.Table)) {
			table := ref.Table
			if ref.Database != "" {
				table = ref.Database + "." + table
			}
			return fail("the statement reads %s; it may read %s", table, strings.Join(stopRelations, ", "))
		}
	}
	for _, fn := range analysis.ExtractFunctions(pr) {
		if slices.Contains(stopUnstable, strings.ToLower(fn.Name)) {
			return fail("%s() differs at every check, so the stop would fire at every check; read the clock relation for time", fn.Name)
		}
	}
	return
}

// stopSet is the stops in force in a conversation: a preset's first, then
// the person's.
type stopSet []stop

// compileStops constructs a list of declarations of one origin, refusing a
// name used twice.
func compileStops(specs []Stop, origin stopOriginE) (set stopSet, err error) {
	for _, spec := range specs {
		s, cerr := spec.compile(origin)
		if cerr != nil {
			return nil, cerr
		}
		set = append(set, s)
	}
	return set, set.check()
}

// check refuses a set that is too large or names a stop twice.
func (inst stopSet) check() (err error) {
	if len(inst) > maxStops {
		return eb.Build().Int("count", len(inst)).Int("max", maxStops).Errorf("stop: too many stops")
	}
	seen := make(map[string]struct{}, len(inst))
	for _, s := range inst {
		if _, dup := seen[s.spec.Name]; dup {
			return eb.Build().Str("stop", s.spec.Name).Errorf("stop: two stops share a name")
		}
		seen[s.spec.Name] = struct{}{}
	}
	return
}

// specs are the set's declarations, for the record.
func (inst stopSet) specs() (out []Stop) {
	for _, s := range inst {
		out = append(out, s.spec)
	}
	return
}

// find is the stop named name.
func (inst stopSet) find(name string) (s stop, ok bool) {
	for _, c := range inst {
		if c.spec.Name == name {
			return c, true
		}
	}
	return
}
