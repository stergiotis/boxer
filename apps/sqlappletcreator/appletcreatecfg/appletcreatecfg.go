// Package appletcreatecfg is the launch contract of the SQL applet creator
// (ADR-0132 Update "O4" / ADR-0135 §SD7): the target app id, the config
// kind, and the typed arguments an app opens a creator window with over
// `windowhost.open`. It is the neutral leaf both sides import — the
// playground (apps/play) encodes and sends; the creator
// (apps/sqlappletcreator) declares it as its manifest LaunchKind and decodes
// it in Mount — so play never imports the creator app itself, only this
// contract (the appletstore.SubjectSave precedent).
//
// The DTO follows the runtime codec grammar and the generated codec is the
// only wire form — callers encode with buscodec.Encode, the creator decodes
// with buscodec.Decode.
//
// Vocabulary (narrow, the ADR-0135 launch cohort in the keelson vdd):
//
//   - [vdd.MembAppletCreateSql] — text, the buffer to author into an applet.
//   - [vdd.MembAppletCreateEndpoint] — symbol, the endpoint the buffer was
//     authored against (stamped into the applet document's frontmatter).
package appletcreatecfg

import "time"

// AppId is the applet creator's registered manifest id — the target an app
// names in a `windowhost.open` request. Kept here (not in the creator app's
// package) so a requester imports the contract without importing the app.
// Untyped so it flows into app.AppIdT contexts (Manifest.Id, RequestOpen)
// without this leaf depending on the app package.
const AppId = "github.com/stergiotis/boxer/apps/sqlappletcreator"

// Kind is the vocabulary kind name of this config — the value the creator's
// manifest declares as LaunchKind and callers put in
// launchrequest.LaunchRequest.ConfigKind.
const Kind = "appletCreate"

// Endpoint values for the AppletCreate.Endpoint field. Empty behaves like
// EndpointDefault.
const (
	// EndpointDefault is the env-configured ClickHouse target; the composed
	// applet document omits the `endpoint` frontmatter key.
	EndpointDefault = "default"
	// EndpointIntrospection is the in-process keelson `/query` endpoint
	// (introspect.LocalQueryEndpoint, ADR-0094 §SD6) — where bare
	// `keelson('…')` is the dialect and ad-hoc datasets resolve (ADR-0134).
	// A buffer authored there must reopen there, so the creator stamps
	// `endpoint: "introspection"` into the frontmatter.
	EndpointIntrospection = "introspection"
)

// AppletCreate is the flat wire form of the applet creator's launch
// arguments: the buffer to author and the endpoint it was authored against.
// The slug/title/icon are entered in the creator window, not carried here.
type AppletCreate struct {
	_ struct{} `kind:"appletCreate"`

	// FactId is the per-row event id (zero from producers; the launch row's
	// durable id is minted by the facts store on persist).
	FactId uint64 `lw:",id"`

	// NaturalKey is the entity natural key; these bus DTOs carry no separate
	// key, so it stays the nil default (the facts SetId is 2-arg).
	NaturalKey []byte `lw:",naturalKey"`

	// At is the event timestamp. time.Time matches the facts SetTimestamp
	// signature directly; the leeway wire truncates to u32 seconds, while the
	// bus preserves full nanos.
	At time.Time `lw:",ts"`

	// Sql is the buffer the creator seeds its editor with and composes into
	// the applet document's sql fence. Empty is valid — a plainly-opened
	// creator window starts with an empty editor.
	Sql string `lw:"appletCreateSql,textArray"`

	// Endpoint names the query target the buffer was authored against. Empty
	// or EndpointDefault omits the frontmatter key; EndpointIntrospection
	// pins the in-process endpoint the buffer's bare `keelson('…')` needs.
	Endpoint string `lw:"appletCreateEndpoint,symbol"`
}
