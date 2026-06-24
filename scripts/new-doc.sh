#!/bin/bash
# Seed a new Diátaxis Markdown doc from the repo's canonical template.
#
# Usage: scripts/new-doc.sh <type> <destination>
#   <type>        explanation | howto | tutorial | adr
#   <destination> target path (e.g. public/foo/EXPLANATION.md,
#                 or doc/adr/0097-my-decision.md)
#
# Templates live under doc/templates/. The file is copied verbatim;
# contributors fill in placeholders and flip `status: draft` → `stable`
# + reviewed-by/reviewed-date after human review (see
# doc/DOCUMENTATION_STANDARD.md §4).
set -e
set -o pipefail

if [ $# -ne 2 ]; then
    echo "usage: $0 <explanation|howto|tutorial|adr> <destination>" >&2
    exit 2
fi

kind=$1
dest=$2
here=$(dirname "$(readlink -f "$BASH_SOURCE")")
templates="$here/../doc/templates"

case "$kind" in
    explanation) src="$templates/EXPLANATION.md.tmpl" ;;
    howto)       src="$templates/HOWTO.md.tmpl" ;;
    tutorial)    src="$templates/TUTORIAL.md.tmpl" ;;
    adr)         src="$templates/adr/0000-template.md" ;;
    *) echo "unknown type: $kind (expected explanation|howto|tutorial|adr)" >&2; exit 2 ;;
esac

if [ ! -f "$src" ]; then
    echo "template not found: $src" >&2
    exit 1
fi

if [ -e "$dest" ]; then
    echo "refusing to overwrite existing file: $dest" >&2
    exit 1
fi

mkdir -p "$(dirname "$dest")"
cp "$src" "$dest"
echo "seeded $dest from $src"
