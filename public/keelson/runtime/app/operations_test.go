package app

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type opsArgs struct {
	Text      string
	ResultRef string
}

type opsResult struct {
	Rev uint64
}

func validCatalog() *OperationsCatalog {
	return &OperationsCatalog{
		Resources: []ResourceSpec{{Name: "doc", Summary: "the document"}},
		Operations: []OperationSpec{
			{Name: "get_doc", Version: 1, Summary: "read the document", Class: OperationClassQuery, Effect: OperationEffectNone,
				Reads: []string{"doc"}, Result: reflect.TypeFor[opsResult]()},
			{Name: "set_doc", Version: 1, Summary: "replace the document", Class: OperationClassCommand, Effect: OperationEffectDocument,
				Writes: []string{"doc"}, Args: reflect.TypeFor[opsArgs](), Refs: []string{"result_ref"}, Agents: true},
		},
	}
}

func TestOperationsCatalogValidate(t *testing.T) {
	require.NoError(t, validCatalog().Validate())
	var none *OperationsCatalog
	require.NoError(t, none.Validate())

	for name, mutate := range map[string]func(c *OperationsCatalog){
		"no operations":         func(c *OperationsCatalog) { c.Operations = nil },
		"bad name":              func(c *OperationsCatalog) { c.Operations[0].Name = "Get-Doc" },
		"zero version":          func(c *OperationsCatalog) { c.Operations[0].Version = 0 },
		"no summary":            func(c *OperationsCatalog) { c.Operations[0].Summary = "" },
		"duplicate operation":   func(c *OperationsCatalog) { c.Operations[1].Name = "get_doc" },
		"duplicate resource":    func(c *OperationsCatalog) { c.Resources = append(c.Resources, ResourceSpec{Name: "doc"}) },
		"query with effect":     func(c *OperationsCatalog) { c.Operations[0].Effect = OperationEffectView },
		"query writes":          func(c *OperationsCatalog) { c.Operations[0].Writes = []string{"doc"} },
		"command without":       func(c *OperationsCatalog) { c.Operations[1].Effect = OperationEffectNone },
		"document writes none":  func(c *OperationsCatalog) { c.Operations[1].Writes = nil },
		"undeclared resource":   func(c *OperationsCatalog) { c.Operations[0].Reads = []string{"nope"} },
		"non-struct args":       func(c *OperationsCatalog) { c.Operations[1].Args = reflect.TypeFor[string]() },
		"unencodable result":    func(c *OperationsCatalog) { c.Operations[0].Result = reflect.TypeFor[struct{ F any }]() },
		"ref names no field":    func(c *OperationsCatalog) { c.Operations[1].Refs = []string{"missing"} },
		"refs without args":     func(c *OperationsCatalog) { c.Operations[0].Refs = []string{"x"} },
		"unspecified class":     func(c *OperationsCatalog) { c.Operations[0].Class = OperationClassUnspecified },
		"unspecified effect":    func(c *OperationsCatalog) { c.Operations[1].Effect = OperationEffectUnspecified },
		"invalid resource name": func(c *OperationsCatalog) { c.Resources[0].Name = "Doc" },
	} {
		c := validCatalog()
		mutate(c)
		assert.Error(t, c.Validate(), name)
	}
}

func TestRegistryWithdrawsABadCatalogAndKeepsTheApp(t *testing.T) {
	r := NewRegistry()
	m := validManifestForOps("github.com/x/apps/bad")
	m.Operations = validCatalog()
	m.Operations.Operations[0].Version = 0
	require.NoError(t, r.RegisterFactory(m, func() (AppI, error) { return nil, nil }))
	got, ok := r.LookupManifest(m.Id)
	require.True(t, ok)
	assert.Nil(t, got.Operations)
	assert.Contains(t, r.OperationsDiagnostic(m.Id), "version")
	regs := r.Registrations()
	require.Len(t, regs, 1)
	assert.NotEmpty(t, regs[0].OperationsDiagnostic)
}

func TestRegistryWithdrawsACatalogOnASingleton(t *testing.T) {
	r := NewRegistry()
	m := validManifestForOps("github.com/x/apps/single")
	m.Operations = validCatalog()
	a, err := NewLegacyFuncApp(m, func() error { return nil })
	require.NoError(t, err)
	require.NoError(t, r.Register(a))
	got, _ := r.LookupManifest(m.Id)
	assert.Nil(t, got.Operations)
	assert.Contains(t, r.OperationsDiagnostic(m.Id), "factory")
}

func TestRegistryKeepsAValidCatalog(t *testing.T) {
	r := NewRegistry()
	m := validManifestForOps("github.com/x/apps/good")
	m.Operations = validCatalog()
	require.NoError(t, r.RegisterFactory(m, func() (AppI, error) { return nil, nil }))
	got, _ := r.LookupManifest(m.Id)
	require.NotNil(t, got.Operations)
	spec, ok := got.Operations.Lookup("set_doc")
	require.True(t, ok)
	assert.True(t, spec.Agents)
	assert.Empty(t, r.OperationsDiagnostic(m.Id))
}

func validManifestForOps(id AppIdT) Manifest {
	return Manifest{Id: id, Display: "x", Surface: SurfaceWindowed, Topics: []TopicT{AllTopics[0]}, Summary: "does x"}
}

func TestRegistryRefusesACapabilityOnOperationSubjects(t *testing.T) {
	r := NewRegistry()
	m := validManifestForOps("github.com/x/apps/snoop")
	m.Caps = []SubjectFilter{{Pattern: "app.>", Direction: CapDirectionPub}}
	require.Error(t, r.RegisterFactory(m, func() (AppI, error) { return nil, nil }))
	m.Caps = []SubjectFilter{{Pattern: "app.snoop.request.>", Direction: CapDirectionPub}}
	require.NoError(t, r.RegisterFactory(m, func() (AppI, error) { return nil, nil }))
}
