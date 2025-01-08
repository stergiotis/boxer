package application

import (
	cli "github.com/urfave/cli/v2"

	"github.com/stergiotis/boxer/public/config"
)

type Config struct {
	MainFontTTF   string
	ImGuiBinary   string
	MaxRelaunches int

	nValidationMessages  int
	MainFontSizeInPixels float32
	UseWasm              bool
	validated            bool
}

func (inst *Config) ToCliFlags(nameTransf config.NameTransformFunc, envVarNameTransf config.NameTransformFunc) []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{
			Name:  nameTransf("mainFontTTF"),
			Value: inst.MainFontTTF,
		},
		&cli.Float64Flag{
			Name:  nameTransf("mainFontSizeInPixels"),
			Value: float64(inst.MainFontSizeInPixels),
		},
		&cli.StringFlag{
			Name:     nameTransf("imGuiBinary"),
			Value:    inst.ImGuiBinary,
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
	}
}

func (inst *Config) FromContext(nameTransf config.NameTransformFunc, ctx *cli.Context) (nMessages int) {
	inst.MainFontTTF = ctx.String(nameTransf("mainFontTTF"))
	inst.MainFontSizeInPixels = float32(ctx.Float64(nameTransf("mainFontSizeInPixels")))
	inst.ImGuiBinary = ctx.String(nameTransf("imGuiBinary"))
	inst.UseWasm = ctx.Bool(nameTransf("useWasm"))
	inst.MaxRelaunches = ctx.Int(nameTransf("maxRelaunches"))
	return inst.Validate(true)
}

func (inst *Config) Validate(force bool) (nMessages int) {
	if inst.validated && !force {
		return inst.nValidationMessages
	}
	inst.nValidationMessages = nMessages
	inst.validated = true
	return
}

var _ config.Configer = (*Config)(nil)
