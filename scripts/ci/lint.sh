#!/bin/bash
set -e
set -o pipefail
here=$(dirname "$(readlink -f "$BASH_SOURCE")")
cd "$here/../.."
tags="$(cat "$here/../../tags" | tr -d "\n")"

rc=0

# Per-step bookkeeping for the summary trailer. Parallel arrays indexed by
# step. status is one of: pass | fail | warn. fail means the step set rc=1
# (drove the script's non-zero exit); warn means the step produced findings
# that are non-blocking (staticcheck/errcheck/nilaway, doclint warn-only).
declare -a step_names step_durs step_statuses
overall_t0=$EPOCHREALTIME

step_begin() {
    _step_name="$1"
    _step_t0=$EPOCHREALTIME
    echo ""
    echo "=== $_step_name ==="
}

step_end() {
    local status="$1"
    local dur
    dur=$(awk -v s="$_step_t0" -v e="$EPOCHREALTIME" 'BEGIN{printf "%.3f", e-s}')
    step_names+=("$_step_name")
    step_durs+=("$dur")
    step_statuses+=("$status")
}

step_begin "go vet"
# go vet has no built-in exclude for generated files, so filter output.
if go vet -tags "$tags" ./public/... 2>&1 | grep -v '\.out\.go:' | grep -v '\.gen\.go:' | grep -q .; then
    go vet -tags "$tags" ./public/... 2>&1 | grep -v '\.out\.go:' | grep -v '\.gen\.go:'
    rc=1
    step_end fail
else
    echo "passed"
    step_end pass
fi

step_begin "staticcheck"
# Exclude generated ANTLR parser files. Capture so we can mark warn vs pass;
# this trades streaming for status visibility (staticcheck batches anyway).
#
# SKIP LIST — packages withheld from the run because staticcheck aborts the
# whole PROCESS on them, taking every other package's analysis down with it.
# This is not a findings filter: nothing in a listed package gets checked, and
# the step reports warn rather than pass while the list is non-empty.
#
# The cause is upstream. staticcheck carries analysis facts between packages
# keyed by golang.org/x/tools/go/types/objectpath, whose method paths are
# ordinals into the receiver's method list ("SectionReaders.M4"). Under go1.27
# generic methods (ADR-0199) the two sides of the export-data boundary number
# those methods differently: analysing marshallreflect FROM SOURCE encodes
# (*SectionReaders).PlainColumn as M4 and .Section as M5, while a consumer
# reading the same type FROM EXPORT DATA resolves M4 to .DetectAll and M5 to
# .ReadComponent — export data groups the non-generic methods ahead of the
# generic ones, source order interleaves them. Every fact on such a type
# therefore lands on the wrong method. SA4023 reads the 1-result builder
# method's nilness fact as if it belonged to 3-result ReadComponent and indexes
# past the end:
#
#   panic: runtime error: index out of range [2] with length 1
#     nilness.(*Result).Nilness -> sa4023.run
#
# -checks cannot dodge it: staticcheck runs every analyzer and filters
# diagnostics afterwards, so "-SA4023" still panics. No published version
# helps either — v0.7.0, the pin this repo carried until the bump alongside
# this note, cannot read go1.27 export data at all ("export data version 4 is
# greater than maximum supported version 2"), and master as of 7dc2d7d2 has the
# same unguarded index as v0.8.0. Re-check on the next release; when the list
# can be emptied, delete it and the panic branch with it.
#
# Note what the skip does NOT fix: the mis-attribution is silent everywhere
# else. Any fact-carrying check (U1000 among them) reading a type that mixes
# generic and non-generic methods may be reading a sibling method's fact. Two
# types are exposed today — marshallreflect.SectionReaders and
# ecsdemo/stage2.FatRow — so treat findings that name their methods with
# suspicion until upstream numbers them consistently.
sc_skip='github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/componentview'
mapfile -t sc_pkgs < <(go list -tags "$tags" ./public/... | grep -vxF "$sc_skip")
if [ -n "$sc_skip" ]; then
    echo "skipped, NOT analysed (see the note above this step in $0):"
    printf '%s\n' "$sc_skip" | sed 's/^/  /'
fi
sc_out=$(go tool honnef.co/go/tools/cmd/staticcheck -tags "$tags" \
    -checks "all,-ST1000,-ST1003,-ST1005,-ST1016,-ST1020,-ST1021,-ST1022,-S1023,-SA4011,-SA1019" \
    "${sc_pkgs[@]}" 2>&1 | grep -v '\.out\.go:' | grep -v '\.gen\.go:' || true)
