package providers

import (
	"context"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/applink"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
)

// TableLinkCodecs names the link codecs table.
const TableLinkCodecs = "link_codecs"

// linkCodecsProvider lists every codec that spells a boxer: link
// (ADR-0409): an operation's, from its catalog, and a launch
// kind's, with the alias scheme that stands for it. Which links this host
// can follow, and how each app spells them, is then one query.
type linkCodecsProvider struct{}

func (linkCodecsProvider) Name() string                         { return TableLinkCodecs }
func (linkCodecsProvider) Freshness() introspect.FreshnessClass { return introspect.FreshnessStatic }
func (linkCodecsProvider) Schema() *arrow.Schema                { return linkCodecsTable(nil).Schema() }

func (linkCodecsProvider) Snapshot(ctx context.Context, proj introspect.Projection) (arrow.RecordBatch, error) {
	rows := linkCodecRows(app.AllRegistrations())
	return linkCodecsTable(rows).Build(proj, len(rows)), nil
}

// linkCodecRow is one codec.
type linkCodecRow struct {
	For       string
	App       app.AppIdT
	Operation string
	Version   uint16
	Stage     string
	Kind      string
	Alias     string
	Examples  []string
}

func linkCodecRows(regs []app.Registration) (rows []linkCodecRow) {
	for _, r := range regs {
		m := r.Manifest
		if m.Operations == nil {
			continue
		}
		for _, o := range m.Operations.Operations {
			if o.Link == nil {
				continue
			}
			rows = append(rows, linkCodecRow{For: "operation", App: m.Id, Operation: o.Name, Version: o.Version, Stage: o.LinkStage,
				Examples: applink.Examples(m, o)})
		}
	}
	aliasOf := map[string]string{}
	schemes, kinds := applink.Aliases()
	for i, s := range schemes {
		aliasOf[kinds[i]] = s
	}
	for _, k := range applink.Kinds() {
		row := linkCodecRow{For: "kind", Kind: k, Alias: aliasOf[k]}
		if codec, ok := applink.KindCodec(k); ok {
			for _, ex := range codec.LinkExamples() {
				if tail, err := codec.FormatLink(ex); err == nil {
					row.Examples = append(row.Examples, applink.Format(applink.Envelope{Form: applink.FormKind, Verb: applink.VerbOpen, Kind: k, Tail: tail}))
				}
			}
		}
		rows = append(rows, row)
	}
	return
}

func linkCodecsTable(rows []linkCodecRow) *introspect.Table {
	v := func(i int) *linkCodecRow { return &rows[i] }
	return introspect.NewTable().
		// operation or kind: what the codec spells.
		String("codec_for", func(i int) string { return v(i).For }).
		String("app_id", func(i int) string { return string(v(i).App) }).
		String("operation", func(i int) string { return v(i).Operation }).
		Int32("version", func(i int) int32 { return int32(v(i).Version) }).
		// The command an authorless link to a run calls instead.
		String("link_stage", func(i int) string { return v(i).Stage }).
		String("kind", func(i int) string { return v(i).Kind }).
		// The scheme that stands for the kind's links, e.g. mdedit.
		String("alias_scheme", func(i int) string { return v(i).Alias }).
		StringList("examples", func(i int) []string { return v(i).Examples })
}
