package application

import (
	"slices"
	"strings"

	"github.com/urfave/cli/v2"

	"github.com/stergiotis/boxer/public/config"
)

type ImZeroClientConfig struct {
	AppTitle                string `json:"appTitle"`
	Fullscreen              string `json:"fullscreen"`
	InitialMainWindowWidth  string `json:"initialMainWindowWidth"`
	InitialMainWindowHeight string `json:"initialMainWindowHeight"`
	AllowMainWindowResize   string `json:"allowMainWindowResize"`
	ExportBasePath          string `json:"exportBasePath"`
	Vsync                   string `json:"vsync"`
	BackgroundColorRGBA     string `json:"backgroundColorRGBA"`
	BackdropFilter          string `json:"backdropFilter"`

	nValidationMessages int
	validated           bool
}

func (inst *ImZeroClientConfig) PassthroughArgs(args []string) (argsOut []string) {
	argsOut = args
	add := func(name string, val string) {
		if val != "" {
			argsOut = append(argsOut, "-"+name, val)
		}
	}
	// general
	add("appTitle", inst.AppTitle)
	add("fullscreen", inst.Fullscreen)
	add("initialMainWindowWidth", inst.InitialMainWindowWidth)
	add("initialMainWindowHeight", inst.InitialMainWindowHeight)
	add("allowMainWindowResize", inst.AllowMainWindowResize)
	add("exportBasePath", inst.ExportBasePath)
	// graphics
	add("vsync", inst.Vsync)
	add("backgroundColorRGBA", inst.BackgroundColorRGBA)
	add("backdropFilter", inst.BackdropFilter)
	return
}

func (inst *ImZeroClientConfig) ToCliFlags(nameTransf config.NameTransformFunc, envVarNameTransf config.NameTransformFunc) []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{
			Category: "general",
			Name:     nameTransf("appTitle"),
			Value:    inst.AppTitle,
		},
		&cli.StringFlag{
			Category: "general",
			Name:     nameTransf("fullscreen"),
			Value:    inst.Fullscreen,
		},
		&cli.StringFlag{
			Category: "general",
			Name:     nameTransf("initialMainWindowWidth"),
			Value:    inst.InitialMainWindowWidth,
		},
		&cli.StringFlag{
			Category: "general",
			Name:     nameTransf("initialMainWindowHeight"),
			Value:    inst.InitialMainWindowHeight,
		},
		&cli.StringFlag{
			Category: "general",
			Name:     nameTransf("allowMainWindowResize"),
			Value:    inst.AllowMainWindowResize,
		},
		&cli.StringFlag{
			Category: "general",
			Name:     nameTransf("exportBasePath"),
			Value:    inst.ExportBasePath,
		},
		&cli.StringFlag{
			Category: "graphics",
			Name:     nameTransf("vsync"),
			Value:    inst.Vsync,
		},
		&cli.StringFlag{
			Category: "graphics",
			Name:     nameTransf("backgroundColorRGBA"),
			Value:    inst.BackgroundColorRGBA,
		},
		&cli.StringFlag{
			Category: "graphics",
			Name:     nameTransf("backdropFilter"),
			Value:    inst.BackdropFilter,
		},
	}
}

func (inst *ImZeroClientConfig) FromContext(nameTransf config.NameTransformFunc, ctx *cli.Context) (nMessages int) {
	inst.AppTitle = ctx.String(nameTransf("appTitle"))
	inst.Fullscreen = ctx.String(nameTransf("fullscreen"))
	inst.InitialMainWindowWidth = ctx.String(nameTransf("initialMainWindowWidth"))
	inst.InitialMainWindowHeight = ctx.String(nameTransf("initialMainWindowHeight"))
	inst.AllowMainWindowResize = ctx.String(nameTransf("allowMainWindowResize"))
	inst.ExportBasePath = ctx.String(nameTransf("exportBasePath"))
	inst.Vsync = ctx.String(nameTransf("vsync"))
	inst.BackgroundColorRGBA = ctx.String(nameTransf("backgroundColorRGBA"))
	inst.BackdropFilter = ctx.String(nameTransf("backdropFilter"))
	return inst.Validate(true)
}

