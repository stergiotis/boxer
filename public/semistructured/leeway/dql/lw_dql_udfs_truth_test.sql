-- Truth-table for the leeway DQL helper UDFs (lw_dql_udfs.sql), run through
-- clickhouse-local by TestHelperUDFs_TruthTable. Emits one row per FAILED
-- check (its name); empty output means every check passed.
--
-- Fixtures exercise: scalar value-by-tag, aliasing (a value with >1
-- membership), empty/zero-membership attributes, missing tags, the
-- begin/end/card round-trip, membership-set reads, level-2 array unflatten +
-- list-by-tag, an empty array attribute, and membership-card decoupled from
-- value-length. Decoded by hand against the SoA layout (see EXPLANATION.md).
SELECT chk.1 AS failed_check
FROM (
  SELECT arrayJoin([
    ('a1', a1),('a2', a2),('a3', a3),('a4', a4),('a5', a5),
    ('a6', a6),('a7', a7),('a8', a8),('a9', a9),('a10', a10),
    ('a11', a11),('a12', a12),('a13', a13),('a14', a14),('a15', a15),
    ('b1', b1),('b2', b2),('b3', b3),('b4', b4),('b5', b5),
    ('b6', b6),('b7', b7),('b8', b8),('b9', b9),
    ('c1', c1),('c2', c2),('c3', c3),
    ('d1', d1),('d2', d2),('d3', d3),('d4', d4)
  ]) AS chk
  FROM (
    SELECT
      -- Fixture A: scalar; aliasing (attr1 has 2 memberships); empty middle attr2
      LEEWAY_LU_MEMB_IDX_TO_VAL_IDX([2,0,1,3]) = [1,1,3,4,4,4] AS a1,
      LEEWAY_LU_VAL_BY_MEMB_IDX(['a','b','c','d'],[2,0,1,3]) = ['a','a','c','d','d','d'] AS a2,
      LEEWAY_LU_VAL_IDX_TO_MEMB_IDX_BEGIN_INCL([2,0,1,3]) = [1,0,3,4] AS a3,
      LEEWAY_LU_VAL_IDX_TO_MEMB_IDX_END_EXCL([2,0,1,3]) = [3,0,4,7] AS a4,
      arrayMap((b,e)->e-b, LEEWAY_LU_VAL_IDX_TO_MEMB_IDX_BEGIN_INCL([2,0,1,3]), LEEWAY_LU_VAL_IDX_TO_MEMB_IDX_END_EXCL([2,0,1,3])) = [2,0,1,3] AS a5,
      LEEWAY_VALUE_BY_TAG_EQUAL(['a','b','c','d'],[10,11,12,13,14,15],10,LEEWAY_LU_MEMB_IDX_TO_VAL_IDX([2,0,1,3])) = 'a' AS a6,
      LEEWAY_VALUE_BY_TAG_EQUAL(['a','b','c','d'],[10,11,12,13,14,15],11,LEEWAY_LU_MEMB_IDX_TO_VAL_IDX([2,0,1,3])) = 'a' AS a7,
      LEEWAY_VALUE_BY_TAG_EQUAL(['a','b','c','d'],[10,11,12,13,14,15],12,LEEWAY_LU_MEMB_IDX_TO_VAL_IDX([2,0,1,3])) = 'c' AS a8,
      LEEWAY_VALUE_BY_TAG_EQUAL(['a','b','c','d'],[10,11,12,13,14,15],15,LEEWAY_LU_MEMB_IDX_TO_VAL_IDX([2,0,1,3])) = 'd' AS a9,
      LEEWAY_VALUE_BY_TAG_EQUAL(['a','b','c','d'],[10,11,12,13,14,15],99,LEEWAY_LU_MEMB_IDX_TO_VAL_IDX([2,0,1,3])) = '' AS a10,
      LEEWAY_LU_ATTR_BY_TAG([10,11,12,13,14,15],12,LEEWAY_LU_MEMB_IDX_TO_VAL_IDX([2,0,1,3])) = 3 AS a11,
      LEEWAY_LU_ATTR_BY_TAG([10,11,12,13,14,15],99,LEEWAY_LU_MEMB_IDX_TO_VAL_IDX([2,0,1,3])) = 0 AS a12,
      LEEWAY_LU_MEMBS_OF_VAL_IDX([10,11,12,13,14,15],[2,0,1,3],1) = [10,11] AS a13,
      empty(LEEWAY_LU_MEMBS_OF_VAL_IDX([10,11,12,13,14,15],[2,0,1,3],2)) AS a14,
      LEEWAY_LU_MEMBS_OF_VAL_IDX([10,11,12,13,14,15],[2,0,1,3],4) = [13,14,15] AS a15,
      -- Fixture B: homogenous array (level 2); B7-9 empty array attribute
      LEEWAY_LU_MEMB_IDX_TO_VAL_IDX([1,1,1]) = [1,2,3] AS b1,
      LEEWAY_UNFLATTEN(['x1','x2','y1','y2','y3','z1'],[2,3,1]) = [['x1','x2'],['y1','y2','y3'],['z1']] AS b2,
      LEEWAY_LIST_BY_TAG_EQUAL(['x1','x2','y1','y2','y3','z1'],[2,3,1],[100,200,300],100,LEEWAY_LU_MEMB_IDX_TO_VAL_IDX([1,1,1])) = ['x1','x2'] AS b3,
      LEEWAY_LIST_BY_TAG_EQUAL(['x1','x2','y1','y2','y3','z1'],[2,3,1],[100,200,300],200,LEEWAY_LU_MEMB_IDX_TO_VAL_IDX([1,1,1])) = ['y1','y2','y3'] AS b4,
      LEEWAY_LIST_BY_TAG_EQUAL(['x1','x2','y1','y2','y3','z1'],[2,3,1],[100,200,300],300,LEEWAY_LU_MEMB_IDX_TO_VAL_IDX([1,1,1])) = ['z1'] AS b5,
      empty(LEEWAY_LIST_BY_TAG_EQUAL(['x1','x2','y1','y2','y3','z1'],[2,3,1],[100,200,300],999,LEEWAY_LU_MEMB_IDX_TO_VAL_IDX([1,1,1]))) AS b6,
      LEEWAY_UNFLATTEN(['x1','x2','z1'],[2,0,1]) = [['x1','x2'],[],['z1']] AS b7,
      empty(LEEWAY_LIST_BY_TAG_EQUAL(['x1','x2','z1'],[2,0,1],[100,200,300],200,LEEWAY_LU_MEMB_IDX_TO_VAL_IDX([1,1,1]))) AS b8,
      LEEWAY_LIST_BY_TAG_EQUAL(['x1','x2','z1'],[2,0,1],[100,200,300],300,LEEWAY_LU_MEMB_IDX_TO_VAL_IDX([1,1,1])) = ['z1'] AS b9,
      -- Fixture C: array + aliasing, membership-card ([2,1]) decoupled from value-length ([3,1])
      LEEWAY_LU_MEMB_IDX_TO_VAL_IDX([2,1]) = [1,1,2] AS c1,
      LEEWAY_LIST_BY_TAG_EQUAL(['p1','p2','p3','q1'],[3,1],[10,11,20],11,LEEWAY_LU_MEMB_IDX_TO_VAL_IDX([2,1])) = ['p1','p2','p3'] AS c2,
      LEEWAY_LIST_BY_TAG_EQUAL(['p1','p2','p3','q1'],[3,1],[10,11,20],20,LEEWAY_LU_MEMB_IDX_TO_VAL_IDX([2,1])) = ['q1'] AS c3,
      -- Fixture D: single-attribute / zero-membership edges
      LEEWAY_LU_MEMB_IDX_TO_VAL_IDX([1]) = [1] AS d1,
      LEEWAY_LU_MEMB_IDX_TO_VAL_IDX([3]) = [1,1,1] AS d2,
      empty(LEEWAY_LU_MEMB_IDX_TO_VAL_IDX([0])) AS d3,
      (LEEWAY_LU_VAL_IDX_TO_MEMB_IDX_BEGIN_INCL([0]) = [0] AND LEEWAY_LU_VAL_IDX_TO_MEMB_IDX_END_EXCL([0]) = [0]) AS d4
  )
)
WHERE chk.2 = 0
ORDER BY failed_check;
