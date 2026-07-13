package lwsql

// BuildLabels maps each leeway physical column name in columnNames to a
// friendly display label. It is meant for the result side: the SQL sent to
// ClickHouse keeps physical names, and the UI renders these labels in column
// headers (physical name still available on hover). A physical name whose label
// equals itself, or a non-leeway result, is omitted so callers fall back to the
// raw name.
//
// Unlike the Resolver's input vocabulary — which is deliberately value-only,
// since those are the columns a user queries by name — labels cover EVERY
// leeway column, value and support alike. A raw `SELECT *` on a leeway table
// returns far more support columns (membership refs, cardinalities, lengths)
// than value columns; leaving those as raw physical names makes the whole table
// read as unlabelled. A support column labels as `section:role`
// (e.g. `symbol:lr`); a value column labels as its handle form — a section's
// sole default `value` column as the bare section, anything else as
// `section:column`.
func BuildLabels(columnNames []string) (labels map[string]string) {
	if len(columnNames) == 0 {
		return nil
	}
	infos, ok := classifyColumns(columnNames)
	if !ok {
		return nil // not leeway-shaped — caller uses raw names
	}
	// Count value columns per section: a section with exactly one is named bare,
	// so its value column elides to the section; otherwise columns keep their
	// section:column form. Support columns never elide.
	valueCountBySection := make(map[string]int, len(infos))
	for _, ci := range infos {
		if ci.isValue && ci.section != "" {
			valueCountBySection[fold(ci.section)]++
		}
	}
	labels = make(map[string]string, len(infos))
	for _, ci := range infos {
		count := 0
		if ci.isValue {
			count = valueCountBySection[fold(ci.section)]
		}
		label := friendlyLabel(ci.section, ci.column, count)
		if label != "" && label != ci.physical {
			labels[ci.physical] = label
		}
	}
	if len(labels) == 0 {
		return nil
	}
	return labels
}

// friendlyLabel renders a (section, column) pair to its display form.
// sectionValueCount is the number of value columns in the section (0 for a
// support column, which never elides). An empty section is a plain/backbone
// column (its bare name). A value column that is its section's sole default
// `value` column labels as the section alone; any other column — a non-default
// value column, one of several value columns, or a support column — labels as
// `section:column`, which for a support column is its `section:role`.
func friendlyLabel(section string, column string, sectionValueCount int) string {
	if section == "" {
		return column
	}
	if sectionValueCount == 1 && fold(column) == "value" {
		return section
	}
	return section + ":" + column
}
