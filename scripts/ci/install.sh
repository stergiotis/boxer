#!/bin/bash
set -ev
here=$(dirname "$(readlink -f "$BASH_SOURCE")")
cd "$here"
go get -tool github.com/mfridman/tparse@latest
go get -tool go.uber.org/nilaway/cmd/nilaway@latest
go get -tool cyclonedx/cyclonedx/cyclonedx-gomod@latest
go get -tool golang.org/x/vuln/cmd/govulncheck@latest
