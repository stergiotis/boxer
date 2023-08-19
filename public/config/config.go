package config

import (
	"github.com/stergiotis/boxer/public/observability/eh"
	cli "github.com/urfave/cli/v2"
)

type NameTransformFunc func(name string) (newName string)

func IdentityNameTransf(name string) (newName string) {
	return name
}

type Configer interface {
	ToCliFlags(nameTransf NameTransformFunc, envVarNameTransf NameTransformFunc) []cli.Flag
	FromContext(nameTransf NameTransformFunc, ctx *cli.Context) (nMessages int)
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
		return 0, eh.Errorf("unable to resolve %q: possible values %q", s, nameAry)
	}
}
