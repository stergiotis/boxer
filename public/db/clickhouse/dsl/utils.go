package dsl

import (
	"encoding/json"
	chparser "github.com/AfterShip/clickhouse-sql-parser/parser"
	"github.com/fxamacker/cbor/v2"
	"github.com/stergiotis/boxer/public/observability/eh"
)

func deepCopyAst(ast chparser.Expr) (astOut chparser.Expr, err error) {
	return ast, nil
	var b []byte
	b, err = json.Marshal(ast)
	if err != nil {
		err = eh.Errorf("unable to marshall ast: %w", err)
		return
	}
	err = json.Unmarshal(b, &astOut)
	if err != nil {
		err = eh.Errorf("unable to unmarshall ast: %w", err)
		return
	}
	return
}
func deepCopyAst_(ast chparser.Expr) (astOut chparser.Expr, err error) {
	var b []byte
	b, err = cbor.Marshal(ast)
	if err != nil {
		err = eh.Errorf("unable to marshall ast: %w", err)
		return
	}
	err = cbor.Unmarshal(b, &astOut)
	if err != nil {
		err = eh.Errorf("unable to unmarshall ast: %w", err)
		return
	}
	return
}
