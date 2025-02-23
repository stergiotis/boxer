package compiletime

import (
	"github.com/urfave/cli/v2"

	"github.com/stergiotis/boxer/public/config"
)

type Config struct {
	GoCodeProlog        string
	IdlBuildTag         string
	IdlPackagePattern   string
	GoOutputFile        string
	CppOutputFile       string
	InterfaceOutputFile string
	FuncProcIdOffset    uint32
	NoThrow             bool
	validated           bool
	nValidationMessages int
}

func (inst *Config) ToCliFlags(nameTransf config.NameTransformFunc, envVarNameTransf config.NameTransformFunc) []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{
			Name:     nameTransf("idlBuildTag"),
			Required: inst.IdlBuildTag == "",
			Value:    inst.IdlBuildTag,
		},
		&cli.StringFlag{
			Name:     nameTransf("idlPackagePattern"),
			Required: inst.IdlBuildTag == "",
			Value:    inst.IdlBuildTag,
		},
		&cli.StringFlag{
			Name:     nameTransf("goOutputFile"),
			Required: inst.GoOutputFile == "",
			Value:    inst.GoOutputFile,
		},
		&cli.StringFlag{
			Name:     nameTransf("cppOutputFile"),
			Required: inst.CppOutputFile == "",
			Value:    inst.CppOutputFile,
		},
		&cli.StringFlag{
			Name:     nameTransf("interfaceOutputFile"),
			Required: false,
			Value:    inst.InterfaceOutputFile,
		},
		&cli.StringFlag{
			Name:     nameTransf("goCodeProlog"),
			Required: false,
			Value:    inst.GoCodeProlog,
		},
		&cli.UintFlag{
			Name:  "funcProcIdOffset",
			Value: uint(inst.FuncProcIdOffset),
		},
		&cli.BoolFlag{
			Name:  nameTransf("noThrow"),
			Value: inst.NoThrow,
		},
	}
}

func (inst *Config) FromContext(nameTransf config.NameTransformFunc, ctx *cli.Context) (nMessages int) {
	inst.IdlBuildTag = ctx.String(nameTransf("idlBuildTag"))
	inst.IdlPackagePattern = ctx.String(nameTransf("idlPackagePattern"))
	inst.GoOutputFile = ctx.String(nameTransf("goOutputFile"))
	inst.CppOutputFile = ctx.String(nameTransf("cppOutputFile"))
	inst.InterfaceOutputFile = ctx.String(nameTransf("interfaceOutputFile"))
	inst.FuncProcIdOffset = uint32(ctx.Uint(nameTransf("funcProcIdOffset")))
	inst.GoCodeProlog = ctx.String(nameTransf("goCodeProlog"))
	inst.NoThrow = ctx.Bool(nameTransf("noThrow"))
	nMessages = inst.Validate(true)
	return
}

func (inst *Config) Validate(force bool) (nMessages int) {
	if inst.validated && !force {
		return inst.nValidationMessages
	}

	inst.nValidationMessages = nMessages
	return
}

var _ config.Configer = (*Config)(nil)
