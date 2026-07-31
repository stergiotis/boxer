#!/bin/bash
# Flip a proposed ADR to accepted and strip the pre-acceptance banners.
#
# Usage: scripts/dev/adr-accept.sh [options] <adr>...
#   <adr>   a path (doc/adr/0118-extbin-....md) or a bare number (118, 0118)
#
# Options:
#   --reviewed-by <handle>   front-matter reviewer   (default: p@stergiotis)
#   --date <YYYY-MM-DD>      acceptance date         (default: today)
#   -n, --dry-run            print a unified diff, write nothing
#       --no-verify          skip the closing doclint run
#   -h, --help               this text
#
# What it edits, and why each edit is mechanical:
#
#   1. Front matter — `status: proposed` → `status: accepted`, and
#      `reviewed-by` / `reviewed-date` are set (uncommenting the template's
#      placeholders when present, inserting after `date:` otherwise). doclint
#      DL003 errors on an accepted doc missing either field.
#      `date:` is NOT touched: it records when the ADR was written, and 45 of
#      the accepted ADRs have it differ from `reviewed-date`.
#
#   2. The leading status banner — the `> **Status: proposed — pre-human-review.**`
#      blockquote is deleted whole, along with the blank lines around it, leaving
#      one. doclint DL004 requires that banner while proposed and forbids it once
#      accepted; the detection here mirrors DL004's DetectStatusBanner
#      (public/gov/doclint/gov_doclint_rule_dl004.go).
#
#   3. The `## Status` section's leading sentence — "Proposed — awaiting review
#      by …." becomes "Accepted <date>.". Only that first sentence: several
#      proposed ADRs fold substantive implementation status into the same
#      paragraph (0112's "S1 and S2 are implemented and tested", 0118's list of
#      what shipped), so collapsing the paragraph would lose it.
#
# Step 3 is the one that cannot be fully mechanical — prose after the leading
# sentence is often written against a not-yet-accepted ADR ("Awaiting review
# by …", "Promote to `accepted` after review by …") and reads stale afterwards.
# The script therefore prints the resulting `## Status` section and warns; read
# it and edit what no longer holds. Where the section does not lead with
# "Proposed" at all (ADR-0025 opens "Engineering-decided — …"), it is left
# untouched and flagged.
#
# See doc/DOCUMENTATION_STANDARD.md §1/§4 for the ADR lifecycle and the
# edit-policy tiers this flip sits at the boundary of.
set -e
set -o pipefail

here=$(dirname "$(readlink -f "$BASH_SOURCE")")
root=$(readlink -f "$here/../..")
adrdir="$root/doc/adr"

reviewed_by="p@stergiotis"
accept_date=$(date +%F)
dry_run=0
verify=1
targets=()

usage() {
    sed -n '2,12p' "$BASH_SOURCE" | sed 's/^# \{0,1\}//'
}

while [ $# -gt 0 ]; do
    case "$1" in
        --reviewed-by)
            [ $# -ge 2 ] || { echo "--reviewed-by needs a value" >&2; exit 2; }
            reviewed_by=$2; shift 2 ;;
        --date)
            [ $# -ge 2 ] || { echo "--date needs a value" >&2; exit 2; }
            accept_date=$2; shift 2 ;;
        -n|--dry-run) dry_run=1; shift ;;
        --no-verify)  verify=0; shift ;;
        -h|--help)    usage; exit 0 ;;
        --)           shift; targets+=("$@"); break ;;
        -*)           echo "unknown option: $1" >&2; usage >&2; exit 2 ;;
        *)            targets+=("$1"); shift ;;
    esac
done

