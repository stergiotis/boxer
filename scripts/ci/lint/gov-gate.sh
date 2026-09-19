#!/bin/bash
# The composite governance gate: buildtags, doclint, entry-points, file-naming,
# codelint, in one boxer process.
#
# One lint step. Runnable on its own; scripts/ci/lint.sh runs it with the
# others and documents the exit-status contract every step obeys. A failing
# gate step exits 1; a gate that only warns exits 2, which the `warn` marker on
# this step's line in that script's step list keeps out of the failure list.

set -e
set -o pipefail
here=$(dirname "$(readlink -f "$BASH_SOURCE")")
cd "$here/../../.."
tags="$(cat ./tags | tr -d "\n")"

# The composite gate boxer publishes to consuming repositories (ADR-0179):
# buildtags, doclint, entry-points, file-naming, codelint. This step does not
# spell that list out — public/gov/gate.DefaultSteps() is the single definition,
# so a step added there reaches boxer and every consumer at once, and boxer
# breaks first when it changes.
#
# gofmt and go vet are separate steps, outside the gate, on purpose: they must
# still run on a tree too broken to build this binary.
#
# `--exclude prompts/` withholds the LLM prompt books (ADR-0216 §SD2) from the
# doc rules. A prompt document's body IS the system prompt, verbatim, with no
# surrounding commentary to fence it off, so the doc-standard conventions here
# would not merely fail to apply — they would change the artifact. DL001 wants a
# type and a status, and every status it accepts then costs something untrue:
# draft adds DL004's pre-human-review banner as the prompt's FIRST LINE, stable
# asserts DL003 review metadata nobody produced, and deprecated / superseded
# say the corpus is retired. An applet book (ADR-0132) carries the stanza
# because its payload is fenced SQL and the prose around it really is
# documentation.
#
# Five steps in one process rather than five separate boxer.sh invocations also
# stops the binary being rebuilt per step (~38s -> ~18s here).
#
# The gate prints its own per-step summary; the driver folds the whole thing
# into one entry in its trailer. `if out=$(...)` is required under `set -e`,
# since the gate exits non-zero on any failing step.
gate_err=$(mktemp -t gate-err.XXXXXX)
if out=$(./boxer.sh gov gate \
        --tags "$tags" \
        --entry-points-baseline scripts/ci/entry-points-baseline.txt \
        --naming-baseline scripts/ci/naming-baseline.txt \
        --exclude 'prompts/' \
        2>"$gate_err"); then
    rm -f "$gate_err"
    printf '%s\n' "$out"
    if printf '%s' "$out" | grep -q 'warnings:'; then
        exit 2
    fi
    exit 0
fi
printf '%s\n' "$out"
cat "$gate_err"
rm -f "$gate_err"
exit 1
