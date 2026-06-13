package marshallreflect_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/go/marshallreflect"
)

// Regression for review E-2: the reflect front-end classified by rt.Kind(), so
// named scalar types (`type severity uint8`, time.Duration, json.RawMessage)
// were accepted as their underlying builtin and then panicked at marshal time
// via reflect.Set on a non-assignable type — while the AST front-end rejects
// the source spelling at plan-build. Both front-ends must reject the same DTOs
// at plan-build (error, not accept-then-panic).

type severity uint8

type namedScalarDTO struct {
	_   struct{} `kind:"named"`
	Id  uint64   `lw:",id"`
	Sev severity `lw:"sev,symbol"`
}

func TestPlanFor_RejectsNamedScalarType(t *testing.T) {
	_, err := marshallreflect.PlanFor[namedScalarDTO]()
	require.Error(t, err, "a named scalar type (type severity uint8) must be rejected at plan-build")
}

type durationDTO struct {
	_  struct{}      `kind:"dur"`
	Id uint64        `lw:",id"`
	D  time.Duration `lw:"d,u64Array"`
}

func TestPlanFor_RejectsTimeDuration(t *testing.T) {
	_, err := marshallreflect.PlanFor[durationDTO]()
	require.Error(t, err, "time.Duration (named int64) must be rejected at plan-build")
}

type rawMsgDTO struct {
	_  struct{}        `kind:"raw"`
	Id uint64          `lw:",id"`
	R  json.RawMessage `lw:"r,blobArray"`
}

func TestPlanFor_RejectsJSONRawMessage(t *testing.T) {
	_, err := marshallreflect.PlanFor[rawMsgDTO]()
	require.Error(t, err, "json.RawMessage (named []byte) must be rejected at plan-build")
}

type unexportedTaggedDTO struct {
	_  struct{} `kind:"unexp"`
	Id uint64   `lw:",id"`
	x  string   `lw:"x,symbol"` //nolint:unused // intentionally unexported for the test
}

func TestPlanFor_RejectsUnexportedTaggedField(t *testing.T) {
	_, err := marshallreflect.PlanFor[unexportedTaggedDTO]()
	require.ErrorContains(t, err, "unexported", "an unexported tagged field must be rejected at plan-build")
}
