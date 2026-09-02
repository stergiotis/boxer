#!/bin/bash
# lading-demo.sh — fill a lading store with real trees, then say what to ask it.
#
# The store is ADR-0198; the task-oriented walkthrough is
# doc/howto/lading-snapshot-store.md. This script is that walkthrough executed
# against the repository it lives in: it makes sure the tables are there, walks
# five mounts into them, and prints the read paths back out. Every snapshot is
# taken under a whole-day TTL class (7 days by default), so a demo store nobody
# comes back to empties itself.
#
# What it puts in the store, and why each mount is there:
#
#   0x…001  boxer-tree      the repository's tracked files, content stored.
#                           The mount the how-to's SQL examples and its rclone
#                           command name, so those work verbatim afterwards.
#                           Two vendored fonts are over the 4 MiB inline
#                           threshold and land as "ref" — size, mtime and hash,
#                           no bytes.
#   0x…002  boxer-doc       doc/, content stored. Rooted at doc/, so an ADR is
#                           adr/0198-….md — two segments deep, which is what
#                           scripts/dev/tally-scene.sh drives. All text, so
#                           grep with real line numbers has something to find.
#   0x…003  lading-src      public/fs/lading, content stored. A second mount,
#                           so fs('*') and the ledger have more than one row
#                           and a cross-mount diff has a right-hand side.
#   0x…004  boxer-checkout  the checkout as it is on disk — .git and build
#                           output included — with --meta-only. Tens of
#                           thousands of entries describing many GiB, and not
#                           one byte stored: it lists, stats and diffs by size
#                           and mtime, and every read of it is ErrNoContent.
#   0x…005  demo-scratch    a generated tree, snapshotted TWICE. The edge cases
#                           on purpose: a file over the threshold ("ref"), a
#                           text file whose line is longer than a block
#                           (text=false), a symlink, an unreadable file (the
#                           "err" column), and a mutation between the two walks
#                           so history and diff have something to show.
#
# The mount ids are the ones the how-to and the tally scene already name. A real
# application mints its own under its own tag (ADR-0106) and the store claims
# none; a shell script cannot mint, which is why these are constants here.
#
# Running it again adds one more snapshot per mount rather than replacing
# anything, because that is the only way this store changes. Two runs are how
# you get a history and a diff over the repository trees as well.
#
# Usage:
#   scripts/dev/lading-demo.sh                    # every stage
#   scripts/dev/lading-demo.sh doc scratch        # stages matching a pattern
#   LADINGDEMO_LIST=1 scripts/dev/lading-demo.sh  # list the stages, run none
#   LADINGDEMO_DRY=1  scripts/dev/lading-demo.sh  # say what it would walk
#   scripts/dev/lading-demo.sh purge              # delete the demo mounts again
#
# Knobs, all optional: LADINGDEMO_OUT (working directory, default
# tmp/lading-demo), LADINGDEMO_TTL_DAYS (7), LADINGDEMO_BIN (an existing boxer
# binary), LADINGDEMO_BUILD=0 (reuse the one at LADINGDEMO_BIN),
# LADINGDEMO_PROFILE (corpus | fleet — fixed at table creation),
# LADINGDEMO_KEEP_STAGE=1 (keep the staged copy of the tracked tree).
#
# Prerequisites: a reachable ClickHouse (CLICKHOUSE_ENDPOINT, else
# CLICKHOUSE_URL, else http://localhost:8123/), a Go toolchain, and git.
# Nothing else — no display, no Rust client, no rclone. The read paths that
# need a GUI or rclone are printed rather than run, because the fs() macros are
# expanded in-process by the host that issues the query (how-to §5): there is
# no CLI that expands them, so a plain SQL client cannot run them.
set -uo pipefail

here=$(dirname "$(readlink -f "$BASH_SOURCE")")
root=$(cd "$here/../.." && pwd)

# ---------------------------------------------------------------- configuration

OUT="${LADINGDEMO_OUT:-$root/tmp/lading-demo}"
TTL="${LADINGDEMO_TTL_DAYS:-7}"
PROFILE="${LADINGDEMO_PROFILE:-corpus}"
BUILD="${LADINGDEMO_BUILD:-1}"
BIN="${LADINGDEMO_BIN:-$OUT/bin/boxer}"
LIST="${LADINGDEMO_LIST:-0}"
DRY="${LADINGDEMO_DRY:-0}"
KEEP_STAGE="${LADINGDEMO_KEEP_STAGE:-0}"

