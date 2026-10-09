package config

import (
	"context"
	cli "github.com/urfave/cli/v3"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

type NameTransformFunc func(name string) (newName string)

func IdentityNameTransf(name string) (newName string) {
	return name
}

type ConfigerI interface {
	ToCliFlags(nameTransf NameTransformFunc, envVarNameTransf NameTransformFunc) []cli.Flag
	FromContext(ctx context.Context, nameTransf NameTransformFunc, cmd *cli.Command) (nMessages int)
	Validate(force bool) (nMessages int)
}

func GenerateResolverFunc(nameAry []string, emptyMeansZero bool) func(s string) (int, error) {
	return func(s string) (int, error) {
		if s == "" && emptyMeansZero {
			return 0, nil
		}
		for i, a := range nameAry {
			if s == a {
				return i, nil
			}
		}
		return 0, eb.Build().Str("value", s).Strs("possibleValues", nameAry).Errorf("unable to resolve value")
	}
}
