package play

import (
	"strings"
	"testing"
)

func TestExtractParamsUserExample(t *testing.T) {
	residual, params, err := ExtractParams(`SET param_a=1; SET param_b=2; SELECT {param_a : UInt64}`)
	if err != nil {
		t.Fatalf("ExtractParams: %v", err)
	}
	if got, want := params["param_a"], "1"; got != want {
		t.Errorf("params[param_a] = %q, want %q", got, want)
	}
	if got, want := params["param_b"], "2"; got != want {
		t.Errorf("params[param_b] = %q, want %q", got, want)
	}
	if !strings.Contains(residual, "SELECT") {
		t.Errorf("residual missing SELECT: %q", residual)
	}
	if strings.Contains(residual, "param_a=1") || strings.Contains(residual, "param_b=2") {
		t.Errorf("residual still contains harvested SETs: %q", residual)
	}
	if !strings.Contains(residual, "{param_a") {
		t.Errorf("residual lost placeholder: %q", residual)
	}
}

func TestExtractParamsStringValueUnquoted(t *testing.T) {
	_, params, err := ExtractParams(`SET param_name = 'hello world'; SELECT {param_name : String}`)
	if err != nil {
		t.Fatalf("ExtractParams: %v", err)
	}
	if got, want := params["param_name"], "hello world"; got != want {
		t.Errorf("params[param_name] = %q, want %q", got, want)
	}
}

func TestExtractParamsStringEscapes(t *testing.T) {
	_, params, err := ExtractParams(`SET param_quoted = 'it\'s'; SELECT 1`)
	if err != nil {
		t.Fatalf("ExtractParams: %v", err)
	}
	if got, want := params["param_quoted"], "it's"; got != want {
		t.Errorf("params[param_quoted] = %q, want %q", got, want)
	}
}

func TestExtractParamsArrayPassthrough(t *testing.T) {
	_, params, err := ExtractParams(`SET param_arr = [1, 2, 3]; SELECT 1`)
	if err != nil {
		t.Fatalf("ExtractParams: %v", err)
	}
	got := params["param_arr"]
	if !strings.HasPrefix(got, "[") || !strings.HasSuffix(got, "]") {
		t.Errorf("param_arr = %q, want bracket form", got)
	}
}

func TestExtractParamsLeavesNonParamSetsIntact(t *testing.T) {
	residual, params, err := ExtractParams(`SET max_threads = 4; SET param_a = 1; SELECT {param_a : UInt64}`)
	if err != nil {
		t.Fatalf("ExtractParams: %v", err)
	}
	if _, ok := params["param_a"]; !ok {
		t.Errorf("params missing param_a: %v", params)
	}
	if _, ok := params["max_threads"]; ok {
		t.Errorf("params should not contain max_threads: %v", params)
	}
	if !strings.Contains(residual, "max_threads") {
		t.Errorf("residual lost max_threads SET: %q", residual)
	}
	if strings.Contains(residual, "param_a = 1") {
		t.Errorf("residual still has harvested SET: %q", residual)
	}
}

func TestExtractParamsRejectsMixedSetStmt(t *testing.T) {
	_, _, err := ExtractParams(`SET max_threads = 4, param_a = 1; SELECT {param_a : UInt64}`)
	if err == nil {
		t.Fatalf("expected error for mixed SET, got nil")
	}
	if !strings.Contains(err.Error(), "mixes") {
		t.Errorf("error doesn't mention mixing: %v", err)
	}
}

func TestExtractParamsNoParams(t *testing.T) {
	residual, params, err := ExtractParams(`SELECT 1`)
	if err != nil {
		t.Fatalf("ExtractParams: %v", err)
	}
	if len(params) != 0 {
		t.Errorf("params = %v, want empty", params)
	}
	if !strings.Contains(residual, "SELECT") {
		t.Errorf("residual = %q", residual)
	}
}

func TestExtractParamsSyntaxError(t *testing.T) {
	_, _, err := ExtractParams(`THIS IS NOT SQL`)
	if err == nil {
		t.Fatal("expected syntax error, got nil")
	}
}