func (inst *ImZeroClientConfig) Validate(force bool) (nMessages int) {
	if inst.validated && !force {
		return inst.nValidationMessages
	}
	inst.nValidationMessages = nMessages
	inst.validated = true
	return
}

var _ config.Configer = (*ImZeroClientConfig)(nil)

type Config struct {
	ImZeroSkiaClientConfig *ImZeroClientConfig

	MainFontTTF      string
	ClientBinary     string
	ImZeroCmdOutFile string
	ImZeroCmdInFile  string
	MaxRelaunches    int

	nValidationMessages  int
	MainFontSizeInPixels float32
	UseWasm              bool
	validated            bool
}

func (inst *Config) ToCliFlags(nameTransf config.NameTransformFunc, envVarNameTransf config.NameTransformFunc) []cli.Flag {
	return slices.Concat([]cli.Flag{
		&cli.StringFlag{
			Name:  nameTransf("mainFontTTF"),
			Value: inst.MainFontTTF,
		},
		&cli.Float64Flag{
			Name:  nameTransf("mainFontSizeInPixels"),
			Value: float64(inst.MainFontSizeInPixels),
		},
		&cli.StringFlag{
			Name:     nameTransf("clientBinary"),
			Value:    inst.ClientBinary,
			Required: false,
		},
		&cli.StringFlag{
			Name:     nameTransf("imZeroCmdOutFile"),
			Value:    inst.ImZeroCmdOutFile,
			Required: false,
		},
		&cli.StringFlag{
			Name:     nameTransf("imZeroCmdInFile"),
			Value:    inst.ImZeroCmdInFile,
			Required: false,
		},
		&cli.BoolFlag{
			Name:     nameTransf("useWasm"),
			Value:    inst.UseWasm,
			Required: false,
		},
		&cli.IntFlag{
			Name:     nameTransf("maxRelaunches"),
			Value:    inst.MaxRelaunches,
			Required: false,
		},
		&cli.StringFlag{
			Name:  nameTransf("clientType"),
			Value: "egui",
		},
	}, inst.ImZeroSkiaClientConfig.ToCliFlags(clientPrefixNameTransf, clientPrefixNameTransf))
}
func clientPrefixNameTransf(name string) (newName string) {
	return "client" + strings.ToUpper(string(name[0])) + name[1:]
}

func (inst *Config) FromContext(nameTransf config.NameTransformFunc, ctx *cli.Context) (nMessages int) {
	inst.MainFontTTF = ctx.String(nameTransf("mainFontTTF"))
	inst.MainFontSizeInPixels = float32(ctx.Float64(nameTransf("mainFontSizeInPixels")))
	inst.ClientBinary = ctx.String(nameTransf("clientBinary"))
	inst.UseWasm = ctx.Bool(nameTransf("useWasm"))
	inst.MaxRelaunches = ctx.Int(nameTransf("maxRelaunches"))
	inst.ImZeroCmdInFile = ctx.String(nameTransf("imZeroCmdInFile"))
	inst.ImZeroCmdOutFile = ctx.String(nameTransf("imZeroCmdOutFile"))
	if inst.ImZeroSkiaClientConfig != nil {
		nMessages = inst.ImZeroSkiaClientConfig.FromContext(func(name string) (newName string) {
			return clientPrefixNameTransf(nameTransf(name))
		}, ctx)
	}
	nMessages += inst.Validate(true)
	return
}

func (inst *Config) Validate(force bool) (nMessages int) {
	if inst.validated && !force {
		return inst.nValidationMessages
	}
	if inst.ImZeroSkiaClientConfig != nil {
		nMessages += inst.ImZeroSkiaClientConfig.Validate(force)
	}
	inst.nValidationMessages = nMessages
	inst.validated = true
	return
}

var _ config.Configer = (*Config)(nil)