if printf '%s' "$sc_out" | grep -q '^panic:'; then
    echo "DID NOT RUN — staticcheck aborted, no analysis was performed:"
    printf '%s' "$sc_out" | grep -m1 '^panic:' | sed 's/^/  /'
    echo "  (a package outside the skip list now trips it — see the note above this step in $0)"
    step_end warn
elif [ -n "$sc_out" ]; then
    printf '%s\n' "$sc_out"
    step_end warn
elif [ -n "$sc_skip" ]; then
    echo "no findings, but the skip list is non-empty"
    step_end warn
else
    echo "passed"
    step_end pass
fi

step_begin "errcheck"
ec_out=$(go tool github.com/kisielk/errcheck -tags "$tags" \
    -exclude <(printf '%s\n' \
        'fmt.Fprintf' 'fmt.Fprintln' 'fmt.Fprint' \
        '(*strings.Builder).WriteString' '(*strings.Builder).WriteByte' '(*strings.Builder).WriteRune' \
        '(*bytes.Buffer).WriteString' '(*bytes.Buffer).WriteByte' '(*bytes.Buffer).Write') \
    ./public/... 2>&1 | grep -v '\.out\.go:' | grep -v '\.gen\.go:' || true)
if [ -n "$ec_out" ]; then
    printf '%s\n' "$ec_out"
    step_end warn
else
    echo "passed"
    step_end pass
fi

# nilaway disabled — re-enable by uncommenting the block below.
# step_begin "nilaway"
# # nilaway's own -tags flag is deprecated/no-op; build tags must be passed via
# # GOFLAGS so the analysis driver picks them up. Without this, tag-gated
# # packages (e.g. gpu_intel, integration) are excluded and importers cascade into
# # hundreds of bogus "could not import / undefined" lines.
# # -include-pkgs restricts analysis to first-party code; stdlib and 3rd-party
# # returns are then assumed non-nil, which suppresses the bulk of noise from
# # os.Stdout/http.Response.Body/ANTLR-style false positives that we cannot
# # fix locally.
# na_out=$(GOFLAGS="-tags=$tags" go tool go.uber.org/nilaway/cmd/nilaway \
#     -include-pkgs=github.com/stergiotis/boxer \
#     ./public/... 2>&1 || true)
# if [ -n "$na_out" ]; then
#     printf '%s\n' "$na_out"
#     step_end warn
# else
#     echo "passed"
#     step_end pass
# fi

step_begin "gov gate"
# The composite gate boxer publishes to consuming repositories (ADR-0179):
# buildtags, doclint, entry-points, file-naming, codelint. This script no longer
# spells that list out — public/gov/gate.DefaultSteps() is the single definition,
# so a step added there reaches boxer and every consumer at once, and boxer
# breaks first when it changes.
#
# gofmt and go vet stay above, outside the gate, on purpose: they must still run
# on a tree too broken to build this binary.
#
# Five steps in one process rather than five separate boxer.sh invocations also
# stops the binary being rebuilt per step (~38s -> ~18s here).
#
# The gate prints its own per-step summary; this script folds the whole thing
# into one entry in its trailer. `if out=$(...)` is required under `set -e`,
# since the gate exits non-zero on any failing step.
gate_err=$(mktemp -t gate-err.XXXXXX)
if out=$("$here/../../boxer.sh" gov gate \
        --tags "$tags" \
        --entry-points-baseline scripts/ci/entry-points-baseline.txt \
        --naming-baseline scripts/ci/naming-baseline.txt \
        2>"$gate_err"); then
    rm -f "$gate_err"
    printf '%s\n' "$out"
    if printf '%s' "$out" | grep -q 'warnings:'; then
        step_end warn
    else
        step_end pass
    fi
else
    printf '%s\n' "$out"
    cat "$gate_err"
    rm -f "$gate_err"
    rc=1
    step_end fail
fi

