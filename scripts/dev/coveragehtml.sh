#!/bin/bash
set -ev
here=$(dirname "$(readlink -f "$BASH_SOURCE")")
cd "$here/../.."
if [[ -z "${GOCOVERDIR}" ]]; then
  echo "env variable GOCOVERDIR is not set"
  exit 1
fi
go tool covdata textfmt -i="$GOCOVERDIR" -o coverage.csv
go tool cover -html=./coverage.csv
