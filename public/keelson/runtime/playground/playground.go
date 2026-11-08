// Package playground is the runtime's contract for "the playground" — the
// window another window hands its data to for composition (ADR-0361). It
// holds the launch config, its kind, the endpoint constants, the helper
// that opens the playground over `windowhost.open` by kind, and the
// producer side of an ad-hoc bundle: BundleSpec, the composer and publisher,
// the parsed applet document and the slot its parser is installed into. It
// has no app and registers none: importing it links the contract, not an
// implementation.
//
// The launch config is a generated codec, the only wire form — callers
// encode with buscodec.Encode, a playground decodes in Mount with the
// generated Unmarshal. The kind stays `playLaunch` and every column keeps
// its name, so a launch row or stored workingset written before the
// contract moved here decodes under this package (§SD1).
//
// Vocabulary (narrow, ADR-0135 cohort in the keelson vdd):
//
//   - [vdd.MembPlayLaunchSql] / [vdd.MembPlayLaunchBandsSql] — text,
//     the editor buffer and the Timeline bands buffer.
//   - [vdd.MembPlayLaunchAutoRun] / [vdd.MembPlayLaunchLive] — bool
//     flags.
//   - [vdd.MembPlayLaunchTab] — symbol, the initially focused tab id.
//   - [vdd.MembPlayLaunchDatasets] — string list, the dataset aliases the
//     window follows (ADR-0240 §SD7).
//
// Which app takes the kind is the host's decision (ADR-0361 §SD2): the open
// service resolves it from the launchable manifests declaring this kind, and
// a wiring site names one with app.Registry.DesignateForKind when several
// do.
package playground

import "time"

// Kind is the vocabulary kind name of this config — the value a playground
// app declares as its manifest's LaunchKind and that a caller sends as
// launchrequest.LaunchRequest.ConfigKind. The wire name keeps `play` for
// the role (ADR-0361 §SD1, Negative consequences): kinds are append-only
// and a new name would leave stored rows under the old one.
const Kind = "playLaunch"

// Endpoint values for the Launch.Endpoint field. Empty behaves like
// EndpointDefault.
const (
	// EndpointDefault keeps the opened window's env-configured ClickHouse
	// target.
	EndpointDefault = "default"
	// EndpointIntrospection binds the opened window's client to the
	// in-process keelson `/query` endpoint (introspect.LocalQueryEndpoint,
	// ADR-0094 §SD6) — where ad-hoc `keelson('<handle>')` datasets resolve
	// (ADR-0134). Ignored, with a warning, when no such endpoint is up.
	EndpointIntrospection = "introspection"
)

// Launch is the flat wire form of the playground's launch arguments.
//
// A destination must honour Sql, AutoRun, Endpoint, Datasets, DatasetNames
// and Bundle, since those carry the data a handover hands over. Tab,
// BandsSql and Live name one implementation's panes and live lane; another
// playground may ignore them, logging a warning as play does for an unknown
// tab (ADR-0361 §SD3).
type Launch struct {
	_ struct{} `kind:"playLaunch"`

	// FactId is the per-row event id (zero from producers; the launch
	// row's durable id is minted by the facts store on persist).
	FactId uint64 `lw:",id"`

	// NaturalKey is the entity natural key; the facts SetId is 2-arg.
	// These bus DTOs carry no separate key, so it stays the nil default.
	NaturalKey []byte `lw:",naturalKey"`

	// At is the event timestamp. time.Time matches the facts
	// SetTimestamp signature directly (strict 1:1); the leeway wire
	// truncates to u32 seconds, while the bus preserves full nanos.
	At time.Time `lw:",ts"`

	// Sql is the initial editor buffer. Must be honoured. Non-empty wins
	// over a playground's own seed (play's BOXER_PLAY_SQL override and the
	// persisted-session restore); empty falls through to that chain.
	Sql string `lw:"playLaunchSql,textArray"`

	// AutoRun runs the seeded buffer on mount. Must be honoured. Applied
	// whenever the window carries a config, matching the "config beats
	// env" rule.
	AutoRun bool `lw:"playLaunchAutoRun,bool"`

	// Live enables live re-run on the main lane. A hint: it names play's
	// live lane, and another playground may ignore it.
	Live bool `lw:"playLaunchLive,bool"`

	// BandsSql seeds the Timeline panel's bands editor when non-empty;
	// empty leaves the persisted/env-seeded bands untouched. A hint: it
	// names one of play's panes, and another playground may ignore it.
	BandsSql string `lw:"playLaunchBandsSql,textArray"`

	// Tab selects the initially focused body tab by id when non-empty; an
	// unknown id is a warning, not a mount error. A hint: another
	// playground may ignore it, logging a warning.
	Tab string `lw:"playLaunchTab,symbol"`

	// Endpoint binds the opened window's client to a query target. Must be
	// honoured. EndpointIntrospection points it at the in-process keelson
	// `/query` endpoint (where ad-hoc datasets resolve, ADR-0240); empty
	// or EndpointDefault keeps the env-configured ClickHouse. An
	// "introspection" request with no such endpoint up is a warning, not
	// a mount error — the window opens on the default target.
	Endpoint string `lw:"playLaunchEndpoint,symbol"`

	// Datasets lists the ad-hoc dataset aliases the window keeps bound
	// (ADR-0240 §SD7). Must be honoured. Sql names `keelson('<alias>')`,
	// the window resolves each alias to the newest live dataset at mount
	// and follows publish and retract from then on, exactly as a declared
	// applet does. Empty follows nothing.
	Datasets []string `lw:"playLaunchDatasets,stringArray"`

	// DatasetNames are the names the window binds Datasets under, index
	// for index — the names Sql reads in keelson('…') (ADR-0288
	// §SD3). Must be honoured. A publisher of window-scoped aliases
	// launches with its window's alias in Datasets and the base name here;
	// empty, or an empty entry, binds the alias under itself.
	DatasetNames []string `lw:"playLaunchDatasetNames,stringArray"`

	// Bundle names an ad-hoc bundle the window opens and follows (ADR-0288
	// §SD4). Must be honoured. Its document seeds the buffer, its datasets
	// are bound under their local names, and a republish reloads both. It
	// takes the window to the introspection endpoint, where datasets
	// resolve, and supersedes Sql and Datasets.
	Bundle string `lw:"playLaunchBundle,symbol"`
}
