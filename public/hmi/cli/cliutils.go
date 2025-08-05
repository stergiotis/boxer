package cli

import (
	"fmt"
	"slices"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/urfave/cli/v2"
)

func BuildEnumStringFlag[V fmt.Stringer](allValues []V, defaultValue V, name string) (inst *cli.StringFlag, parseFunc func(context *cli.Context) (validValue V)) {
	if len(allValues) == 0 {
		log.Panic().Msg("allValues can not be empty")
	}
	strs := make([]string, 0, len(allValues))
	for _, t := range allValues {
		strs = append(strs, t.String())
	}
	inst = &cli.StringFlag{
		Name:        name,
		Category:    "",
		DefaultText: "",
		FilePath:    "",
		Usage:       fmt.Sprintf("possible values are: %q", strs),
		Required:    false,
		Hidden:      false,
		HasBeenSet:  false,
		Value:       defaultValue.String(),
		Destination: nil,
		Aliases:     nil,
		EnvVars:     nil,
		TakesFile:   false,
		Action: func(context *cli.Context, s string) (err error) {
			if slices.Index(strs, s) < 0 {
				err = eb.Build().WithoutStack().Strs("possible", strs).Type("type", allValues[0]).Str("flagName", inst.Name).Errorf("unable to parse string enum cli flag")
				return
			}
			return
		},
	}
	parseFunc = func(context *cli.Context) (validValue V) {
		s := context.String(inst.Name)
		validValue = allValues[slices.Index(strs, s)]
		return
	}
	return
}
func BuildEnumStringFlagStr[V ~string](allValues []V, defaultValue V, name string) (inst *cli.StringFlag, parseFunc func(context *cli.Context) (validValue V)) {
	if len(allValues) == 0 {
		log.Panic().Msg("allValues can not be empty")
	}
	strs := make([]string, 0, len(allValues))
	for _, t := range allValues {
		strs = append(strs, string(t))
	}
	inst = &cli.StringFlag{
		Name:        name,
		Category:    "",
		DefaultText: "",
		FilePath:    "",
		Usage:       fmt.Sprintf("possible values are: %q", strs),
		Required:    false,
		Hidden:      false,
		HasBeenSet:  false,
		Value:       string(defaultValue),
		Destination: nil,
		Aliases:     nil,
		EnvVars:     nil,
		TakesFile:   false,
		Action: func(context *cli.Context, s string) (err error) {
			if slices.Index(strs, s) < 0 {
				err = eb.Build().WithoutStack().Strs("possible", strs).Type("type", allValues[0]).Str("flagName", inst.Name).Errorf("unable to parse string enum cli flag")
				return
			}
			return
		},
	}
	parseFunc = func(context *cli.Context) (validValue V) {
		s := context.String(inst.Name)
		validValue = allValues[slices.Index(strs, s)]
		return
	}
	return
}
