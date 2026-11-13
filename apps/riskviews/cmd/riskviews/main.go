// Command riskviews is the imzero2 host binary with the riskviews app linked
// in beside the carousel's, so `riskviews imzero2 demo --launch riskviews` and
// `riskviews imzero2 scene` both see it. hostmain is the stock host's body.
package main

import (
	"github.com/urfave/cli/v3"

	_ "github.com/stergiotis/boxer/apps/riskviews"
	"github.com/stergiotis/boxer/public/observability/logging"
	"github.com/stergiotis/boxer/public/observability/vcs"
	"github.com/stergiotis/boxer/public/thestack/imzero2/hostmain"
)

var host = hostmain.New(hostmain.Options{},
	&cli.Command{Name: "riskviews", Version: vcs.BuildVersionInfo(), Before: logging.Apply})

func main() { host.Main() }