step_begin "designlint"
# IDS Tier 1 mechanical rules (ADR-0029 §SD8), driven via `go vet -vettool=`
# over a tempfile-built multichecker binary — the tag-aware analyzer path
# (multichecker.Main's own -tags flag is a deprecated no-op). Hard gate since
# the M5 fleet backfill (2026-07-12) took every shipped rule to zero findings:
# ANY output — finding or compile error — fails the build. Intentional
# exceptions use the per-line `// designlint:ignore=<rule-id> (reason)`
# annotation (doc/design-system/policy/tier1-mechanical.md §Annotations).
# Scoped to the egui2 UI tree and keelson runtime, where IDS tokens apply;
# generated files are filtered out.
dl_bin=$(mktemp -t designlint.XXXXXX)
if go build -tags "$tags" -o "$dl_bin" ./public/keelson/designsystem/lint/cmd/designlint 2>/dev/null; then
    dl_out=$(go vet -vettool="$dl_bin" -tags "$tags" \
        ./public/thestack/imzero2/... ./public/keelson/runtime/... 2>&1 \
        | grep -v '\.out\.go:' | grep -v '\.gen\.go:' || true)
    rm -f "$dl_bin"
    if [ -n "$dl_out" ]; then
        printf '%s\n' "$dl_out"
        rc=1
        step_end fail
    else
        echo "passed"
        step_end pass
    fi
else
    rm -f "$dl_bin"
    echo "designlint vettool failed to build"
    rc=1
    step_end fail
fi

step_begin "packageprops"
# Two ADR-0080 gates, one step. Both are boxer-specific (they are about this
# repo's own generated table and declarations), so they stay out of the
# `gov gate` composite, which publishes to consuming repositories.
#
#   props drift  — the committed proptable.out.go agrees with the git-tracked
#                  package_props.go declarations. Needs no survey and no
#                  TinyGo: it parses declarations and compares. A missing row
#                  makes every query over keelson's go_package_props table
#                  return a short answer with no error and no null, which is
#                  the failure mode a reader cannot see.
#   props verify — declarations agree with the freshly computed verdict.
#                  Static mode only proves red, which is sound (ADR-0078), so
#                  this fails on regressions and stays quiet about what it
#                  cannot judge. It is ~10s.
#
# `--mode static` is load-bearing, not decoration: the empirical mode shells
# out to `tinygo build` once per candidate package (minutes), and its verdict
# depends on whether this runner carries a TinyGo whose Go ceiling clears the
# repo's toolchain. The flag is not defended by this comment — `props verify`
# has no mode default and refuses to run without one, so dropping it here fails
# in a second rather than turning the lint step into a TinyGo sweep.
if out=$("$here/../../boxer.sh" code analysis golang wasmsurvey props drift 2>&1) &&
   out2=$("$here/../../boxer.sh" code analysis golang wasmsurvey props verify --mode static 2>&1); then
    printf '%s\n%s\n' "$out" "$out2"
    step_end pass
else
    printf '%s\n%s\n' "$out" "${out2:-}"
    rc=1
    step_end fail
fi

step_begin "h3_wasm_parity"
# Rebuilds rust/h3bridge to wasm and byte-compares against the committed
# public/science/geo/h3/internal/h3o_wasm/h3.wasm. Gracefully skipped when
# cargo or the wasm32-unknown-unknown target is absent so local lint stays
# green for contributors not touching the bridge; CI is the enforcer.
if out=$("$here/h3_wasm_parity.sh" 2>&1); then
    if [ -n "$out" ]; then
        echo "$out"
    else
        echo "passed"
    fi
    step_end pass
else
    echo "$out"
    rc=1
    step_end fail
fi

step_begin "rust_imzero2_check"
# Feature-matrix `cargo check` plus the crate's tests for rust/imzero2, and the
# ADR-0128 SD6 / ADR-0205 guarantee that a GPU-less feature set pulls no wgpu.
# The crate had no automated gate at all until ADR-0205 M4, which is how three
# Dependabot bumps merged green and broken on 2026-08-10, and how an eframe PR
# floated egui in a lock-only commit on 2026-08-19. Deliberately excludes
# clippy: rust/imzero2/check.sh runs it with -D warnings and it is red at HEAD,
# mostly in generated code, so gating on it would have kept the crate ungated.
# ~50s cold, ~3s warm. Skips gracefully without cargo, like h3_wasm_parity.
if out=$("$here/rust_imzero2_check.sh" 2>&1); then
    echo "passed"
    step_end pass
else
    echo "$out"
    rc=1
    step_end fail
fi