if [ ${#targets[@]} -eq 0 ]; then
    usage >&2
    exit 2
fi

# doclint DL003 wants a real calendar date, not just four-two-two digits.
if ! date -d "$accept_date" +%F > /dev/null 2>&1 \
   || [ "$(date -d "$accept_date" +%F)" != "$accept_date" ]; then
    echo "not a YYYY-MM-DD date: $accept_date" >&2
    exit 2
fi

# resolve <arg> — a path as given, or NNN/NNNN matched against doc/adr/.
resolve() {
    local arg=$1 num matches
    if [ -f "$arg" ]; then
        readlink -f "$arg"
        return
    fi
    if [[ "$arg" =~ ^[0-9]{1,4}$ ]]; then
        num=$(printf '%04d' "$((10#$arg))")
        matches=("$adrdir/$num-"*.md)
        if [ ${#matches[@]} -eq 1 ] && [ -f "${matches[0]}" ]; then
            readlink -f "${matches[0]}"
            return
        fi
        if [ ${#matches[@]} -gt 1 ]; then
            echo "ambiguous ADR number $arg:" >&2
            printf '  %s\n' "${matches[@]}" >&2
            return 1
        fi
    fi
    echo "no such ADR: $arg" >&2
    return 1
}

flip() {
    local file=$1 out=$2
    awk -v reviewer="$reviewed_by" -v acceptdate="$accept_date" '
    function fail(msg) { print FILENAME ": " msg > "/dev/stderr"; failed = 1; exit 3 }
    # The counter is bumped in its own statement: written inline, awk lexes
    # "SUBSEP ++insCount[idx]" as a post-increment of SUBSEP.
    function addins(idx, text,   c) { c = insCount[idx] + 1; insCount[idx] = c; insText[idx SUBSEP c] = text }
    { L[NR] = $0 }
    END {
        if (failed) exit 3
        n = NR
        if (n < 3 || L[1] != "---") fail("no front-matter stanza")

        fmEnd = 0
        for (i = 2; i <= n; i++) if (L[i] == "---") { fmEnd = i; break }
        if (!fmEnd) fail("unterminated front matter")

        # ---- 1. front matter ----------------------------------------------
        statusLine = dateLine = rbLine = rdLine = 0
        for (i = 2; i < fmEnd; i++) {
            if      (L[i] ~ /^status:[ \t]/)                 statusLine = i
            else if (L[i] ~ /^date:[ \t]/)                   dateLine = i
            else if (L[i] ~ /^#?[ \t]*reviewed-by:/)         rbLine = i
            else if (L[i] ~ /^#?[ \t]*reviewed-date:/)       rdLine = i
        }
        if (!statusLine) fail("front matter has no status: field")
        st = L[statusLine]
        sub(/^status:[ \t]+/, "", st); sub(/[ \t]+$/, "", st)
        if (st == "accepted") fail("already accepted \x2d nothing to flip")
        if (st != "proposed") fail("status is " st ", expected proposed")
        L[statusLine] = "status: accepted"

        # Rewrite both reviewer fields from scratch so a commented template
        # placeholder, a live field and an absent field all land the same way.
        if (rbLine) del[rbLine] = 1
        if (rdLine) del[rdLine] = 1
        anchor = dateLine ? dateLine : statusLine
        addins(anchor, "reviewed-by: \"" reviewer "\"")
        addins(anchor, "reviewed-date: " acceptdate)

        # ---- 2. the leading status banner ----------------------------------
        b = 0
        for (i = fmEnd + 1; i <= n; i++) if (L[i] !~ /^[ \t]*$/) { b = i; break }
        if (b && L[b] ~ /^[ \t]*>/) {
            t = L[b]
            sub(/^[> \t]+/, "", t)
            if (t ~ /^\*\*Status: (draft|proposed|stable|accepted|deprecated|superseded) — pre-human-review\.\*\*/) {
                if (t !~ /^\*\*Status: proposed —/)
                    print FILENAME ": note: leading banner did not announce proposed; removed anyway" > "/dev/stderr"
                e = b
                while (e + 1 <= n && L[e + 1] ~ /^[ \t]*>/) e++
                s = b
                while (s - 1 > fmEnd && L[s - 1] ~ /^[ \t]*$/) s--
                while (e + 1 <= n && L[e + 1] ~ /^[ \t]*$/) e++
                for (i = s; i <= e; i++) del[i] = 1
                addins(s - 1, "")   # one blank line where the banner block was
                bannerGone = 1
            }
        }
        if (!bannerGone)
            print FILENAME ": note: no leading pre-human-review banner found" > "/dev/stderr"

        # ---- 3. the ## Status lead sentence --------------------------------
        sec = 0
        for (i = fmEnd + 1; i <= n; i++) if (L[i] ~ /^##[ \t]+Status[ \t]*$/) { sec = i; break }
        if (!sec) {
            print FILENAME ": warn: no \x23\x23 Status section; nothing rewritten there" > "/dev/stderr"
        } else {
            ps = 0
            for (i = sec + 1; i <= n; i++) {
                if (L[i] ~ /^[ \t]*$/) continue
                if (L[i] ~ /^#/) break
                ps = i; break
            }
            if (!ps) {
                print FILENAME ": warn: \x23\x23 Status section is empty" > "/dev/stderr"
            } else if (L[ps] !~ /^Proposed([^A-Za-z]|$)/) {
                print FILENAME ": warn: \x23\x23 Status does not lead with \"Proposed\"; left untouched \x2d edit it by hand" > "/dev/stderr"
            } else {
                pe = ps
                while (pe + 1 <= n && L[pe + 1] !~ /^[ \t]*$/ && L[pe + 1] !~ /^#/) pe++
                # First sentence end: a period followed by whitespace or the end
                # of the line. Requiring the trailing space is what keeps the
                # dots inside a relative link target (../../public/x) from
                # reading as a sentence boundary.
                termLine = termCol = 0
                for (i = ps; i <= pe && !termLine; i++) {
                    if (match(L[i], /\.([ \t]|$)/)) { termLine = i; termCol = RSTART }
                }
                if (!termLine) {
                    print FILENAME ": warn: no sentence end in the \x23\x23 Status lead; left untouched" > "/dev/stderr"
                } else {
                    L[ps] = "Accepted " acceptdate "." substr(L[termLine], termCol + 1)
                    for (i = ps + 1; i <= termLine; i++) del[i] = 1
                }
            }
        }

        # ---- emit -----------------------------------------------------------
        for (i = 1; i <= n; i++) {
            if (!del[i]) print L[i]
            for (k = 1; k <= insCount[i]; k++) print insText[i SUBSEP k]
        }
    }
    ' "$file" > "$out"
}

# print the ## Status section of a file, for the post-flip review warning.
show_status_section() {
    awk '
    /^##[ \t]+Status[ \t]*$/ { if (!seen) { seen = 1; inSec = 1; print "  " $0; next } }
    inSec && /^##[ \t]/ { inSec = 0 }
    inSec { print "  " $0 }
    ' "$1"
}

files=()
for t in "${targets[@]}"; do
    f=$(resolve "$t") || exit 1
    files+=("$f")
done

tmp=$(mktemp)
cleanup() {
    rv=$?
    rm -f -- "$tmp"
    exit $rv
}
trap 'cleanup' EXIT

changed=()
for f in "${files[@]}"; do
    rel=${f#"$root"/}
    flip "$f" "$tmp"

    if [ "$dry_run" -eq 1 ]; then
        echo "=== $rel (dry run)"
        diff -u --label "a/$rel" --label "b/$rel" "$f" "$tmp" || true
        continue
    fi

    cat "$tmp" > "$f"
    changed+=("$f")
    echo "accepted $rel (reviewed-by \"$reviewed_by\", reviewed-date $accept_date)"

    # The prose after the lead sentence was written against a proposed ADR and
    # is the part no rule can check. Put it in front of the caller.
    echo "--- review the resulting Status section, it may still read as pending:" >&2
    show_status_section "$f" >&2
    echo >&2
done

if [ "$dry_run" -eq 1 ]; then
    exit 0
fi

# Any remaining banner deeper in the body is outside DL004's leading-line check
# but still contradicts an accepted status.
for f in "${changed[@]}"; do
    if grep -n -- "pre-human-review" "$f" > /dev/null; then
        echo "warn: ${f#"$root"/} still mentions pre-human-review:" >&2
        grep -n -- "pre-human-review" "$f" >&2
    fi
done

if [ "$verify" -eq 1 ] && [ ${#changed[@]} -gt 0 ]; then
    echo "running doclint on the flipped ADRs..." >&2
    "$root/boxer.sh" gov doclint --min-severity warn "${changed[@]}"
fi
