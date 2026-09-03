package play

import (
	"github.com/stergiotis/boxer/public/fs/lading/ladingpolicy"
	"github.com/stergiotis/boxer/public/keelson/runtime/sysmfacts"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/go/marshallreflect"
	"github.com/stergiotis/boxer/public/semistructured/markdown/mddocfacts"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/componentview"
)

// componentStore is one generated `boxer.facts` store whose component DTOs
// the Detail pane can detect and decode (ADR-0075, play consumer). It pairs
// the store's baked membership-id snapshot — the same `<Store>MembershipIds`
// map its codec and its Scan filters were generated from, so the reflect read
// and the SQL read cannot disagree about an id — with the DTO types bound
// under the kind names `LW_COMPONENT` resolves (the Go type name).
//
// One Binder per store rather than one for all: a Binder carries a single
// membership lookup, and two stores are two vocabularies. Merging their
// snapshots would silently let a name in one shadow the same name in the
// other; keeping them apart makes a clash impossible rather than unlikely.
type componentStore struct {
	name string
	ids  map[string]map[string]uint64
	bind func(b *componentview.Binder) error

	binder *componentview.Binder
}

// playComponentStores lists the stores the Detail pane reads, and the kinds
// of each. It is the reflect-read twin of RegisterComponents (play_passes.go)
// and deliberately explicit for the same reason: a roster populated by
// import would depend on the link set. TestPlayComponentStores_MatchRegistry
// pins the two rosters to each other, so a kind added to one and not the
// other fails the build's tests rather than going quietly dark in one surface.
func playComponentStores() []componentStore {
	return []componentStore{
		{
			name: "Sysmetrics",
			ids:  sysmfacts.SysmetricsMembershipIds,
			bind: func(b *componentview.Binder) error {
				return firstErr(
					bindKind[sysmfacts.SysBattery](b, "SysBattery"),
					bindKind[sysmfacts.SysCpu](b, "SysCpu"),
					bindKind[sysmfacts.SysCpuInfo](b, "SysCpuInfo"),
					bindKind[sysmfacts.SysDiskIo](b, "SysDiskIo"),
					bindKind[sysmfacts.SysDiskMount](b, "SysDiskMount"),
					bindKind[sysmfacts.SysGpu](b, "SysGpu"),
					bindKind[sysmfacts.SysMem](b, "SysMem"),
					bindKind[sysmfacts.SysNet](b, "SysNet"),
					bindKind[sysmfacts.SysProc](b, "SysProc"),
					bindKind[sysmfacts.SysProcCmd](b, "SysProcCmd"),
					bindKind[sysmfacts.SysPsi](b, "SysPsi"),
					bindKind[sysmfacts.SysSocket](b, "SysSocket"),
					bindKind[sysmfacts.SysTopology](b, "SysTopology"),
				)
			},
		},
		{
			name: "Policy",
			ids:  ladingpolicy.PolicyMembershipIds,
			bind: func(b *componentview.Binder) error {
				return bindKind[ladingpolicy.LadingMount](b, "LadingMount")
			},
		},
		{
			name: "Mddoc",
			ids:  mddocfacts.MddocMembershipIds,
			bind: func(b *componentview.Binder) error {
				return firstErr(
					bindKind[mddocfacts.MdCodeBlock](b, "MdCodeBlock"),
					bindKind[mddocfacts.MdDoc](b, "MdDoc"),
					bindKind[mddocfacts.MdEmphasis](b, "MdEmphasis"),
					bindKind[mddocfacts.MdHeading](b, "MdHeading"),
					bindKind[mddocfacts.MdLink](b, "MdLink"),
					bindKind[mddocfacts.MdTag](b, "MdTag"),
				)
			},
		},
	}
}

// bindKind binds DTO type T under kind and adds it to b. The projection is
// the identity: the generic field renderer (play_detail_components.go) draws
// any DTO, so no per-kind carrier is needed.
func bindKind[T any](b *componentview.Binder, kind string) (err error) {
	bd, err := componentview.Bind(componentview.ComponentKindE(kind), func(r T) any { return r })
	if err != nil {
		return
	}
	return b.Add(bd)
}

func firstErr(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

// storeLookup flattens a store's per-kind membership-id snapshot into the one
// lookup its Binder takes. Within one store a name is one membership, so the
// per-kind maps overlap only on equal ids; an unequal pair means the snapshot
// is not what this function assumes, and is refused rather than resolved.
func storeLookup(store string, ids map[string]map[string]uint64) (lookup marshallreflect.MapLookup, err error) {
	flat := make(map[string]uint64, 64)
	for kind, m := range ids {
		for name, id := range m {
			if prev, ok := flat[name]; ok && prev != id {
				err = eb.Build().Str("store", store).Str("kind", kind).Str("membership", name).
					Errorf("membership name resolves to two ids within one store")
				return
			}
			flat[name] = id
		}
	}
	return marshallreflect.NewRegistryLookup(flat)
}
