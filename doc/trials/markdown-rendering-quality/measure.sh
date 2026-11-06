#!/bin/bash
# measure.sh — one run of the markdown-rendering-quality trial (README §4).
#
# Writes runs/<slug>/: environment.md, metrics.json, metrics.md — and, once
# the person has voted, votes.json and votes.md. Everything bulky (pages,
# captures, SVG and tree sidecars, DOM dumps, the vote page and its pictures)
# lands under tmp/trials/markdown-rendering-quality/<slug>/, outside git.
#
# Usage:
#   doc/trials/markdown-rendering-quality/measure.sh <run-slug> [step...]
#
# Steps, in order, all by default: prepare scenes browsers metrics vote tally.
# `tally` needs votes.json exported from the vote page into <work>/vote/.
#
# Environment:
#   MDQ_CLIENT    the headless Rust client; default: the runner's own choice,
#                 the CPU rasterizer build if it is fresh, else the wgpu one
#   MDQ_CHROMIUM  chromium binary; default `chromium`
#   MDQ_FIREFOX   firefox binary; default `firefox`
#   GOTMPDIR/TMPDIR  where go builds, when /tmp is small
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
root=$(cd "$here/../../.." && pwd)
slug=${1:?usage: measure.sh <run-slug> [step...]}
shift
steps=${*:-prepare scenes browsers metrics vote tally}
run="$here/runs/$slug"
work="$root/tmp/trials/markdown-rendering-quality/$slug"
mkdir -p "$run" "$work"
tags="$(tr -d '\n' < "$root/tags"),integration"
client=${MDQ_CLIENT:-}
clientflag=()
if [ -n "$client" ]; then clientflag=(--clientBinary "$client"); fi
chromium=${MDQ_CHROMIUM:-chromium}
firefox=${MDQ_FIREFOX:-firefox}
W=1000
H=1400

harness() { # <test-name>
	( cd "$here/harness" && MDQ_WORK_DIR="$work" MDQ_RUN_DIR="$run" go test -count=1 -tags "$tags" -run "^$1\$" . )
}

has() { case " $steps " in *" $1 "*) return 0 ;; *) return 1 ;; esac; }

if has prepare; then
	{
		printf -- '---\ntype: reference\naudience: package maintainer\nstatus: draft\n---\n\n> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.\n\n'
		echo "# Environment — $slug"
		echo
		echo "- boxer commit: $(git -C "$root" rev-parse --short HEAD)$(git -C "$root" diff --quiet -- public/thestack/imzero2/egui2/widgets/markdown public/semistructured/markdown/obsidian public/keelson/designsystem/web rust/imzero2/src || echo " (the markdown widget, the obsidian package, the IDS web CSS or the Rust host modified in the working tree)")"
		echo "- Go: $(go version | cut -d' ' -f3-)"
		echo "- Rust client: ${client:-the runner default (headless-soft when fresh)} $(ls -t "$root"/rust/imzero2/target/headless*/release/imzero2 2>/dev/null | head -1 | xargs -r stat -c "(newest build %y)" | cut -d. -f1)"
		echo "- Chromium: $("$chromium" --version 2>/dev/null | head -1)"
		echo "- Firefox: $("$firefox" --version 2>/dev/null | head -1)"
		echo "- Viewport: ${W}x${H}, device scale 1, standard density"
		echo "- CPU: $(lscpu | sed -n 's/^Model name: *//p'), $(nproc) threads"
		echo "- Memory: $(free -h | awk '/^Mem:/ {print $2 " total, " $7 " available"}')"
		echo "- Kernel: $(uname -sr)"
		echo "- Fonts on the host the browsers may fall back to: $(fc-list : family | sort -u | wc -l) families"
	} > "$run/environment.md"
	harness TestPrepare
fi

if has scenes; then
	"$root/scripts/dev/scene.sh" --out "$work/captures" "${clientflag[@]}" --timeout 120s "$work/scenes"
fi

if has browsers; then
	mkdir -p "$work/ffprofile"
	for page in "$work"/html/*.html; do
		base=$(basename "$page" .html)
		url="file://$page"
		# Chromium: the picture, then the DOM with the geometry block.
		"$chromium" --headless=new --disable-gpu --hide-scrollbars --allow-file-access-from-files \
			--window-size=$W,$H --screenshot="$work/captures/$base-chromium.png" "$url" >/dev/null 2>&1 || echo "chromium screenshot failed: $base" >&2
		"$chromium" --headless=new --disable-gpu --hide-scrollbars --allow-file-access-from-files \
			--window-size=$W,$H --virtual-time-budget=5000 --dump-dom "$url" > "$work/captures/$base-chromium.dom.html" 2>/dev/null || echo "chromium dump-dom failed: $base" >&2
		# Firefox: the picture only. A separate profile, so a desktop instance
		# does not answer; the page must sit under a visible directory of the
		# home for the snap build to read it.
		"$firefox" --headless --no-remote --new-instance --profile "$work/ffprofile" \
			--window-size=$W,$H --screenshot "$work/captures/$base-firefox.png" "$url" >/dev/null 2>&1 || echo "firefox screenshot failed: $base" >&2
	done
fi

if has metrics; then
	harness TestMetrics
fi

if has vote; then
	harness TestVote
	echo "vote: open file://$work/vote/vote.html, vote, download votes.json into $work/vote/, then run: $0 $slug tally"
fi

if has tally; then
	if [ -f "$work/vote/votes.json" ]; then
		harness TestTally
	else
		echo "tally: no $work/vote/votes.json yet — skipped"
	fi
fi
