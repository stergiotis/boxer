package sessiondata

import (
	"sort"
	"strings"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
)

// CatalogTableName is the keelson('…') name of every live data set of
// every data session: the operator's view. It shows a definition's digest,
// not its text, and never a ticket; a window or an agent reads its own
// session's definitions through List.
const CatalogTableName = "session_datasets"

type catalogRow struct {
	sessionKind   string
	sessionDigest string
	task          string
	owner         string
	info          DatasetInfo
	defDigest     string
}

func (inst *Service) catalogRows() (rows []catalogRow) {
	inst.mu.Lock()
	sessions := make([]*session, 0, len(inst.byTicket))
	for _, s := range inst.byTicket {
		sessions = append(sessions, s)
	}
	inst.mu.Unlock()
	for _, s := range sessions {
		s.mu.Lock()
		for _, d := range s.datasets {
			rows = append(rows, catalogRow{
				sessionKind: s.kind.String(), sessionDigest: digest(s.ticket), task: s.task,
				owner: string(s.owner.App), info: d.info(), defDigest: digest(d.sql),
			})
		}
		s.mu.Unlock()
	}
	sort.Slice(rows, func(i, j int) bool {
		if c := strings.Compare(rows[i].sessionDigest, rows[j].sessionDigest); c != 0 {
			return c < 0
		}
		return rows[i].info.Name < rows[j].info.Name
	})
	return
}

type catalogProvider struct{ svc *Service }

func newCatalogProvider(svc *Service) (p *catalogProvider) { return &catalogProvider{svc: svc} }

func (inst *catalogProvider) Name() (s string) { return CatalogTableName }

func (inst *catalogProvider) Freshness() (f introspect.FreshnessClass) {
	return introspect.FreshnessLive
}

func (inst *catalogProvider) Schema() (s *arrow.Schema) { return catalogTable(nil).Schema() }

func (inst *catalogProvider) Snapshot(proj introspect.Projection) (rec arrow.RecordBatch, err error) {
	rows := inst.svc.catalogRows()
	return catalogTable(rows).Build(proj, len(rows)), nil
}

func catalogTable(rows []catalogRow) (t *introspect.Table) {
	return introspect.NewTable().
		String("session_kind", func(i int) string { return rows[i].sessionKind }).
		String("session_digest", func(i int) string { return rows[i].sessionDigest }).
		String("task", func(i int) string { return rows[i].task }).
		String("owner", func(i int) string { return rows[i].owner }).
		String("name", func(i int) string { return rows[i].info.Name }).
		Uint64("revision", func(i int) uint64 { return rows[i].info.Revision }).
		String("state", func(i int) string { return rows[i].info.State.String() }).
		String("reason", func(i int) string { return rows[i].info.Reason }).
		Uint64("rows", func(i int) uint64 { return rows[i].info.Rows }).
		Uint64("bytes", func(i int) uint64 { return rows[i].info.Bytes }).
		String("endpoint", func(i int) string { return rows[i].info.Endpoint }).
		String("table", func(i int) string { return rows[i].info.Table }).
		Bool("confined", func(i int) bool { return rows[i].info.Confined }).
		StringList("dependencies", func(i int) []string { return rows[i].info.Dependencies }).
		StringList("attachments", func(i int) []string { return rows[i].info.Attachments }).
		String("definition_digest", func(i int) string { return rows[i].defDigest }).
		Uint64("builds", func(i int) uint64 { return rows[i].info.Builds }).
		Uint64("replays", func(i int) uint64 { return rows[i].info.Replays }).
		Int64("defined_at_unix_us", func(i int) int64 { return rows[i].info.DefinedAt.UnixMicro() }).
		Int64("built_at_unix_us", func(i int) int64 {
			if rows[i].info.BuiltAt.IsZero() {
				return 0
			}
			return rows[i].info.BuiltAt.UnixMicro()
		})
}
