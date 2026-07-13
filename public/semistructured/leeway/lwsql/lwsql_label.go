package lwsql

// BuildLabels maps each leeway physical column name in columnNames to its
// friendly display label — the inverse of the Resolver. It is meant for the
// result side: the SQL sent to ClickHouse keeps physical names, and the UI
// renders these labels in column headers (physical name still on hover).
//
// Every leeway column labels as `section:column`, symmetric with the
// colon-always handle syntax, so what a user reads in a header is exactly what
// they can type. That covers value and support columns alike, and the six
// plain/backbone sections (`id:id`, `routing:naturalKey`, `timestamp:ts`, …).
// A non-leeway result yields nil, so the caller falls back to raw names.
func BuildLabels(columnNames []string) (labels map[string]string) {
	if len(columnNames) == 0 {
		return nil
	}
	infos, ok := classifyColumns(columnNames)
	if !ok {
		return nil // not leeway-shaped — caller uses raw names
	}
	labels = make(map[string]string, len(infos))
	for _, ci := range infos {
		if ci.section == "" {
			continue
		}
		label := ci.section + ":" + ci.column
		if label != ci.physical {
			labels[ci.physical] = label
		}
	}
	if len(labels) == 0 {
		return nil
	}
	return labels
}
