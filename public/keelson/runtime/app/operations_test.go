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
		"consent on a document command": func(c *OperationsCatalog) {
			c.Operations[1].Consent = OperationConsent{Class: "publish", Arg: "text"}
		},
		"consent names no field": func(c *OperationsCatalog) {
			c.Operations[1].Effect, c.Operations[1].Consent = OperationEffectConsequential, OperationConsent{Class: "publish", Arg: "missing"}
		},
		"consent class not the platform's": func(c *OperationsCatalog) {
			c.Operations[1].Effect, c.Operations[1].Consent = OperationEffectConsequential, OperationConsent{Class: "export", Arg: "text"}
		},
		"dry run names no query":   func(c *OperationsCatalog) { c.Operations[1].DryRun = "nope" },
		"dry run names a command":  func(c *OperationsCatalog) { c.Operations[1].DryRun = c.Operations[1].Name },
		"query declares a rebuild": func(c *OperationsCatalog) { c.Operations[0].Rebuild = OperationRebuildRefold },
		"query is asynchronous":    func(c *OperationsCatalog) { c.Operations[0].Async = true },
		"query has a dry-run twin": func(c *OperationsCatalog) { c.Operations[0].DryRun = "get_doc" },
		"rebuild out of range":     func(c *OperationsCatalog) { c.Operations[1].Rebuild = OperationRebuildE(9) },
		"fork out of range":        func(c *OperationsCatalog) { c.Operations[0].Fork = OperationForkE(9) },
		"command read-shared":      func(c *OperationsCatalog) { c.Operations[1].Fork = OperationForkReadShared },
		"query per-instance":       func(c *OperationsCatalog) { c.Operations[0].Fork = OperationForkPerInstance },
		"rebuild without writes": func(c *OperationsCatalog) {
			c.Operations[1].Effect, c.Operations[1].Writes, c.Operations[1].Rebuild = OperationEffectConsequential, nil, OperationRebuildDurable
		},
		"dry-run twin takes other arguments": func(c *OperationsCatalog) {
			c.Operations[1].DryRun, c.Operations[0].Args = c.Operations[0].Name, reflect.TypeFor[struct{ Other string }]()
		},
	} {
		c := validCatalog()
		mutate(c)
		assert.Error(t, c.Validate(), name)
	}
}

// The stateful-tool contract (ADR-0315 §SD4–§SD7) is declared on a
// command: its rebuild and fork classes, whether it settles later, and a
// query of the catalog as its dry-run twin.
func TestACommandDeclaresTheStatefulToolContract(t *testing.T) {
	c := validCatalog()
	c.Operations[1].Rebuild, c.Operations[1].Fork, c.Operations[1].Async, c.Operations[1].DryRun = OperationRebuildRefold, OperationForkPerInstance, true, c.Operations[0].Name
	c.Operations[0].Args = c.Operations[1].Args
	require.NoError(t, c.Validate())
	c.Operations[0].Fork = OperationForkReadShared
	require.NoError(t, c.Validate(), "a query fans out read-shared")
	assert.Equal(t, "refold", c.Operations[1].Rebuild.String())
	assert.Equal(t, "per-instance", c.Operations[1].Fork.String())
	assert.Equal(t, "unspecified", OperationRebuildUnspecified.String())
	assert.Equal(t, "unspecified", OperationForkUnspecified.String())
}

func TestAConsequentialCommandMayDeclareAConsent(t *testing.T) {
	c := validCatalog()
	c.Operations[1].Effect, c.Operations[1].Consent = OperationEffectConsequential, OperationConsent{Class: "publish", Arg: "text"}
	require.NoError(t, c.Validate())
	assert.Equal(t, "publish:sales_", c.Operations[1].Consent.Destination("sales_"))
}