# The endpoint is resolved the way chclient.ConfigFromEnv resolves it —
# CLICKHOUSE_ENDPOINT, then CLICKHOUSE_URL, then the default — so this script's
# own queries and the store's writes cannot end up talking to two servers.
CH_URL="${CLICKHOUSE_ENDPOINT:-${CLICKHOUSE_URL:-http://localhost:8123/}}"
CH_HEADERS=()
[[ -n "${CLICKHOUSE_USER:-}" ]] && CH_HEADERS+=(-H "X-ClickHouse-User: $CLICKHOUSE_USER")
[[ -n "${CLICKHOUSE_PASSWORD:-}" ]] && CH_HEADERS+=(-H "X-ClickHouse-Key: $CLICKHOUSE_PASSWORD")
# The database is not a knob: ladingschema renders it into the DDL.
DB=boxer

# The mounts, and the names their policy records carry (--name, which is what
# the lading book and tally show instead of the id).
MOUNT_TREE=0x3BFE363BCF148001; NAME_TREE=boxer-tree
MOUNT_DOC=0x3BFE363BCF148002;  NAME_DOC=boxer-doc
MOUNT_SRC=0x3BFE363BCF148003;  NAME_SRC=lading-src
MOUNT_CHK=0x3BFE363BCF148004;  NAME_CHK=boxer-checkout
MOUNT_SCR=0x3BFE363BCF148005;  NAME_SCR=demo-scratch

# demo-scratch is walked under a lowered threshold so a 3 MiB fixture is over it
# without needing a 5 MiB one, and a raised threshold would hide the ref case.
SCRATCH_INLINE_MAX=$((2 * 1024 * 1024))

SCRATCH_DIR="$OUT/scratch/$NAME_SCR"
STAGE_DIR="$OUT/stage/$NAME_TREE"
SQL_FILE="$OUT/lading-demo.sql"

# Stages that only run when named. purge is destructive, so it is never part of
# "every stage".
OPT_IN_ONLY=(purge)

# DESC_ONLY makes a stage set its description and return without doing
# anything, which is how the listing and the per-stage banner read it.
DESC_ONLY=0
desc=""

log() { printf '%s\n' "$*" >&2; }
die() { log "lading-demo: $*"; exit 1; }

# ch runs one statement against the endpoint and writes the response to stdout.
# --fail-with-body turns a ClickHouse error into a non-zero exit *and* keeps the
# server's message, which is the only useful half of a 500.
ch() {
	curl -sS --fail-with-body --max-time "${LADINGDEMO_CH_TIMEOUT:-120}" \
		"${CH_HEADERS[@]}" "$CH_URL" --data-binary "$1"
}

# physcol asks the server for one physical column name rather than typing a
# leeway-mangled one into this script (how-to §8). The two it is used for are
# backbone plain columns — the mount id and the snapshot instant — and they are
# the only physical columns this script names; everything else about these
# tables is read through the macros.
physcol() {
	ch "SELECT name FROM system.columns
	    WHERE database = '$DB' AND table = '$1' AND name LIKE '$2'
	    LIMIT 1"
}

# field reads one key=value word out of the summary line `fs snapshot` prints.
field() {
	printf '%s' "$1" | tr ' ' '\n' | sed -n "s/^$2=//p" | tail -1
}

# rel is a path as this script prints it: relative to the repository root, and
# "." for the root itself. Nothing here prints an absolute path.
rel() {
	local p=${1#"$root"/}
	[[ "$p" == "$1" ]] && p=.
	printf '%s' "$p"
}

# fill is n copies of a character, which is how the section rules below are
# drawn at one width rather than by counting dashes by hand.
fill() {
	local n=$1 pad
	((n < 0)) && n=0
	pad=$(printf '%*s' "$n" '')
	printf '%s' "${pad// /$2}"
}
rule() { log "── $1 $(fill $((70 - 4 - ${#1})) '─')"; }
banner() { log "═══ $1 $(fill $((70 - 5 - ${#1})) '═')"; }

# human is a byte count as a person reads it. The exact numbers stay on the
# per-snapshot lines the command prints.
human() {
	awk -v b="${1:-0}" 'BEGIN {
		split("B KiB MiB GiB TiB", u, " ")
		i = 1
		while (b >= 1024 && i < 5) { b /= 1024; i++ }
		fmt = (i == 1) ? "%d %s" : "%.1f %s"
		printf fmt, b, u[i]
	}'
}

