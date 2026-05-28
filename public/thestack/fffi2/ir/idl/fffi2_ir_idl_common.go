package idl

import (
	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
)

func checkNameClashesPedantic(ns1, ns2 []naming.StylableName) {
	for _, n2 := range ns2 {
		for _, n1 := range ns1 {
			if naming.Compare(n1, n2) == 0 {
				log.Panic().Stringer("name1", n1).Stringer("name2", n2).Msg("found clashing names")
			}
		}
	}
}
