#!/bin/bash
set -ev
here=$(dirname "$(readlink -f "$BASH_SOURCE")")
cd "$here"
go get -tool github.com/mfridman/tparse@latest
go get -tool go.uber.org/nilaway/cmd/nilaway@latest