step_begin "gofmt"
# Go formatting enforcement — plain `gofmt`, the baseline §9 defers to. It is
# the Go counterpart of the rustfmt step below, and closes the gap
# ENGINEERING_PRACTICES §10 recorded (gofumpt / gci stay unadopted). Drift had
# reached 175 files before the tree was cleared on 2026-08-06.
#
# Generated files are skipped by their `Code generated ... DO NOT EDIT.` header
# rather than by path: relying on the `.out.go` / `.gen.go` patterns the vet and
# staticcheck steps filter on would have missed every generated file that isn't
# suffixed that way — which, until the `file-naming` gate started enforcing it
# (ADR-0048 N2/N3; `gov/filenaming`'s "generated-suffix" rule), was a real,
# silent gap: `palette_generated.go` and `chaliases_gen.go` sat undetected for
# a while before being renamed to close it. That gate now catches a fresh case
# of the same shape before it can linger the same way, but this step still
# reads the header rather than the path: a generator can still choose to emit
# somewhere the pattern doesn't reach, and this step's job is to not choke on
# it regardless. The header is read from the first lines only, which is where
# Go's own convention puts it — a file merely quoting the phrase is still checked.
#
# FIXING A FAILURE — read the diff before running `gofmt -w`. gofmt reformats
# doc comments, and its parser takes two kinds of ordinary prose punctuation for
# markup: `` and '' become Unicode quotes, and a line-leading + becomes a -
# bullet. Both have already falsified comments in this repo — one documenting
# SQL's doubled-quote escape, one where the + continued a sum and became a
# minus. When `gofmt -d` changes what a comment SAYS rather than how it is
# spaced, fix the prose first (name the thing instead of spelling it; rewrap so
# a + ends a line rather than starting one), then format. See b8c1f701.
gofmt_dirty=$(gofmt -l . 2>/dev/null || true)
gofmt_out=""
for f in $gofmt_dirty; do
    head -n 5 "$f" | grep -qE '^// Code generated .* DO NOT EDIT\.$' && continue
    gofmt_out+="$f"$'\n'
done
if [ -n "$gofmt_out" ]; then
    echo "not gofmt-clean (run gofmt -d on each, then see the note above):"
    printf '%s' "$gofmt_out"
    rc=1
    step_end fail
else
    echo "passed"
    step_end pass
fi

step_begin "rustfmt"
# Verifies every crate under ./rust is formatted with its OWN pinned rustfmt:
# scripts/dev/fmt_rust.sh --check runs `cargo fmt --all --check` inside each crate
# so the rustup proxy resolves the pin (imzero2 -> 1.96 / rustfmt 1.9.0,
# watermark -> 1.92 / rustfmt 1.8.0, h3bridge -> stable). Drift fails the build;
# fix with `scripts/dev/fmt_rust.sh`. Like h3_wasm_parity it skips gracefully when
# cargo or a pinned toolchain is absent, so local lint stays green for contributors
# not touching Rust and CI is the enforcer; h3bridge's stable pin shares that
# step's assumption that CI's stable matches the committed formatting. The
# `if out=$(...)` capture is required under `set -e` since --check exits non-zero
# on drift.
if out=$("$here/../dev/fmt_rust.sh" --check 2>&1); then
    # fmt_rust.sh is verbose even when clean (per-crate headers + rustfmt.toml
    # unstable-option warnings), so keep the step concise on success like its
    # siblings and surface the full output only on drift.
    echo "passed"
    step_end pass
else
    echo "$out"
    rc=1
    step_end fail
fi

