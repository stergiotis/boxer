package agentschema

// The journal relations (ADR-0313 §SD1), declared over the plain-shaped
// journal tables the introspection registry serves (ADR-0308 §SD10):
// keelson('agent_run_entries') and keelson('agent_runs'), by the column
// names those tables carry.

// The tables and the scope setting the journal relations read.
const (
	JournalEntries = "agent_run_entries"
	JournalRuns    = "agent_runs"
	// ScopeSetting is the custom setting the agent principal's profile
	// constrains (§SD2); the server's custom_settings_prefixes must admit
	// its prefix.
	ScopeSetting = "custom_agent_run"
	// ScopeConversation and ScopeApp are the settings my_conversation and
	// agent_history are pinned by.
	ScopeConversation = "custom_agent_conversation"
	ScopeApp          = "custom_agent_app"
)

// entryColumns are the journal entry table's columns, in its order; a
// test pins them to keelson('agent_run_entries') as agentrun serves it.
var entryColumns = []string{"run", "ordinal", "at_ms", "kind", "key", "role", "body", "calls", "body_digest", "calls_digest", "name", "book", "form", "target", "from",
	"effect", "turn", "label_level", "label_tier", "label_exposure", "label_origin", "writer_run", "writer_turn", "writer_ordinal",
	"prev", "digest", "key_id", "parent", "attachments"}

// contentColumns are the columns that carry content; agent_history
// replaces them by sizes, and passes the digests the chain keeps of the
// body and the calls (ADR-0314 §SD5).
var contentColumns = map[string]bool{"body": true, "calls": true, "attachments": true}

func passAll() (rules []SectionRule) {
	for _, c := range entryColumns {
		rules = append(rules, SectionRule{Name: c, Pass: true})
	}
	return
}

func contentFree() (rules []SectionRule) {
	for _, c := range entryColumns {
		if contentColumns[c] {
			continue
		}
		rules = append(rules, SectionRule{Name: c, Pass: true})
	}
	rules = append(rules,
		SectionRule{Name: "body_bytes", Pass: true, Expr: "length(base.`body`)"},
		SectionRule{Name: "calls_bytes", Pass: true, Expr: "length(base.`calls`)"},
		SectionRule{Name: "attachments_bytes", Pass: true, Expr: "length(base.`attachments`)"})
	return
}

// MyRun is the run the session is pinned to: its entries, every column.
func MyRun() Relation {
	return Relation{Name: "my_run", Over: JournalEntries, Scope: Scope{Setting: ScopeSetting, Column: "run"}, Sections: passAll(),
		Summary: "the pinned run's journal entries, every column"}
}

// MyConversation is the conversation the run belongs to: the earlier
// runs' entries, the pinned run's left to my_run, bodies included, since
// inside the cell they are the person's to see, bounded by budget not by
// label.
func MyConversation() Relation {
	return Relation{Name: "my_conversation", Over: JournalEntries, Scope: Scope{Setting: ScopeConversation, Column: "run",
		Via: &ScopeVia{Table: JournalRuns, Key: "id", Column: "conversation"}, Except: ScopeSetting}, Sections: passAll(),
		Summary: "the pinned conversation's earlier runs' entries, bodies included"}
}

// AgentHistory is a declared scope's history, content-free: kinds, keys,
// effects, digests, labels and sizes, no body.
func AgentHistory() Relation {
	return Relation{Name: "agent_history", Over: JournalEntries, Scope: Scope{Setting: ScopeApp, Column: "run",
		Via: &ScopeVia{Table: JournalRuns, Key: "id", Column: "owner_app"}}, Sections: contentFree(),
		Summary: "the scope's runs' entries without content: kinds, keys, effects, digests, labels, sizes"}
}

// JournalRelations are the three, in §SD1's order.
func JournalRelations() []Relation { return []Relation{MyRun(), MyConversation(), AgentHistory()} }
