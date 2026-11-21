package chstore

import (
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/storage/recordstore"
)

// WriteTransformed reports whether an insert transform is registered for
// cfg's table (ADR-0402 §SD1): the facts are stored in a deployment's own
// table. Registering the transform is what tells boxer so — there is no
// second setting. Under it boxer issues no DDL for cfg's table and refuses
// the statements that change or empty stored rows by its name (§SD5).
func WriteTransformed(cfg Config) (transformed bool) {
	_, transformed = recordstore.RegisteredInsertTransform(cfg.Database + "." + cfg.Table)
	return
}

// refuseTransformed is the refusal of a statement that assumes cfg's table
// holds the facts, when a transform says it does not; nil otherwise.
func refuseTransformed(cfg Config, what string) (err error) {
	if WriteTransformed(cfg) {
		err = eb.Build().Str("table", cfg.Database+"."+cfg.Table).Str("statement", what).
			Errorf("chstore: %w", recordstore.ErrTransformedTable)
	}
	return
}

// SchemaContracts are the contracts the schema check judges for the facts
// under cfg (ADR-0387, ADR-0402 §SD4):
//
//   - the table written to, by [SchemaContract], unless a transform stores
//     the facts in a deployment's own table, whose schema boxer does not
//     know;
//   - the read relation, judged by its columns only, when it is another
//     relation than the table written to — a read table, or, under a
//     transform, the name boxer.facts itself, which the deployment may have
//     made its view.
func SchemaContracts(cfg Config) (cs []recordstore.TableContract, err error) {
	full, err := SchemaContract(cfg)
	if err != nil {
		return
	}
	transformed := WriteTransformed(cfg)
	if !transformed {
		cs = append(cs, full)
	}
	read := cfg.ReadTable
	if read == "" {
		read = full.Table
	}
	if transformed || read != full.Table {
		rel := full
		rel.Table = read
		rel.Stamp = ""
		rel.Layout = recordstore.TableLayout{}
		rel.LayoutRecipe = ""
		rel.ColumnsOnly = true
		cs = append(cs, rel)
	}
	return
}