step_begin "ids font SHA256SUMS"
# IDS font binary hash pinning (ADR-0034 §SD5). Each per-directory
# SHA256SUMS verifies the committed .ttf bytes; drift fails the build
# with a structured error naming the file and expected vs observed SHA.
ids_fonts_dir="$here/../../rust/imzero2/assets/fonts"
ids_fonts_ok=1
if [ -d "$ids_fonts_dir" ]; then
    for d in "$ids_fonts_dir"/*/; do
        if [ -f "$d/SHA256SUMS" ]; then
            (cd "$d" && sha256sum -c --quiet SHA256SUMS) || ids_fonts_ok=0
        fi
    done
fi
if [ "$ids_fonts_ok" -eq 0 ]; then
    rc=1
    step_end fail
else
    echo "passed"
    step_end pass
fi

step_begin "fetcher discipline"
# Enforces the imzero2 "fetchers only run in StateManager.Sync" rule
# (doc/skills/imzero2-fetchers/SKILLS.md). An inline Fetcher.Fetch* call
# in a render body deadlocks the loop the moment that body is wrapped in
# a deferred-block capture (dock.Tab, etable row, …), so this is a hard
# gate: a violation is a deadlock waiting to happen. The script prints
# its own findings and exits non-zero on any.
if "$here/fetcher-discipline.sh"; then
    step_end pass
else
    rc=1
    step_end fail
fi

step_begin "egui persistence gate"
# ADR-0148 (Update 2026-07-30): runtime and app state lives in boxer.facts
# and nowhere else. eframe's `persistence` feature enables
# `egui/persistence`, which serializes egui's IdTypeMap to a file under the
# home directory — and egui_table's TableState stores itself with
# insert_persisted while carrying col_widths. Enabling it would put column
# widths on disk AND make that copy authoritative, since TableState
# overrides the Go-supplied width on every frame after first show.
#
# A hard gate rather than a warning: the failure is silent at runtime (the
# app works, it just reads its widths from the wrong place), so review is
# the only other thing standing between a one-character edit and a second
# store. Matches the feature only when uncommented.
if grep -nE '^[[:space:]]*"persistence"' rust/imzero2/Cargo.toml; then
    echo "eframe 'persistence' is enabled in rust/imzero2/Cargo.toml — prohibited by ADR-0148."
    echo "egui memory would be serialized to disk, creating a second store for state that belongs in boxer.facts."
    rc=1
    step_end fail
else
    echo "passed"
    step_end pass
fi

step_begin "glyph coverage"
# A glyph a control is drawn from has to come from a font the CLIENT loads, not
# from the fallback chain — see the imzero2 skill §12 "Oversized, Off-Centre
# Glyph". This rasterises every non-ASCII glyph in a Go string literal against
# the real family chains from apphost.rs and fails on any that no face draws:
# those render as empty boxes, and the failure is invisible to every automated
# lane we have, because a scene's font stack is thinner than a desktop's.
#
# Baselined (scripts/ci/glyph-baseline.txt) rather than absolute: some sites are
# terminal text or a deliberate fallback showcase. A new site fails; an accepted
# one passes and carries its reason in that file.
#
# Skipped, not failed, when Pillow or fontconfig is missing, or when fontconfig
# does not return the families this audits against — a contributor's font setup
# is not a finding about the repo, and the script says so and exits 0.
if ! command -v python3 >/dev/null 2>&1 || ! python3 -c "import PIL" >/dev/null 2>&1; then
    echo "skipped: needs python3 + Pillow (python3-pillow)"
    step_end pass
elif ! command -v fc-match >/dev/null 2>&1; then
    echo "skipped: needs fontconfig (fc-match)"
    step_end pass
elif glyph_out=$("$here/../dev/glyph-audit.py" --baseline "$here/glyph-baseline.txt" 2>&1); then
    echo "$glyph_out" | tail -3
    step_end pass
else
    echo "$glyph_out"
    rc=1
    step_end fail
fi

step_begin "capslock"
# ADR-0026 §SD10: cross-checks the capabilities each app's own code
# exercises against its manifest declarations, in `compare` mode — a
# finding already accepted in baseline.go is reported and passes; a new
# one, or a baseline entry that no longer reproduces, fails.
#
# Runs as a plain Go test rather than a script over a JSON report, so it
# cannot drift from the code it checks. It is excluded from the default
# test gate (-short) because it costs ~15s and several GB of RSS building
# SSA over the apps' dependency cones; this is the step that runs it.
#
# Captured rather than piped: under `set -o pipefail` a `go test | grep`
# pipeline reports the grep's status too, so a run whose output the filter
# happens to drop entirely would read as a failure.
if cl_out=$(go test -tags "$tags" -count=1 \
    -run 'TestAnalyse_MatchesBaseline|TestAppSetIsComplete' \
    ./public/keelson/security/capslock/ 2>&1); then
    echo "passed"
    step_end pass
else
    printf '%s\n' "$cl_out" | grep -vE '^\{"level":"(warn|info|debug)"' || true
    rc=1
    step_end fail
fi

# === summary trailer ===
overall_dur=$(awk -v s="$overall_t0" -v e="$EPOCHREALTIME" 'BEGIN{printf "%.2f", e-s}')

# Compute name column width for alignment.
max_w=4
for n in "${step_names[@]}"; do
    [ ${#n} -gt $max_w ] && max_w=${#n}
done

echo ""
echo "=== summary ==="
for i in "${!step_names[@]}"; do
    printf "%-*s  %-4s  %7.2fs\n" "$max_w" "${step_names[i]}" "${step_statuses[i]}" "${step_durs[i]}"
done

failed=()
warned=()
for i in "${!step_names[@]}"; do
    case "${step_statuses[i]}" in
        fail) failed+=("${step_names[i]}") ;;
        warn) warned+=("${step_names[i]}") ;;
    esac
done

trailer="total: ${overall_dur}s  exit $rc"
if [ ${#failed[@]} -gt 0 ]; then
    trailer="$trailer  failing: ${failed[*]}"
fi
if [ ${#warned[@]} -gt 0 ]; then
    trailer="$trailer  warnings: ${warned[*]}"
fi
echo ""
echo "$trailer"

exit $rc