# fit keeps a field inside its column by dropping the HEAD of a path: what
# identifies one is its end. The marker is ASCII, so the padding still counts
# what the terminal shows.
fit() {
	local s=$1 w=$2
	if ((${#s} > w)); then
		printf '..%s' "${s: -$((w - 2))}"
	else
		printf '%s' "$s"
	fi
}

# SUMMARY collects one record per snapshot taken, for the report stage.
SUMMARY=()
SCRATCH_SNAP1=""
SCRATCH_SNAP2=""

# snap takes one snapshot and records what it turned out to be. Extra arguments
# go to the command, which is where --meta-only and --inline-max travel.
snap() {
	local mount=$1 name=$2 src=$3; shift 3
	log "  $mount  $name  <- $(rel "$src")${*:+  $*}"
	if ((DRY)); then
		return 0
	fi
	local out rc
	out=$("$BIN" --logFormat=console --logLevel=warn fs snapshot \
		--mount "$mount" --name "$name" --ttl-days "$TTL" --profile "$PROFILE" \
		"$@" "$src" 2>&1)
	rc=$?
	printf '%s\n' "$out" | sed 's/^/    /' >&2
	((rc == 0)) || die "snapshot failed for $name — see above"
	SUMMARY+=("$name|$mount|$(rel "$src")|$(field "$out" entries)|$(field "$out" bytes)|$(field "$out" referenced)|$(field "$out" errors)|$(field "$out" snapshot)")
	return 0
}

# =============================================================================
# Stages
# =============================================================================
# One function per stage, named stage_<NN>_<slug>; the slug is what a selection
# pattern matches. Each sets desc first and returns when only that was asked
# for. Adding a stage is adding a function; nothing else needs touching.

stage_10_tables() {
	desc="report whether the store's tables are there, and how big they are"
	if ((DESC_ONLY)); then return 0; fi
	log "the store at $CH_URL, before this run:"
	local shape
	shape=$(ch "SELECT name, engine, total_rows AS part_rows,
	                   formatReadableSize(total_bytes) AS size
	            FROM system.tables
	            WHERE database = '$DB' AND name LIKE 'fs%'
	            ORDER BY name
	            FORMAT PrettyCompactMonoBlock") || die "cannot read $CH_URL: $shape"
	if [[ -z "$shape" ]]; then
		log "  no $DB.fs* tables yet — the first snapshot below creates them."
		log "  Every snapshot run calls lading.Provision first (CREATE … IF NOT"
		log "  EXISTS, the materialised tree columns, the path constraint, the"
		log "  directory skip index, the snapshot-index view) and then"
		log "  lading.Verify, which is not optional: IF NOT EXISTS succeeds"
		log "  against an older shape and the store decodes positionally."
	else
		printf '%s\n' "$shape" | sed 's/^/  /' >&2
		log "  present, so provisioning below is a no-op. A table keeps the"
		log "  profile it was CREATEd under — changing it is a migration, not a"
		log "  re-run (how-to §1)."
	fi
	return 0
}

stage_20_tree() {
	desc="mount $NAME_TREE: the repository's tracked files, content stored"
	if ((DESC_ONLY)); then return 0; fi
	# A local walk has no filters — filters are a property of an rclone source
	# (how-to §7) — so the scope is expressed by staging the file list git
	# already keeps: tracked files only. No .git, no tmp/, and none of
	# rust/target, which on its own is larger than everything else here
	# together. Content is read from the working tree, so uncommitted edits are
	# what lands; the staging tree is thrown away once the walk has it.
	log "staging the tracked tree into $(rel "$STAGE_DIR")"
	if ((DRY)); then
		snap "$MOUNT_TREE" "$NAME_TREE" "$STAGE_DIR"
		return 0
	fi
	git -C "$root" rev-parse --is-inside-work-tree >/dev/null 2>&1 ||
		die "not a git work tree at $(rel "$root"), and the tracked file list is how this stage scopes the walk"
	rm -rf "$STAGE_DIR" || die "cannot clear $(rel "$STAGE_DIR")"
	mkdir -p "$STAGE_DIR" || die "cannot create $(rel "$STAGE_DIR")"
	# A staged deletion leaves a tracked path with no file, which tar would
	# refuse; the filter drops those rather than failing the stage.
	git -C "$root" ls-files -z |
		while IFS= read -r -d '' f; do
			if [[ -f "$root/$f" || -L "$root/$f" ]]; then
				printf '%s\0' "$f"
			fi
		done |
		tar -C "$root" --null -T - -cf - |
		tar -xf - -C "$STAGE_DIR"
	local st=("${PIPESTATUS[@]}")
	[[ "${st[0]}" == 0 && "${st[2]}" == 0 && "${st[3]}" == 0 ]] ||
		die "staging the tracked tree failed (git=${st[0]} tar-c=${st[2]} tar-x=${st[3]})"
	snap "$MOUNT_TREE" "$NAME_TREE" "$STAGE_DIR"
	if ((KEEP_STAGE)); then
		log "  keeping $(rel "$STAGE_DIR") (LADINGDEMO_KEEP_STAGE=1)"
	else
		rm -rf "$STAGE_DIR"
	fi
	return 0
}

stage_30_doc() {
	desc="mount $NAME_DOC: doc/ with content — the tree tally-scene.sh drives"
	if ((DESC_ONLY)); then return 0; fi
	snap "$MOUNT_DOC" "$NAME_DOC" "$root/doc"
	return 0
}

stage_40_src() {
	desc="mount $NAME_SRC: public/fs/lading — the store's own source, as a second mount"
	if ((DESC_ONLY)); then return 0; fi
	snap "$MOUNT_SRC" "$NAME_SRC" "$root/public/fs/lading"
	return 0
}

stage_50_checkout() {
	desc="mount $NAME_CHK: the whole checkout, --meta-only (no bytes stored)"
	if ((DESC_ONLY)); then return 0; fi
	# Deliberately the tree as it is on disk rather than the tracked subset:
	# .git, tmp/ and every build artefact included. Stat-only, so the walk reads
	# no content and only the row count grows.
	snap "$MOUNT_CHK" "$NAME_CHK" "$root" --meta-only
	return 0
}

stage_60_scratch() {
	desc="mount $NAME_SCR: a generated tree, twice — ref, long lines, symlink, err, and a diff"
	if ((DESC_ONLY)); then return 0; fi
	log "building the scratch tree at $(rel "$SCRATCH_DIR")"
	if ((DRY)); then
		snap "$MOUNT_SCR" "$NAME_SCR" "$SCRATCH_DIR" --inline-max "$SCRATCH_INLINE_MAX"
		log "  … then a mutation, then the same walk again"
		return 0
	fi
	scratch_build || die "cannot build the scratch tree"
	snap "$MOUNT_SCR" "$NAME_SCR" "$SCRATCH_DIR" --inline-max "$SCRATCH_INLINE_MAX"
	local errs
	IFS='|' read -r _ _ _ _ _ _ errs SCRATCH_SNAP1 <<<"${SUMMARY[-1]}"
	if [[ "$errs" == 0 ]]; then
		log "  errors=0: the unreadable fixture was readable after all — this"
		log "  runs as someone who can read a 0000 file (root, or a mount that"
		log "  ignores the mode), so the err column has nothing to show."
	else
		log "  errors=$errs — the unreadable file became a row carrying its"
		log "  failure in err, and the walk carried on. That is the design:"
		log "  a node that cannot be read is a row, not a failed snapshot."
	fi
	log "mutating the tree, so the second walk is a different snapshot"
	scratch_mutate || die "cannot mutate the scratch tree"
	snap "$MOUNT_SCR" "$NAME_SCR" "$SCRATCH_DIR" --inline-max "$SCRATCH_INLINE_MAX"
	SCRATCH_SNAP2=${SUMMARY[-1]##*|}
	log "  two snapshots of one mount: history for a path, and a real diff."
	log "  Nothing was updated — a snapshot is written once and there is no"
	log "  update path, so every modify question means take another one."
	return 0
}

# scratch_build lays out the fixtures, each one an entry the store records
# differently.
scratch_build() {
	rm -rf "$SCRATCH_DIR" || return 1
	mkdir -p "$SCRATCH_DIR/notes/deep" || return 1
	cat >"$SCRATCH_DIR/notes/a.md" <<'EOF'
# a.md
The second walk appends to this file, so the diff calls it modified and the
history chapter shows two versions of one path.
The word marmalade appears exactly once in this tree, which makes it a
harmless needle for the grep chapter.
EOF
	cat >"$SCRATCH_DIR/notes/b.md" <<'EOF'
# b.md
Unchanged between the two walks, so the diff calls it same and leaves it out.
EOF
	printf 'removed by the second walk\n' >"$SCRATCH_DIR/notes/deep/removed.md"
	# Text, and stored — but one line longer than a 1 MiB block, so the store
	# cannot promise that a block boundary lands after a newline: it records
	# text=false and cuts at fixed offsets instead (how-to §9).
	head -c $((1500 * 1024)) /dev/zero | tr '\0' 'x' >"$SCRATCH_DIR/long-line.txt"
	printf '\n' >>"$SCRATCH_DIR/long-line.txt"
	# Over the threshold this mount is walked under, so the entry records ref:
	# size, mtime and BLAKE3, no bytes. Reading it back is ErrReferenced unless
	# the caller supplies a SourceFetcherI (how-to §4).
	head -c $((3 * 1024 * 1024)) /dev/urandom >"$SCRATCH_DIR/over-threshold.bin"
	ln -s notes/a.md "$SCRATCH_DIR/link-to-a.md" || return 1
	# 0000 excludes the owner too, so this is unreadable unprivileged; as root
	# it is simply readable and the fixture is inert.
	printf 'unreadable on purpose\n' >"$SCRATCH_DIR/unreadable.txt"
	chmod 000 "$SCRATCH_DIR/unreadable.txt" || return 1
	return 0
}

# scratch_mutate makes the second walk differ from the first in all three ways
# the diff distinguishes.
scratch_mutate() {
	printf '\nAppended by the second walk.\n' >>"$SCRATCH_DIR/notes/a.md" || return 1
	printf 'added by the second walk\n' >"$SCRATCH_DIR/notes/added.md" || return 1
	rm -f "$SCRATCH_DIR/notes/deep/removed.md" || return 1
	return 0
}

stage_70_report() {
	desc="what is in the store now, a file of queries for it, and where to run them"
	if ((DESC_ONLY)); then return 0; fi
	if ((DRY)); then
		log "would report the store's shape and write $(rel "$SQL_FILE")"
		return 0
	fi
	local shape
	shape=$(ch "SELECT name, engine, total_rows AS part_rows,
	                   formatReadableSize(total_bytes) AS size
	            FROM system.tables
	            WHERE database = '$DB' AND name LIKE 'fs%'
	            ORDER BY name
	            FORMAT PrettyCompactMonoBlock") || die "cannot read $CH_URL: $shape"
	log ""
	rule "the store"
	printf '%s\n' "$shape" | sed 's/^/  /' >&2
	log "  fsmeta is one row per node per snapshot, fsdata one row per stored"
	log "  block, fssnap one row per COMPLETE snapshot — filled by fssnap_mv"
	log "  from root rows, which is why a half-finished walk is in no index."
	log "  part_rows and size are disk: rows a DELETE has masked and rows TTL"
	log "  has not dropped yet are both still counted there."

	# The mount, by the backbone column that carries it — the same column the
	# how-to's §8 purge names.
	local idcol ledger
	idcol=$(physcol fsmeta 'id:id:%') ||
		die "cannot resolve the mount column: $idcol"
	if [[ -n "$idcol" ]]; then
		ledger=$(ch "SELECT lower(hex(m)) AS mount,
		                    sum(meta) AS meta_rows,
		                    sum(data) AS data_rows,
		                    sum(snaps) AS snapshots
		             FROM (
		                       SELECT \"$idcol\" AS m, 1 AS meta, 0 AS data, 0 AS snaps FROM $DB.fsmeta
		             UNION ALL SELECT \"$idcol\", 0, 1, 0 FROM $DB.fsdata
		             UNION ALL SELECT \"$idcol\", 0, 0, 1 FROM $DB.fssnap
		             )
		             GROUP BY m ORDER BY m
		             FORMAT PrettyCompactMonoBlock") || die "ledger: $ledger"
		log ""
		log "  rows per mount, over every snapshot the tables still hold. A"
		log "  plain count(), so a masked row is already gone from it — an"
		log "  expired row TTL has not dropped is not, and hiding those is the"
		log "  expiresAt cutoff the macros add and this query does not:"
		printf '%s\n' "$ledger" | sed 's/^/  /' >&2
	fi

	if ((${#SUMMARY[@]} > 0)); then
		log ""
		rule "what this run walked"
		printf '  %-14s %-36s %7s %9s %4s %3s
' \
			mount source entries size refs err >&2
		local rec name mount src entries bytes refs errs
		for rec in "${SUMMARY[@]}"; do
			IFS='|' read -r name mount src entries bytes refs errs _ <<<"$rec"
			printf '  %-14s %-36s %7s %9s %4s %3s
' \
				"$name" "$(fit "$src" 36)" "$entries" "$(human "$bytes")" \
				"$refs" "$errs" >&2
		done
		log "  The ids are in the query file's header. size is what the entries"
		log "  DESCRIBE rather than what was stored: a ref entry and a"
		log "  --meta-only mount both count their sizes here."
	fi

	write_sql_file
	local mountdir=${MOUNT_TREE#0x}
	mountdir=${mountdir,,}
	log ""
	rule "queries"
	log "  $(rel "$SQL_FILE") — the lading book's chapters and the how-to's"
	log "  examples, with this run's mounts and snapshot instants filled in."
	log ""
	log "  A plain SQL client cannot run them: fs(), fsdata() and fssnap() are"
	log "  macros the QUERYING PROCESS expands (how-to §5), and an expansion is"
	log "  an authorisation decision, so a host that states no mount visibility"
	log "  gets no expansion rather than a default one. Three hosts that do:"
	log ""
	log "    bash rust/imzero2/hmi.sh --launch tally"
	log "        the lading browser (ADR-0200): mounts, snapshots, preview,"
	log "        info, history, diff, find, du, problems."
	log "    bash rust/imzero2/hmi.sh --launch play"
	log "        the SQL editor — paste from the file above."
	log "    bash rust/imzero2/hmi.sh --launch \"subject_alias = 'lad-browse'\""
	log "        one chapter of the lading book as its own applet: lad-browse,"
	log "        lad-tree, lad-grep, lad-find, lad-du, lad-diff, lad-history,"
	log "        lad-problems, lad-audit, lad-ledger."
	log ""
	log "  Headless, no display, and it asserts as well as captures:"
	log "    scripts/dev/tally-scene.sh"
	log "        drives tally over $NAME_DOC and $NAME_SRC — the two mounts its"
	log "        defaults name — and writes seven PNGs."
	log ""
	rule "as a file system"
	log "  The SFTP head speaks on a pipe, so there is no socket, no port and no"
	log "  credential: possession of the pipe is the authorisation."
	log ""
	log "    rclone mount --read-only \\"
	log "      ':sftp,ssh=\"boxer fs sftp-stdio --mount $MOUNT_TREE\",shell_type=unix:/$mountdir/latest' \\"
	log "      /mnt/x"
	log ""
	log "  Snapshot directories are 20060102T150405.000000000Z, so they sort"
	log "  chronologically and cd is time travel; latest is the only mutable"
	log "  name in the tree. rclone is not a prerequisite of this script, so"
	log "  that command is printed rather than run."
	log ""
	rule "afterwards"
	log "  Nothing needs cleaning up: every snapshot above expires $TTL day(s)"
	log "  after the end of the day it was taken on, and the tables drop whole"
	log "  parts. To take them out now: scripts/dev/lading-demo.sh purge"
	log "  Running this script again adds a snapshot per mount, which is what"
	log "  gives the repository trees a history and a diff of their own."
	return 0
}

# write_sql_file emits the queries with this run's ids and instants in them. The
# chapter SQL is lifted from apps/sqlapplet/booklading/, which the integration
# lane executes — better provenance than a fresh invention.
write_sql_file() {
	mkdir -p "$(dirname "$SQL_FILE")" || die "cannot create $(dirname "$SQL_FILE")"
	local diff_s1="'latest'" diff_s2="'latest'" diff_note=""
	if [[ -z "$SCRATCH_SNAP1" || -z "$SCRATCH_SNAP2" ]]; then
		# This run took no scratch snapshot — the report stage on its own — so
		# the two newest complete ones come from the index. Both arguments
		# 'latest' would compare a snapshot with itself and find nothing.
		local idc tsc pair
		idc=$(physcol fssnap 'id:id:%')
		tsc=$(physcol fssnap 'ts:ts:%')
		if [[ -n "$idc" && -n "$tsc" ]]; then
			pair=$(ch "SELECT toString(\"$tsc\") FROM $DB.fssnap
			           WHERE \"$idc\" = $MOUNT_SCR
			           ORDER BY \"$tsc\" DESC LIMIT 2")
			SCRATCH_SNAP2=$(printf '%s' "$pair" | sed -n 1p)
			SCRATCH_SNAP1=$(printf '%s' "$pair" | sed -n 2p)
		fi
	fi
	if [[ -n "$SCRATCH_SNAP1" && -n "$SCRATCH_SNAP2" ]]; then
		# A string is read as a datetime literal; a bare number would be read
		# as SECONDS whatever its scale says, so nanoseconds handed over
		# unquoted match nothing (how-to §5).
		diff_s1="'$SCRATCH_SNAP1'"
		diff_s2="'$SCRATCH_SNAP2'"
	else
		diff_note="-- That mount has fewer than two complete snapshots here, so both
-- arguments below are 'latest' and the query compares a snapshot with itself.
-- Run scripts/dev/lading-demo.sh scratch, or paste two instants from the
-- ledger above.
"
	fi
	cat >"$SQL_FILE" <<EOF
-- Queries over the lading store seeded by scripts/dev/lading-demo.sh.
--
-- fs(), fsdata() and fssnap() are macros, expanded by the process that issues
-- the statement (doc/howto/lading-snapshot-store.md §5). Paste these into
-- play's editor, or open the matching chapter of the lading book; a plain SQL
-- client answers "unknown table function fs".
--
-- The second argument of fs()/fsdata() selects the snapshot: omitted is the
-- newest complete one, '*' is all of them, a string is a datetime literal.
--
-- One block at a time: each query carries the SET prelude its slots need.
--
-- The mounts this file names:
--   $MOUNT_TREE  $NAME_TREE
--   $MOUNT_DOC  $NAME_DOC
--   $MOUNT_SRC  $NAME_SRC
--   $MOUNT_CHK  $NAME_CHK   (--meta-only: content is 'none' everywhere)
--   $MOUNT_SCR  $NAME_SCR     (two snapshots)

-- ── the ledger: every complete snapshot, newest first (lad-ledger) ──────────
SET param_m = '*';
SELECT mount, snap, snap_entries, snap_bytes, ttl_class, text_rule, inline_max,
       expires_at
FROM fssnap({m:String})
ORDER BY snap DESC;

-- ── browse one directory (lad-browse) ──────────────────────────────────────
SET param_m = '$MOUNT_TREE';
SET param_dir = 'public/fs/lading';
SELECT name, is_dir, node_kind, size, mtime, ext, content, text, link_target,
       lower(hex(content_hash)) AS hash, path
FROM fs({m:String})
WHERE dir = {dir:String}
ORDER BY is_dir DESC, name;

-- ── the biggest files, and which of them were only referenced ──────────────
SELECT path, size, content
FROM fs($MOUNT_TREE)
WHERE NOT is_dir
ORDER BY size DESC
LIMIT 20;

-- ── grep with real line numbers (lad-grep) ─────────────────────────────────
-- Only files the store marked text: their block boundaries fall immediately
-- after a newline and each block carries its first line's number, so
-- line0 + i - 1 is the line an editor would show. Files without the guarantee
-- are left out rather than searched inexactly.
SET param_m = '$MOUNT_DOC';
SET param_needle = 'ttl_only_drop_parts';
SELECT path, line0 + i - 1 AS lineno, line
FROM fsdata({m:String})
ARRAY JOIN splitByChar('\n', data) AS line,
           arrayEnumerate(splitByChar('\n', data)) AS i
WHERE {needle:String} != ''
  AND (mount, path) IN (SELECT mount, path FROM fs({m:String}) WHERE text)
  AND match(data, {needle:String})
  AND match(line, {needle:String})
ORDER BY path, lineno
LIMIT 100;

-- ── du: directory totals in one pass (lad-du) ──────────────────────────────
SET param_m = '$MOUNT_TREE';
SELECT anc AS directory, sum(size) AS bytes, count() AS files
FROM fs({m:String})
ARRAY JOIN arrayMap(k -> arrayStringConcat(arraySlice(splitByChar('/', path), 1, k), '/'),
                    range(1, depth)) AS anc
WHERE NOT is_dir
GROUP BY anc
ORDER BY bytes DESC
LIMIT 30;

-- ── one path across every snapshot of a mount (lad-history) ────────────────
SET param_m = '$MOUNT_SCR';
SET param_path = 'notes/a.md';
SELECT snap, node_kind, size, mtime, lower(hex(content_hash)) AS hash, content,
       expires_at
FROM fs({m:String}, '*')
WHERE path = {path:String}
ORDER BY snap;

-- ── two snapshots of one mount, diffed (lad-diff) ──────────────────────────
${diff_note}SET param_m = '$MOUNT_SCR';
SET param_s1 = $diff_s1;
SET param_s2 = $diff_s2;
SELECT if(n.path != '', n.path, o.path) AS path,
       multiIf(o.path = '', 'added',
               n.path = '', 'removed',
               n.content_hash != o.content_hash OR n.mtime != o.mtime, 'modified',
               'same') AS change,
       o.size AS size_before, n.size AS size_after,
       o.mtime AS mtime_before, n.mtime AS mtime_after
FROM fs({m:String}, {s2:String}) AS n
FULL OUTER JOIN fs({m:String}, {s1:String}) AS o
  ON n.mount = o.mount AND n.path = o.path
WHERE change != 'same'
ORDER BY path;

-- ── what the walk could not read (lad-problems) ────────────────────────────
SELECT mount, path, node_kind, content, err
FROM fs('*')
WHERE err != ''
ORDER BY mount, path;

-- ── the three content modes, per mount ─────────────────────────────────────
-- blocks = content stored; ref = over the inline threshold, so size, mtime and
-- hash only; none = a directory, a symlink, or a metadata-only mount.
SELECT mount, content, count() AS entries, sum(size) AS bytes
FROM fs('*')
GROUP BY mount, content
ORDER BY mount, content;

-- ── identical content: a question, not a storage strategy ──────────────────
-- Nothing is deduplicated, so two identical files are two copies — and which
-- ones they are is one GROUP BY (how-to §9).
SELECT lower(hex(content_hash)) AS hash, count() AS copies, any(size) AS bytes,
       groupArray(10)(path) AS paths
FROM fs($MOUNT_TREE)
WHERE NOT is_dir AND size > 0
GROUP BY content_hash
HAVING copies > 1
ORDER BY bytes * copies DESC
LIMIT 20;

-- ── the block audit: recompute every digest (lad-audit) ────────────────────
-- Empty is the good answer. Under the corpus profile every block carries its
-- own BLAKE3, so this re-reads the mount's stored bytes.
SELECT path, seq, lower(hex(hash)) AS recorded,
       lower(hex(BLAKE3(data))) AS recomputed
FROM fsdata($MOUNT_SCR)
WHERE BLAKE3(data) != hash
ORDER BY path, seq
LIMIT 100;

-- ── a metadata-only mount lists and stats, and has no bytes ────────────────
SELECT name, node_kind, size, mtime, content
FROM fs($MOUNT_CHK)
WHERE dir = '.'
ORDER BY is_dir DESC, name;
EOF
	return 0
}

stage_99_purge() {
	desc="delete this demo's mounts from the store (opt-in: name it to run it)"
	if ((DESC_ONLY)); then return 0; fi
	if ((DRY)); then
		log "would delete mounts $MOUNT_TREE $MOUNT_DOC $MOUNT_SRC $MOUNT_CHK $MOUNT_SCR"
		return 0
	fi
	# One lightweight DELETE per (table, mount): nothing in the store is shared
	# and nothing is reference-counted, so a per-mount purge needs no traversal
	# (how-to §8). The mount column is resolved, not typed.
	local idcol
	idcol=$(physcol fsmeta 'id:id:%') ||
		die "cannot resolve the mount column: $idcol"
	[[ -n "$idcol" ]] || die "no $DB.fsmeta — nothing to purge"
	local t m out
	for t in fsmeta fsdata fssnap; do
		for m in "$MOUNT_TREE" "$MOUNT_DOC" "$MOUNT_SRC" "$MOUNT_CHK" "$MOUNT_SCR"; do
			out=$(ch "DELETE FROM $DB.$t WHERE \"$idcol\" = $m") ||
				die "delete from $t for $m: $out"
		done
		log "  purged $DB.$t"
	done
	rm -rf "$SCRATCH_DIR" "$STAGE_DIR"
	log "  the mounts' policy records in $DB.facts are left alone: that table is"
	log "  a registry keyed by mount, it carries no snapshot rows, and writing"
	log "  the mount again is what replaces an entry."
	return 0
}

# =============================================================================
# Selection and run
# =============================================================================

mapfile -t all_stages < <(compgen -A function | grep '^stage_[0-9]*_' | sort)
((${#all_stages[@]} > 0)) || die "no stages found"

slug_of() { printf '%s' "${1#stage_*_}"; }

opt_in_only() {
	local s
	for s in "${OPT_IN_ONLY[@]}"; do
		if [[ "$1" == "$s" ]]; then
			return 0
		fi
	done
	return 1
}

describe() {
	desc=""
	DESC_ONLY=1
	"$1"
	DESC_ONLY=0
}

selected=()
if (($# > 0)); then
	for fn in "${all_stages[@]}"; do
		slug=$(slug_of "$fn")
		for pat in "$@"; do
			if [[ "$slug" == *"$pat"* ]]; then
				selected+=("$fn")
				break
			fi
		done
	done
	((${#selected[@]} > 0)) || die "no stage matches: $*  (LADINGDEMO_LIST=1 lists them)"
else
	for fn in "${all_stages[@]}"; do
		slug=$(slug_of "$fn")
		if ! opt_in_only "$slug"; then
			selected+=("$fn")
		fi
	done
fi

if ((LIST)); then
	log "stages, in order — a pattern selects by substring:"
	for fn in "${all_stages[@]}"; do
		describe "$fn"
		slug=$(slug_of "$fn")
		mark=" "
		if opt_in_only "$slug"; then mark="*"; fi
		printf '  %s %-9s %s\n' "$mark" "$slug" "$desc" >&2
	done
	log "  (* opt-in: runs only when named)"
	exit 0
fi

# ------------------------------------------------------------------- preflight

mkdir -p "$OUT" || die "cannot create $OUT"

if ((!DRY)); then
	version=$(ch "SELECT version()") ||
		die "ClickHouse not reachable at $CH_URL: $version"
	log "ClickHouse $version at $CH_URL"
fi

needs_binary=0
for fn in "${selected[@]}"; do
	case "$(slug_of "$fn")" in
	tables | report | purge) ;;
	*) needs_binary=1 ;;
	esac
done

if ((needs_binary && !DRY)); then
	if ((BUILD)); then
		mkdir -p "$(dirname "$BIN")" || die "cannot create $(dirname "$BIN")"
		log "building the boxer CLI into $(rel "$BIN") (LADINGDEMO_BUILD=0 to reuse)"
		# The repo's build tags are not optional: without them packages fail to
		# compile with misleading "undefined" errors (AGENTS.md).
		# shellcheck source=/dev/null
		source "$root/scripts/dev/go-build-env.sh"
		# shellcheck disable=SC2086 # deliberate word splitting of the flag list
		(cd "$root" && go build $BOXER_GO_FLAGS -tags "$BOXER_GO_TAGS" \
			-o "$BIN" ./public/app) || die "go build failed"
	fi
	[[ -x "$BIN" ]] || die "no boxer binary at $(rel "$BIN") — unset LADINGDEMO_BUILD to build one"
fi

# ------------------------------------------------------------------------- run

for fn in "${selected[@]}"; do
	slug=$(slug_of "$fn")
	describe "$fn"
	log ""
	banner "$slug"
	if [[ -n "$desc" ]]; then log "$desc"; fi
	"$fn" || die "stage $slug failed"
done

log ""
if ((DRY)); then
	log "dry run — nothing was walked or written."
else
	log "done."
fi
