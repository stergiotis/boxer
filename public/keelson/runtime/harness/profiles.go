package harness

import (
	"maps"
	"sort"
	"sync"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
)

// A harness profile is registered where its owner lives (the chat's are
// Go literals in that package) and served as one introspection table for
// the host (ADR-0310 §SD7): keelson('harness_profiles'), one row per
// constant, so a run row's profile id and fingerprint resolve to the
// values that were in force. An owner publishes when its registry is
// first built, which the chat does on its first profile lookup, so the
// table is served live: a read before that is empty and is not cached.

// TableProfiles is keelson('harness_profiles').
const TableProfiles = "harness_profiles"

// ProfileRow is one published profile: its owner, id, fingerprint and
// constants by name.
type ProfileRow struct {
	App         string
	Id          string
	Fingerprint string
	Fields      map[string]string
}

var (
	profilesMu sync.Mutex
	published  []ProfileRow
)

// PublishProfile adds a profile to the table; a second publication of the
// same app and id replaces the first. The fields are copied, so the
// owner's map and the table's never alias.
func PublishProfile(r ProfileRow) {
	r.Fields = maps.Clone(r.Fields)
	profilesMu.Lock()
	defer profilesMu.Unlock()
	for i := range published {
		if published[i].App == r.App && published[i].Id == r.Id {
			published[i] = r
			return
		}
	}
	published = append(published, r)
}

// PublishedProfiles is the table's rows, by app and id, each with its own
// copy of the fields.
func PublishedProfiles() (rows []ProfileRow) {
	profilesMu.Lock()
	defer profilesMu.Unlock()
	rows = make([]ProfileRow, 0, len(published))
	for _, p := range published {
		p.Fields = maps.Clone(p.Fields)
		rows = append(rows, p)
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].App != rows[j].App {
			return rows[i].App < rows[j].App
		}
		return rows[i].Id < rows[j].Id
	})
	return
}

// RegisterIntrospect registers keelson('harness_profiles').
func RegisterIntrospect(reg *introspect.Registry) (err error) {
	return reg.Register(profilesProvider{})
}

type profilesProvider struct{}

func (profilesProvider) Name() string                         { return TableProfiles }
func (profilesProvider) Freshness() introspect.FreshnessClass { return introspect.FreshnessLive }
func (profilesProvider) Schema() *arrow.Schema                { return profilesTable(nil).Schema() }

// profileField is one row of the long table.
type profileField struct {
	app, id, fingerprint, field, value string
}

func (profilesProvider) Snapshot(proj introspect.Projection) (rec arrow.RecordBatch, err error) {
	var rows []profileField
	for _, p := range PublishedProfiles() {
		names := make([]string, 0, len(p.Fields))
		for k := range p.Fields {
			names = append(names, k)
		}
		sort.Strings(names)
		for _, k := range names {
			rows = append(rows, profileField{app: p.App, id: p.Id, fingerprint: p.Fingerprint, field: k, value: p.Fields[k]})
		}
	}
	rec = profilesTable(rows).Build(proj, len(rows))
	return
}

func profilesTable(rows []profileField) *introspect.Table {
	return introspect.NewTable().
		String("app", func(i int) string { return rows[i].app }).
		String("profile", func(i int) string { return rows[i].id }).
		String("fingerprint", func(i int) string { return rows[i].fingerprint }).
		String("field", func(i int) string { return rows[i].field }).
		String("value", func(i int) string { return rows[i].value })
}
