package play

import (
	"encoding/binary"
	"os"
	"slices"

	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stergiotis/boxer/public/config"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/thestack/fffi2/runtime"
	"github.com/stergiotis/boxer/public/thestack/fffi2/typed"
	"github.com/stergiotis/boxer/public/thestack/imzero2/application"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/urfave/cli/v2"
)

const (
	flagURL      = "clickHouseUrl"
	flagUser     = "clickHouseUser"
	flagPassword = "clickHousePassword"
	flagInitSQL  = "initialSqlPath"
)

func NewCliCommand() *cli.Command {
	appCfg := &application.Config{
		MainFontTTF:            "",
		ImZeroSkiaClientConfig: &application.ImZeroClientConfig{},
	}
	return &cli.Command{
		Name:  "play",
		Usage: "graphical ClickHouse SQL playground",
		Flags: slices.Concat(
			appCfg.ToCliFlags(config.IdentityNameTransf, config.IdentityNameTransf),
			[]cli.Flag{
				&cli.StringFlag{
					Name:  flagURL,
					Value: "http://localhost:8123/",
					Usage: "ClickHouse HTTP endpoint",
				},
				&cli.StringFlag{
					Name:    flagUser,
					Value:   "default",
					EnvVars: []string{"CLICKHOUSE_USER"},
				},
				&cli.StringFlag{
					Name:    flagPassword,
					EnvVars: []string{"CLICKHOUSE_PASSWORD"},
				},
				&cli.PathFlag{
					Name:  flagInitSQL,
					Usage: "path to a .sql file pre-loaded into the editor",
				},
			},
		),
		Action: func(ctx *cli.Context) error {
			nMessages := appCfg.FromContext(config.IdentityNameTransf, ctx)
			if nMessages > 0 {
				return eb.Build().Int("nMessages", nMessages).Errorf("unable to load application config")
			}

			clientCfg := ClientConfig{
				URL:      ctx.String(flagURL),
				User:     ctx.String(flagUser),
				Password: ctx.String(flagPassword),
			}
			client := NewClient(clientCfg, nil)

			var initSQL string
			if p := ctx.Path(flagInitSQL); p != "" {
				b, err := os.ReadFile(p)
				if err != nil {
					return eh.Errorf("unable to read initial sql file: %w", err)
				}
				initSQL = string(b)
			} else {
				initSQL = "SELECT 1 AS hello, now() AS ts;"
			}

			store := NewQueryStore(client, memory.NewGoAllocator(), 100)
			playApp := NewPlayApp(client, store, initSQL)

			unm := runtime.NewUnmarshaller(nil, binary.NativeEndian, nil, nil)
			app, err := application.NewApplication(appCfg, unm)
			if err != nil {
				return eh.Errorf("unable to create application: %w", err)
			}
			return runApp(app, playApp)
		},
	}
}

func runApp(app *application.Application[*runtime.Unmarshaller], playApp *PlayApp) (err error) {
	topIds := c.NewWidgetIdStack()

	app.FffiEstablishedHandler = func(fffi *runtime.Fffi2[*runtime.Unmarshaller]) error {
		typed.SetCurrentFffiVar(fffi)
		return nil
	}
	app.BeforeFirstFrameInitHandler = func() error { return nil }
	app.RenderLoopHandler = func() error {
		c.CurrentApplicationState.StartServersideFrame()
		defer c.CurrentApplicationState.FinishServersideFrame()
		defer c.RequestRepaint()

		topIds.Reset()
		for range c.PanelTop(topIds.PrepareStr("menu")).KeepIter() {
			for range c.MenuBar().KeepIter() {
				for range c.MenuButton(c.Atoms().Text("File").Keep()).KeepIter() {
					if c.Button(topIds.PrepareStr("quit"),
						c.Atoms().Text("Quit").Keep()).
						SendResp().HasPrimaryClicked() {
						c.ContextSendViewPortCommandClose()
					}
				}
				c.AddSpace(styletokens.GapSections(playApp.density))
				c.WidgetsGlobalThemePreferenceButtons()
			}
		}
		return playApp.Render()
	}

	err = app.Launch()
	if err != nil {
		return eh.Errorf("unable to launch application: %w", err)
	}
	err = app.Run()
	if err != nil {
		return eh.Errorf("unable to run application: %w", err)
	}
	return
}
