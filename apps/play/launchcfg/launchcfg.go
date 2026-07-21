// Package launchcfg is play's launch config (ADR-0135 §SD7): the typed
// arguments another app can open a play window with over
// `windowhost.open`. The DTO follows the runtime codec grammar and the
// generated codec is the only wire form — callers encode with Marshal /
// buscodec.Encode, play decodes in Mount with the generated Unmarshal.
//
// Vocabulary (narrow, ADR-0135 cohort in the keelson vdd):
//
//   - [vdd.MembPlayLaunchSql] / [vdd.MembPlayLaunchBandsSql] — text,
//     the editor buffer and the Timeline bands buffer.
//   - [vdd.MembPlayLaunchAutoRun] / [vdd.MembPlayLaunchLive] — bool
//     flags.
//   - [vdd.MembPlayLaunchTab] — symbol, the initially focused tab id.
//
// Seeding priority in play's Mount: a window opened WITH a config
// applies it above both the BOXER_PLAY_* env overrides and the
// persisted session buffer; plainly-opened windows are unaffected.
package launchcfg

import "time"

// Kind is the vocabulary kind name of this config — the value play's
// manifest declares as LaunchKind and callers put in
// launchrequest.LaunchRequest.ConfigKind.
const Kind = "playLaunch"

// PlayLaunch is the flat wire form of play's launch arguments.
type PlayLaunch struct {
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

	// Sql is the initial editor buffer. Non-empty wins over the
	// BOXER_PLAY_SQL override and the persisted-session restore; empty
	// falls through to that existing chain.
	Sql string `lw:"playLaunchSql,textArray"`

	// AutoRun triggers a Run of the seeded buffer on mount. Applied
	// whenever the window carries a config (replacing the
	// BOXER_PLAY_AUTORUN tier), matching the "config beats env" rule.
	AutoRun bool `lw:"playLaunchAutoRun,bool"`

	// Live enables live re-run on the main lane (SetLiveMain).
	Live bool `lw:"playLaunchLive,bool"`

	// BandsSql seeds the Timeline panel's bands editor when non-empty;
	// empty leaves the persisted/env-seeded bands untouched.
	BandsSql string `lw:"playLaunchBandsSql,textArray"`

	// Tab selects the initially focused body tab by id when non-empty
	// (ActivateTab); an unknown id is a warning, not a mount error.
	Tab string `lw:"playLaunchTab,symbol"`
}