func TestRegistryRefusesABadCatalog(t *testing.T) {
	r := NewRegistry()
	m := validManifestForOps("github.com/x/apps/bad")
	m.Operations = validCatalog()
	m.Operations.Operations[0].Version = 0
	err := r.RegisterFactory(m, func() (AppI, error) { return nil, nil })
	require.ErrorContains(t, err, "version")
	_, ok := r.LookupManifest(m.Id)
	assert.False(t, ok)
}

func TestRegistryRefusesACatalogOnASingleton(t *testing.T) {
	r := NewRegistry()
	m := validManifestForOps("github.com/x/apps/single")
	m.Operations = validCatalog()
	a, err := NewLegacyFuncApp(m, func() error { return nil })
	require.NoError(t, err)
	require.ErrorContains(t, r.Register(a), "factory")
}

type plainApp struct{ m Manifest }

func (inst *plainApp) Manifest() (m Manifest)                { return inst.m }
func (inst *plainApp) Mount(ctx MountContextI) (err error)   { return }
func (inst *plainApp) Frame(ctx FrameContextI) (err error)   { return }
func (inst *plainApp) Unmount(ctx MountContextI) (err error) { return }

func TestOpenRefusesAnInstanceWithoutOperationsAppI(t *testing.T) {
	r := NewRegistry()
	m := validManifestForOps("github.com/x/apps/noops")
	m.Operations = validCatalog()
	require.NoError(t, r.RegisterFactory(m, func() (AppI, error) { return &plainApp{m: m}, nil }))
	_, err := r.Open(m.Id)
	require.ErrorContains(t, err, "OperationsAppI")
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
}

func validManifestForOps(id AppIdT) Manifest {
	return Manifest{Id: id, Display: "x", Surface: SurfaceWindowed, Topics: []TopicT{Topics()[0]}, Summary: "does x"}
}

func TestRegistryRefusesACapabilityOnOperationSubjects(t *testing.T) {
	r := NewRegistry()
	m := validManifestForOps("github.com/x/apps/snoop")
	m.Caps = []SubjectFilter{{Pattern: "app.>", Direction: CapDirectionPub}}
	require.Error(t, r.RegisterFactory(m, func() (AppI, error) { return nil, nil }))
	m.Caps = []SubjectFilter{{Pattern: "app.snoop.request.>", Direction: CapDirectionPub}}
	require.NoError(t, r.RegisterFactory(m, func() (AppI, error) { return nil, nil }))
}

// A direct operation is a tool of an agent's model, so it is one agents may
// call, and a catalog offers few of them, since each is in every request.
func TestADirectOperationIsAnAgentOperationAndFew(t *testing.T) {
	c := validCatalog()
	c.Operations[1].Direct = true
	require.NoError(t, c.Validate())

	c.Operations[0].Direct = true
	err := c.Validate()
	require.Error(t, err, "get_doc is not exposed to agents")
	assert.Contains(t, err.Error(), "operation get_doc: a direct operation is a tool of an agent's model")

	c.Operations[0].Agents = true
	require.NoError(t, c.Validate(), "two direct operations are within the bound")
	c.Operations = append(c.Operations, OperationSpec{Name: "get_doc_too", Version: 1, Summary: "read it again", Class: OperationClassQuery,
		Effect: OperationEffectNone, Reads: []string{"doc"}, Agents: true, Direct: true})
	err = c.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "operation get_doc_too: a catalog declares at most 2 direct operations")
}

// A refusal never asks for a destination that names nothing after its
// class: no grant entry of that shape covers a call.
func TestARefusalAsksForNoDestinationWithoutAName(t *testing.T) {
	var refusal *OperationRefusal
	require.ErrorAs(t, RefuseForDestinations("why", "clickhouse:", "clickhouse-session: ", ""), &refusal)
	assert.Nil(t, refusal.Destinations)
	require.ErrorAs(t, RefuseForDestinations("why", "clickhouse:", "clickhouse:127.0.0.1:8123", "llm"), &refusal)
	assert.Equal(t, []string{"clickhouse:127.0.0.1:8123", "llm"}, refusal.Destinations)
}
