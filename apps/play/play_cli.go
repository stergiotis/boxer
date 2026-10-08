package play

import (
	"context"
	"encoding/binary"
	"os"
	"slices"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/config"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/thestack/fffi2/runtime"
	"github.com/stergiotis/boxer/public/thestack/fffi2/typed"
	"github.com/stergiotis/boxer/public/thestack/imzero2/application"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/urfave/cli/v3"
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
					Sources: cli.EnvVars("CLICKHOUSE_USER"),
				},
				&cli.StringFlag{
					Name:    flagPassword,
					Sources: cli.EnvVars("CLICKHOUSE_PASSWORD"),
				},
				&cli.StringFlag{
					Name:      flagInitSQL,
					Usage:     "path to a .sql file pre-loaded into the editor",
					TakesFile: true,
				},
			},
		),
		Action: func(ctx context.Context, cmd *cli.Command) error {
			nMessages := appCfg.FromContext(ctx, config.IdentityNameTransf, cmd)
			if nMessages > 0 {
				return eb.Build().Int("nMessages", nMessages).Errorf("unable to load application config")
			}

			// ADR-0108 §SD4: the standalone play binary is its own host, so
			// it makes the host's SQL registrations itself. A window host
			// embedding the app calls the same function.
			RegisterHostSql(log.Logger)

			clientCfg := ClientConfig{
				URL:          cmd.String(flagURL),
				User:         cmd.String(flagUser),
				Password:     cmd.String(flagPassword),
				AllowWrites:  AllowWrites.Get() != "",
				AppWritesOff: AppWrites.Get() == "off",
			}
			client := NewClient(clientCfg, nil)

			var initSQL string
			if p := cmd.String(flagInitSQL); p != "" {
				b, err := os.ReadFile(p)
				if err != nil {
					return eh.Errorf("unable to read initial sql file: %w", err)
				}
				initSQL = string(b)
			} else {
				initSQL = "SELECT 1 AS hello, now() AS ts;"
			}

			// NewLivePlayApp wires the live query graph and installs the
			// client's pre-execute SQL pipeline (standard passes + schema-aware
			// leeway name resolver, ADR-0108/0116), feeding the resolver to the
			// Diagnostics pane.
			playApp := NewLivePlayApp(client, initSQL, 100, DefaultRepository())

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
		// The engine's render pass, not PlayApp.Frame: this one-shot CLI host
		// has no keelson frame context to offer, and with a single instance
		// and no state store there is no capability it could carry.
		return playApp.render()
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
