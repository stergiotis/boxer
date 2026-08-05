#!/usr/bin/env bash
#
# Fetch the pinned JSONBench upstream into a work directory and verify every
# file this trial depends on against the hashes recorded in PIN.md.
#
# Upstream is CC BY-NC-SA 4.0 and this repository is MIT, so nothing from it is
# copied into the tree (see PIN.md § Licence). The pin plus these hashes is what
# makes a later run byte-comparable with an earlier one.
#
# Usage:  JSONBENCH_WORK=<dir> ./fetch-pin.sh
# Prints the resolved work directory on success; exits non-zero on any mismatch.

set -euo pipefail

PIN=e6c7c98dc766394d51f7d506a3dd2b5d51165d70
REPO=https://github.com/ClickHouse/JSONBench.git
WORK="${JSONBENCH_WORK:?set JSONBENCH_WORK to a scratch directory outside the repo}"

if [[ ! -d "$WORK/.git" ]]; then
  git clone --quiet "$REPO" "$WORK"
fi
git -C "$WORK" fetch --quiet origin "$PIN" 2>/dev/null || git -C "$WORK" fetch --quiet origin
git -C "$WORK" checkout --quiet "$PIN"

# Files this trial depends on, and their SHA-256 at the pin.
cd "$WORK"
sha256sum --quiet --check --strict <<'EOF'
fe77b96dc6d63845ef5e80a619e21ca7c86101197e004f7829d92410cf6e5467  clickhouse/ddl.sql
86c590b7900fd7e7cceffcea7c1e44c24d996021e9ec1708f2435a863aef5c36  clickhouse/queries.sql
724f79ebb67af5aeec43a3cafc63f1cef0aad599798aec40fbff34f9c06296d5  clickhouse/run_queries.sh
424ae9113413d23f2fafb6f8d5a72ae2678bb95809b79d13c76bf0f0fdbde5b6  clickhouse/load_data.sh
27b44e14fe1c9081f14c8a0500678cd8bf2a02e684f76a4036c3cd6725243681  clickhouse/create_and_load.sh
479269db1bde53fe6cb92aa6dc5de662961ee1996a217076a2b45fadcddc8538  clickhouse/data_size.sh
642cf17dda93c3ca4e0a5dd1f9d8e3da132db3cd2b6b1dc8d6583f178fdede3a  clickhouse/index_size.sh
698e57b316e5326ced180137153ff8a1fddf7ba898e1fd1f1909a7cff0cc9bbc  clickhouse/total_size.sh
8b8db92f0b36f2a05f339962bf860a4b3503b27846a2b18d30095108b05be1b9  clickhouse/index_usage.sh
d5140009d0928b2787f1c1ac096a322b154d160303ae35ffba1cab5cb7a29077  download_data.sh
c8fed02ab266d9ac4caf959d5c0b8bea04088fd1b1f9d402556e4cb4fda6f629  clickhouse/results/m6i.8xlarge_bluesky_1m.json
EOF

echo "$WORK"
