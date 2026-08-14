package capmapfacts

// Physical `boxer.facts` column names, spelled out because the read path is
// SQL: a query has to name leeway columns, and nothing derives them for a
// hand-written one.
//
// That coupling is real and ADR-0168 §SD8 is why the *provider* tables avoid it
// by reading the vault instead. A dump has no such option — it reads what was
// stored — so the names live here, in one place, guarded by
// `TestHandwrittenColumnsMatchGeneratedSchema` (factsschema/ddl), which already
// scans this package and fails when a schema regeneration invalidates one.
//
// # Reading the name
//
// `tv:<section>:<column>:<abbrev>:<type>:<encoding aspects>:<value aspects>:…`.
// The segments after the type are aspect bitmasks in base62, which is why an
// aspect change renames a column and breaks every literal like these. See
// doc/explanation/leeway-column-names.md.
//
// # The support columns matter as much as the value ones
//
// A leeway section stores attributes as flat arrays plus per-attribute
// cardinalities: `lr` is every low-card membership of every attribute
// concatenated, and `lrcard[i]` says how many of them belong to attribute i.
// So the position of a membership in `lr` is NOT the attribute's index — it is
// only equal to it while every preceding attribute carries exactly one. Reading
// through the `card` columns ([membershipAttrs]) is what makes the decode
// independent of the order attributes happened to be written in.
const (
	colId = "`id:id:u64:47::0:`"
	colTs = "`ts:ts:z64:47::0:`"

	colSymValue   = "`tv:symbol:value:val:s:124::I:0::data`"
	colSymLr      = "`tv:symbol:lr:lr:u64:1247:::0::data`"
	colSymLrCard  = "`tv:symbol:lrcard:lrcard:u64:4E:::0::data`"
	colSymLmr     = "`tv:symbol:lmr:lmr:u64:1247:::0::data`"
	colSymLmrCard = "`tv:symbol:lmrcard:lmrcard:u64:4E:::0::data`"
	colSymMrhp    = "`tv:symbol:mrhp:mrhp:y:4:::0::data`"

	colStrValue  = "`tv:stringArray:value:val:sh:4::8:0::data`"
	colStrLr     = "`tv:stringArray:lr:lr:u64:1247:::0::data`"
	colStrLrCard = "`tv:stringArray:lrcard:lrcard:u64:4E:::0::data`"
	colStrLen    = "`tv:stringArray:len:len:u64:4D:::0::data`"

	colTxtValue   = "`tv:textArray:value:val:sh:5::7:0::data`"
	colTxtLmr     = "`tv:textArray:lmr:lmr:u64:1247:::0::data`"
	colTxtLmrCard = "`tv:textArray:lmrcard:lmrcard:u64:4E:::0::data`"
	colTxtMrhp    = "`tv:textArray:mrhp:mrhp:y:4:::0::data`"
	colTxtLen     = "`tv:textArray:len:len:u64:4D:::0::data`"

	colU8Value  = "`tv:u8Array:value:val:u8h:4:::0::data`"
	colU8Lr     = "`tv:u8Array:lr:lr:u64:1247:::0::data`"
	colU8LrCard = "`tv:u8Array:lrcard:lrcard:u64:4E:::0::data`"
	colU8Len    = "`tv:u8Array:len:len:u64:4D:::0::data`"

	colTimeValue   = "`tv:timeArray:value:val:z64h:4:::0::data`"
	colTimeLmr     = "`tv:timeArray:lmr:lmr:u64:1247:::0::data`"
	colTimeLmrCard = "`tv:timeArray:lmrcard:lmrcard:u64:4E:::0::data`"
	colTimeMrhp    = "`tv:timeArray:mrhp:mrhp:y:4:::0::data`"
	colTimeLen     = "`tv:timeArray:len:len:u64:4D:::0::data`"

	colF64Value  = "`tv:f64Array:value:val:f64h:4A:::0::data`"
	colF64Lr     = "`tv:f64Array:lr:lr:u64:1247:::0::data`"
	colF64LrCard = "`tv:f64Array:lrcard:lrcard:u64:4E:::0::data`"
	colF64Len    = "`tv:f64Array:len:len:u64:4D:::0::data`"

	colFkValue  = "`tv:foreignKey:value:val:u64:4:M::0::foreignKey`"
	colFkLr     = "`tv:foreignKey:lr:lr:u64:1247:M::0::foreignKey`"
	colFkLrCard = "`tv:foreignKey:lrcard:lrcard:u64:4E:M::0::foreignKey`"
)
