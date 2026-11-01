package main

import (
	"github.com/urfave/cli/v3"

	"github.com/stergiotis/boxer/public/observability/logging"
	"github.com/stergiotis/boxer/public/observability/vcs"
	"github.com/stergiotis/boxer/public/thestack/imzero2/hostmain"
)

// The stock host links the apps the carousel imports and no others; hostmain
// is the rest.
var host = hostmain.New(hostmain.Options{}, &cli.Command{
	Name:      vcs.ModuleInfo(),
	Copyright: vcs.CopyrightInfo(),
	Version:   vcs.BuildVersionInfo(),
	Before:    logging.Apply,
})

func main() {
	host.Main()
}
