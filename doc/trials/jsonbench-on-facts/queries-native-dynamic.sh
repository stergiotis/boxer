#!/usr/bin/env bash
#
# Emit the pinned upstream queries with the minimal casts needed to run against
# a JSON column that carries **no type hints** (arm a00).
#
# This is a recorded deviation, not a convenience. With the pinned DDL's typed
# paths removed, every `data.<path>` reference is `Dynamic`, and:
#
#   * `GROUP BY` on a Dynamic column is refused outright — ClickHouse requires
#     `allow_suspicious_types_in_group_by = 1` (Q1, Q2, Q3, Q4, Q5);
#   * `IN [...]` on a Dynamic column is refused with no setting to relax it,
#     so Q3 cannot run at all without a cast;
#   * `fromUnixTimestamp64Micro` likewise rejects a Dynamic argument.
#
# So the benchmark's own queries do not port to high-variability JSON. That is
# a property of the workload, not of any system under test, and it is the point
# of measuring arm a00 at all: a store holding a mixture of document shapes has
# no five paths to declare, and the queries assume five declared paths.
#
# The transformation is a fixed set of substitutions applied to the pinned
# file, so the pin stays authoritative and the delta stays auditable.
#
# Usage: JSONBENCH_WORK=<pin checkout> ./queries-native-dynamic.sh > out.sql

set -euo pipefail
WORK="${JSONBENCH_WORK:?}"

python3 - "$WORK/clickhouse/queries.sql" <<'PY'
import re, sys
src = open(sys.argv[1]).read()
# Cast each hinted path back to the type the pinned DDL declared for it.
# The negative lookahead keeps an already-cast reference (Q4/Q5's
# `data.did::String`) from being cast twice.
casts = {
    r'data\.commit\.collection': 'String',
    r'data\.commit\.operation':  'String',
    r'data\.kind':               'String',
    r'data\.did':                'String',
    r'data\.time_us':            'UInt64',
}
total = 0
for path, ty in casts.items():
    src, n = re.subn(rf'({path})(?!\s*::)', rf'\1::{ty}', src)
    total += n
sys.stderr.write(f"applied {total} casts\n")
sys.stdout.write(src)
PY
