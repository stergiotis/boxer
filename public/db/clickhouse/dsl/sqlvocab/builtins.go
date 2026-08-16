package sqlvocab

// builtins.go is the curated table of ClickHouse built-ins whose argument
// positions have a *closed* domain (ADR-0190 §SD4).
//
// It exists because no roster declares ClickHouse's own functions and
// `system.functions` describes their arguments in prose, not machine-readable
// domains (the probes page §P5). So the handful where a closed domain pays —
// a tuple's element names, a type name, a time zone, a dictionary attribute, a
// setting — are written out here.
//
// It is hand-curated and lags ClickHouse's function set by design. That is the
// cost ADR-0190 accepted rather than guessing: a built-in absent from this
// table produces no candidates, never wrong ones. Ordinals past the ones a
// domain is declared for are ordinary expressions, which is also what an
// omitted trailing optional argument reads as.
//
// These are NOT registered in a [Registry]: the vocabulary panel reports what
// this build provisions, and ClickHouse's own functions are not that. The
// completion engine consults them separately.

// Builtins returns the curated table, freshly built so a caller may sort or
// extend its copy.
func Builtins() (fns []Function) {
	const fam = "ClickHouse built-in"
	tuple := func(name string) Function {
		return Function{
			Name: name, Where: WhereServer, Family: fam, Available: true,
			Params: []Param{Expr("tuple"), ElementOf("'name'|index", 0), Expr("default")},
			Doc:    "element of a tuple by name or 1-based index; the third argument is returned when the name is absent",
		}
	}
	cast := func(name string) Function {
		return Function{
			Name: name, Where: WhereServer, Family: fam, Available: true,
			Params: []Param{Expr("x"), Lit("'Type'", DomainTypeName)},
			Doc:    "convert x to the named type",
		}
	}
	// A trailing time zone is optional on the whole to*/toStartOf* family; the
	// ordinal it sits at is what differs.
	zoned := func(name string, before []Param, doc string) Function {
		ps := make([]Param, 0, len(before)+1)
		ps = append(ps, before...)
		ps = append(ps, Lit("'timezone'", DomainTimeZone))
		return Function{Name: name, Where: WhereServer, Family: fam, Available: true, Params: ps, Doc: doc}
	}
	dictGet := func(name string) Function {
		return Function{
			Name: name, Where: WhereServer, Family: fam, Available: true,
			Params: []Param{
				Lit("'dictionary'", DomainDictionary),
				Of("'attribute'", DomainDictionaryAttribute, 0),
				Expr("id"),
			},
			Doc: "read an attribute of a loaded dictionary",
		}
	}

	fns = []Function{
		tuple("tupleElement"),
		cast("CAST"),
		cast("accurateCast"),
		cast("accurateCastOrNull"),
		cast("accurateCastOrDefault"),

		zoned("toDateTime", []Param{Expr("x")}, "parse or convert to DateTime in the given zone"),
		zoned("toDateTime64", []Param{Expr("x"), Expr("precision")}, "parse or convert to DateTime64 in the given zone"),
		zoned("toTimeZone", []Param{Expr("t")}, "reinterpret a DateTime in another zone"),
		zoned("toDate", []Param{Expr("x")}, "convert to Date, reading x in the given zone"),
		zoned("toYear", []Param{Expr("t")}, "calendar year of t in the given zone"),
		zoned("toMonth", []Param{Expr("t")}, "calendar month of t in the given zone"),
		zoned("toDayOfMonth", []Param{Expr("t")}, "day of month of t in the given zone"),
		zoned("toHour", []Param{Expr("t")}, "hour of t in the given zone"),
		zoned("toMinute", []Param{Expr("t")}, "minute of t in the given zone"),
		zoned("toStartOfDay", []Param{Expr("t")}, "truncate t to the day in the given zone"),
		zoned("toStartOfHour", []Param{Expr("t")}, "truncate t to the hour in the given zone"),
		zoned("toStartOfMinute", []Param{Expr("t")}, "truncate t to the minute in the given zone"),
		zoned("toStartOfMonth", []Param{Expr("t")}, "truncate t to the month in the given zone"),
		zoned("toStartOfQuarter", []Param{Expr("t")}, "truncate t to the quarter in the given zone"),
		zoned("toStartOfYear", []Param{Expr("t")}, "truncate t to the year in the given zone"),
		zoned("toMonday", []Param{Expr("t")}, "the Monday of t's week in the given zone"),
		zoned("toStartOfInterval", []Param{Expr("t"), Expr("INTERVAL n unit")}, "truncate t to a multiple of an interval in the given zone"),
		zoned("formatDateTime", []Param{Expr("t"), Expr("'format'")}, "render t with a strftime-like format in the given zone"),

		dictGet("dictGet"),
		dictGet("dictGetOrNull"),
		dictGet("dictGetString"),
		dictGet("dictGetUInt64"),
		dictGet("dictGetInt64"),
		dictGet("dictGetFloat64"),
		dictGet("dictGetDate"),
		dictGet("dictGetDateTime"),
		dictGet("dictGetUUID"),
		{
			Name: "dictGetOrDefault", Where: WhereServer, Family: fam, Available: true,
			Params: []Param{
				Lit("'dictionary'", DomainDictionary),
				Of("'attribute'", DomainDictionaryAttribute, 0),
				Expr("id"),
				Expr("default"),
			},
			Doc: "dictGet with an explicit value for an absent key",
		},
		{
			Name: "dictHas", Where: WhereServer, Family: fam, Available: true,
			Params: []Param{Lit("'dictionary'", DomainDictionary), Expr("id")},
			Doc:    "whether the dictionary carries the key",
		},
		{
			Name: "dictGetHierarchy", Where: WhereServer, Family: fam, Available: true,
			Params: []Param{Lit("'dictionary'", DomainDictionary), Expr("id")},
			Doc:    "the key's ancestors in a hierarchical dictionary",
		},

		{
			Name: "getSetting", Where: WhereServer, Family: fam, Available: true,
			Params: []Param{Lit("'setting'", DomainSetting)},
			Doc:    "the current value of a setting",
		},
		{
			Name: "getSettingOrDefault", Where: WhereServer, Family: fam, Available: true,
			Params: []Param{Lit("'setting'", DomainSetting), Expr("default")},
			Doc:    "getSetting with a value for a setting the server does not know",
		},
		{
			Name: "formatRow", Where: WhereServer, Family: fam, Available: true,
			Params: []Param{Lit("'format'", DomainFormat), Expr("x…")},
			Doc:    "render the arguments through a named output format",
		},
		{
			Name: "formatRowNoNewline", Where: WhereServer, Family: fam, Available: true,
			Params: []Param{Lit("'format'", DomainFormat), Expr("x…")},
			Doc:    "formatRow without the trailing newline",
		},
	}
	return
}
