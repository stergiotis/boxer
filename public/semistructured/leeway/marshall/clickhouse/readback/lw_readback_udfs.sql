-- Leeway DQL helper UDFs — jagged-array (SoA) read-back primitives.
-- Consolidated from a downstream consumer's tag UDFs (with the inherited
-- BEGIN_INCL bug fixed: it referenced an undefined _END) and extended with
-- level-2 (value array/set) extraction. The family layers on the co/ragged
-- function pack (ADR-0162): HelperUDFsSQL() emits the pack's statements
-- ahead of this file, and level-2 unflattening is the pack's RAGGED_NEST.
--
-- A tagged section stores, per entity row, parallel arrays: a value array
-- (one element per attribute for scalar sections; a flattened element array
-- partitioned by `lencol` for array/set sections); per channel a membership
-- array flattened across attributes; and a per-attribute membership-count
-- column `cardcol` (lvcard/lrcard/…). "val idx" = attribute index (1-based);
-- "memb idx" = flattened membership position (1-based).

-- Per-attribute membership-index range [begin, begin+card) into the flattened
-- membership array, from the per-attribute cardinality column.
CREATE OR REPLACE FUNCTION LEEWAY_LU_VAL_IDX_TO_MEMB_IDX_END_EXCL AS (cardcol) ->
    arrayMap((s, c) -> (c > 0) * (s + 1), arrayCumSum(cardcol), cardcol);
CREATE OR REPLACE FUNCTION LEEWAY_LU_VAL_IDX_TO_MEMB_IDX_BEGIN_INCL AS (cardcol) ->
    arrayMap((s, c) -> (c > 0) * (s - c + 1), arrayCumSum(cardcol), cardcol);

-- The inverse map (flattened membership position -> owning attribute index)
-- is the pack's RAGGED_PARENT_IDS: [2,0,1,3] -> [1,1,3,4,4,4].

-- Value broadcast: each membership position carries its owning attribute's
-- value. (['a','b','c','d'], [2,0,1,3]) -> ['a','a','c','d','d','d'].
CREATE OR REPLACE FUNCTION LEEWAY_LU_VAL_BY_MEMB_IDX AS (valcol, cardcol) ->
    arrayFlatten(arrayMap((i, l) -> arrayWithConstant(l, valcol[i]), arrayEnumerate(cardcol), cardcol));

-- Locate: attribute index carrying membership `tagval`, or 0 if absent.
-- `m2v` = RAGGED_PARENT_IDS(cardcol), computed once per row.
CREATE OR REPLACE FUNCTION LEEWAY_LU_ATTR_BY_TAG AS (tagcol, tagval, m2v) ->
    m2v[indexOf(tagcol, tagval)];

-- Membership set an attribute plays on a channel (aliasing-aware).
CREATE OR REPLACE FUNCTION LEEWAY_LU_MEMBS_OF_VAL_IDX AS (membcol, cardcol, validx) ->
    arraySlice(membcol, LEEWAY_LU_VAL_IDX_TO_MEMB_IDX_BEGIN_INCL(cardcol)[validx], cardcol[validx]);

-- Scalar value of the attribute tagged with `tagval`; default ('' / 0) if absent.
CREATE OR REPLACE FUNCTION LEEWAY_VALUE_BY_TAG_EQUAL AS (valcol, tagcol, tagval, m2v) ->
    valcol[LEEWAY_LU_ATTR_BY_TAG(tagcol, tagval, m2v)];

-- Level-2 unflatten (flattened value array -> array-of-arrays by the length
-- column) is the pack's RAGGED_NEST, emitted ahead of this file.

-- List (array/set) value of the attribute tagged with `tagval`; [] if absent.
-- `lencol` is the per-attribute element-count support column (len/card).
CREATE OR REPLACE FUNCTION LEEWAY_LIST_BY_TAG_EQUAL AS (valFlat, lencol, tagcol, tagval, m2v) ->
    arraySlice(valFlat,
               LEEWAY_LU_VAL_IDX_TO_MEMB_IDX_BEGIN_INCL(lencol)[LEEWAY_LU_ATTR_BY_TAG(tagcol, tagval, m2v)],
               lencol[LEEWAY_LU_ATTR_BY_TAG(tagcol, tagval, m2v)]);
