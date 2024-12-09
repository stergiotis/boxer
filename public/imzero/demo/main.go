//go:build !bootstrap

package demo

import (
	"time"

	cli "github.com/urfave/cli/v2"

	"github.com/stergiotis/boxer/public/config"
	"github.com/stergiotis/boxer/public/fffi/runtime"
	"github.com/stergiotis/boxer/public/imzero/application"
	"github.com/stergiotis/boxer/public/imzero/imcolortextedit"
	"github.com/stergiotis/boxer/public/imzero/imgui"
	"github.com/stergiotis/boxer/public/imzero/implot"
	"github.com/stergiotis/boxer/public/imzero/widgets/gostats"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

func NewCommand() *cli.Command {
	cfg := &application.Config{
		MainFontTTF: "",
	}
	return &cli.Command{
		Name:  "demo",
		Flags: cfg.ToCliFlags(config.IdentityNameTransf, config.IdentityNameTransf),
		Action: func(context *cli.Context) error {
			nMessages := cfg.FromContext(config.IdentityNameTransf, context)
			if nMessages > 0 {
				return eb.Build().Int("nMessages", nMessages).Errorf("unable to create config")
			}
			var app *application.Application
			var err error
			app, err = application.NewApplication(cfg)
			if err != nil {
				return eh.Errorf("unable to create application: %w", err)
			}

			return mainE(app)
		},
	}
}

func mainE(app *application.Application) (err error) {
	app.FffiEstablishedHandler = func(fffi *runtime.Fffi2) error {
		implot.SetCurrentFffiVar(fffi)
		imgui.SetCurrentFffiVar(fffi)
		imcolortextedit.SetCurrentFffiVar(fffi)
		return nil
	}
	app.BeforeFirstFrameInitHandler = func() error {
		return nil
	}
	render = MakeRenderBasicWidgets()
	renderStats := gostats.MakeStatRenderer()
	var dt time.Duration
	var writtenBytes int
	app.RenderLoopHandler = func(marshaller *runtime.Marshaller) error {
		marshaller.ResetWrittenBytes()
		t0 := time.Now()
		imgui.ShowDemoWindow()
		RenderDemo(app)
		renderStats(dt, writtenBytes)
		dt = time.Since(t0)
		return nil
	}
	err = app.Launch()
	if err != nil {
		err = eh.Errorf("unable to launch application: %w", err)
		return
	}
	err = app.Run()
	if err != nil {
		err = eh.Errorf("unable to run application: %w", err)
		return
	}

	return
}
