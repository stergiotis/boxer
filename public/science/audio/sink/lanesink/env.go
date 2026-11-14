package lanesink

import "github.com/stergiotis/boxer/public/config/env"

// Target is how much audio a [Device] keeps its remote device ahead by when
// its options name none (ADR-0372, proposed, §SD5). It is the lane's latency
// and a floor: the device keeps two and a half report intervals and 30 ms
// ahead at least — 80 ms with a page that reports every 20 ms, whatever is
// set here — and raises the figure further, to four seconds at most, where
// reports come slowly.
var Target = env.NewDuration(env.Spec{
	Name:        "BOXER_AUDIO_LANE_TARGET",
	Description: "audio lane: how much audio a remote audio device is kept ahead by (ADR-0372 §SD5); never less than 2.5 report intervals and 30 ms (80 ms against a browser page), and grown to 4 s at most where the device reports slowly",
	Category:    env.CategorySystem,
	Default:     "150ms",
})

// Trace asks a host to log every played report of its lane's device, which
// is how the lane is measured (doc/trials/audio-lane-playout).
var Trace = env.NewBool(env.Spec{
	Name:        "BOXER_AUDIO_LANE_TRACE",
	Description: "audio lane: log every played report of the remote audio device, for measuring the lane",
	Category:    env.CategoryDev,
})
