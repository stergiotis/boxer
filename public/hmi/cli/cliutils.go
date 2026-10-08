package cli

import (
	"context"
	"fmt"
	"slices"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/urfave/cli/v3"
)

func BuildEnumStringFlag[V fmt.Stringer](allValues []V, defaultValue V, name string) (inst *cli.StringFlag, parseFunc func(ctx context.Context, cmd *cli.Command) (validValue V)) {
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
		Usage:       fmt.Sprintf("possible values are: %q", strs),
		Required:    false,
		Hidden:      false,
		Value:       defaultValue.String(),
		Destination: nil,
		Aliases:     nil,
		TakesFile:   false,
		Action: func(ctx context.Context, cmd *cli.Command, s string) (err error) {
			if slices.Index(strs, s) < 0 {
				err = eb.Build().WithoutStack().Strs("possible", strs).Type("type", allValues[0]).Str("flagName", inst.Name).Errorf("unable to parse string enum cli flag")
				return
			}
			return
		},
	}
	parseFunc = func(ctx context.Context, cmd *cli.Command) (validValue V) {
		s := cmd.String(inst.Name)
		validValue = allValues[slices.Index(strs, s)]
		return
	}
	return
}
func BuildEnumStringFlagStr[V ~string](allValues []V, defaultValue V, name string) (inst *cli.StringFlag, parseFunc func(ctx context.Context, cmd *cli.Command) (validValue V)) {
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
		Usage:       fmt.Sprintf("possible values are: %q", strs),
		Required:    false,
		Hidden:      false,
		Value:       string(defaultValue),
		Destination: nil,
		Aliases:     nil,
		TakesFile:   false,
		Action: func(ctx context.Context, cmd *cli.Command, s string) (err error) {
			if slices.Index(strs, s) < 0 {
				err = eb.Build().WithoutStack().Strs("possible", strs).Type("type", allValues[0]).Str("flagName", inst.Name).Errorf("unable to parse string enum cli flag")
				return
			}
			return
		},
	}
	parseFunc = func(ctx context.Context, cmd *cli.Command) (validValue V) {
		s := cmd.String(inst.Name)
		validValue = allValues[slices.Index(strs, s)]
		return
	}
	return
}
func removeNullValues[S ~[]E, E comparable](s S) S {
	var n E
	return slices.DeleteFunc(s, func(e E) bool {
		return e == n
	})
}
func CommandsNilRemoved(s ...*cli.Command) []*cli.Command {
	return removeNullValues(s)
}
func FlagsNilRemoved(s ...[]cli.Flag) (r []cli.Flag) {
	r = make([]cli.Flag, 0, len(s)*2)
	for _, f := range s {
		for _, f2 := range f {
			if f2 != nil {
				r = append(r, f2)
			}
		}
	}
	return
}
