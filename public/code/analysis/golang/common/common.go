package common

import (
	"go/types"

	"github.com/stergiotis/boxer/public/containers"
)

// PredeclaredTypes is populated from the go/types.Universe scope.
var PredeclaredTypes = containers.NewHashSet[string](128)

func init() {
	for _, name := range types.Universe.Names() {
		obj := types.Universe.Lookup(name)
		// We only care about predeclared *types* (int, string, error, any, comparable, etc.)
		if _, ok := obj.(*types.TypeName); ok {
			PredeclaredTypes.Add(name)
		}
	}
}

