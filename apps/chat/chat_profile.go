package chat

import (
	"encoding/hex"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/rs/zerolog/log"
	"lukechampine.com/blake3"

	"github.com/stergiotis/boxer/public/config/env"
	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun/projection"
	"github.com/stergiotis/boxer/public/keelson/runtime/harness"
	"github.com/stergiotis/boxer/public/keelson/runtime/headstore"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// The harness profile (ADR-0310 §SD7 and §SD11): what the harness made
// data — the tool registry, the prompt book, the seams' elements by name,
// the route, the round range, the compaction constants and the host's
// other limits — as one value a run can be pinned to and compared by.
// Exactly one profile is registered: the compile-date constants with the
// limits the BOXER_CHAT_* variables resolve to; the selector variable is
// wired and has one choice. The id and fingerprint go on the run row and
// on every call row the run makes (ADR-0310 §SD7), and
// keelson('harness_profiles') lists the values.
//
// A limit is the profile's and the chat reads it from the profile, so a
// changed limit changes the fingerprint. A starting value — the rounds,
// the Pixels level, the operation tools, what a new window offers — is
// only where a conversation's own setting begins: it stays a BOXER_CHAT_*
// seed, outside the profile, and the setting itself is fixed on the run
// row (journalSettings). A profile carries no authority (no grant, ceiling
// or consent class): that stays compiled in the dispatcher.

// ProfileT is one harness profile.
type ProfileT struct {
	// Id names the profile; it is not part of the fingerprint, so two
	// names for one set of values fingerprint alike.
	Id string
	// PromptBook is the prompt book's id; Tools the registry's tool names
	// in the order the model sees them.
	PromptBook string
	Tools      []string
	// Seams are the driver's seams by the names of their elements, in the
	// order they run (chat_seams.go).
	Inject    []string
	Configure []string
	Recover   []string
	ToolMode  string
	Stop      []string
	// Route is the model route's id; empty is the host's one route under
	// its default capabilities.
	Route string
	// RoundsMin and RoundsMax are the range the person picks a turn's
	// rounds from.
	RoundsMin int
	RoundsMax int
	// PixelsMax is the highest Pixels level a chat may use and
	// PixelsLocalRequired makes Only to a local model the host's rule.
	PixelsMax           string
	PixelsLocalRequired bool
	// ImageBudgetBytes, ImageBudgetCount and ImageMaxPixels bound the
	// artefact's screenshots.
	ImageBudgetBytes int64
	ImageBudgetCount int64
	ImageMaxPixels   int64
	// Compact is the compaction constants (ADR-0316); its trigger is
	// CompactTriggerPercent of the route's context, and the thrash check
	// runs against CompactContextFloor, the smallest context the profile
	// is meant for.
	Compact               projection.Constants
	CompactTriggerPercent int
	CompactContextFloor   int64
	// AnchorCadence is when the run's chain heads are shipped to the
	// anchor store (ADR-0314 §SD3). It shapes the record, not what the
	// model reads, so it is outside the fingerprint.
	AnchorCadence headstore.Cadence
}

// Validate checks the profile against what this build registers: every
// tool and seam element named exists in its declared order, the slot is
// filled, the prompt book is the chat's, and the round range is one.
func (inst ProfileT) Validate() (err error) {
	if inst.Id == "" {
		return eb.Build().Errorf("a profile has no id")
	}
	if inst.PromptBook != promptBookId {
		return eb.Build().Str("book", inst.PromptBook).Errorf("the prompt book is not registered")
	}
	registered := make([]string, 0, len(coordinatorTools))
	for i := range coordinatorTools {
		registered = append(registered, coordinatorTools[i].name)
	}
	if strings.Join(inst.Tools, ",") != strings.Join(registered, ",") {
		return eb.Build().Str("profile", strings.Join(inst.Tools, ",")).Str("registry", strings.Join(registered, ",")).Errorf("the tools are not the registry's")
	}
	s := turnSeamsNow()
	for _, seam := range []struct {
		name  string
		named []string
		built []string
	}{
		{"inject", inst.Inject, s.inject.Names()},
		{"configure", inst.Configure, s.configure.Names()},
		{"recover", inst.Recover, s.recover.Names()},
		{"stop", inst.Stop, s.stop.Names()},
	} {
		if strings.Join(seam.named, ",") != strings.Join(seam.built, ",") {
			return eb.Build().Str("seam", seam.name).Str("profile", strings.Join(seam.named, ",")).Str("built", strings.Join(seam.built, ",")).Errorf("the seam's elements are not the build's")
		}
	}
	if mode, _ := s.toolMode.Get(); inst.ToolMode != mode {
		return eb.Build().Str("profile", inst.ToolMode).Str("built", mode).Errorf("the tool mode is not the build's")
	}
	if inst.RoundsMin < 2 || inst.RoundsMax < inst.RoundsMin {
		return eb.Build().Int("min", inst.RoundsMin).Int("max", inst.RoundsMax).Errorf("the rounds are not a range")
	}
	switch inst.PixelsMax {
	case "none", "ask", "ask-once", "captures":
	default:
		return eb.Build().Str("pixels", inst.PixelsMax).Errorf("no such Pixels level")
	}
	if inst.CompactTriggerPercent < 1 || inst.CompactTriggerPercent > 100 || inst.CompactContextFloor <= 0 {
		return eb.Build().Int("percent", inst.CompactTriggerPercent).Int64("floor", inst.CompactContextFloor).Errorf("the compaction trigger is not a share of a context")
	}
	c := inst.Compact
	c.TriggerTokens = inst.CompactContextFloor * int64(inst.CompactTriggerPercent) / 100
	// The head at its largest: every section on.
	if err = c.Validate(int64(len(systemPromptOf(true, true, true))) / 4); err != nil {
		return eb.Build().Errorf("compaction: %w", err)
	}
	return
}

// Fingerprint is the digest of the profile's values and of the prompt
// book and tools it names (promptFingerprint); the id is not in it.
func (inst ProfileT) Fingerprint() string {
	h := blake3.New(32, nil)
	w := func(parts ...string) {
		for _, p := range parts {
			_, _ = h.Write([]byte(p))
			_, _ = h.Write([]byte{0})
		}
	}
	w("prompts", promptFingerprint(), inst.PromptBook)
	w(append([]string{"tools"}, inst.Tools...)...)
	w(append([]string{"inject"}, inst.Inject...)...)
	w(append([]string{"configure"}, inst.Configure...)...)
	w(append([]string{"recover"}, inst.Recover...)...)
	w("toolmode", inst.ToolMode)
	w(append([]string{"stop"}, inst.Stop...)...)
	w("route", inst.Route)
	w("rounds", strconv.Itoa(inst.RoundsMin), strconv.Itoa(inst.RoundsMax))
	w("pixels", inst.PixelsMax, strconv.FormatBool(inst.PixelsLocalRequired))
	w("images", strconv.FormatInt(inst.ImageBudgetBytes, 10), strconv.FormatInt(inst.ImageBudgetCount, 10), strconv.FormatInt(inst.ImageMaxPixels, 10))
	c := inst.Compact
	w("compact", strconv.Itoa(inst.CompactTriggerPercent), strconv.FormatInt(inst.CompactContextFloor, 10), strconv.Itoa(c.KeepTurns), strconv.Itoa(c.PinBytes), strconv.Itoa(c.ElideMinBytes),
		strconv.FormatInt(c.SummaryTokens, 10), strconv.FormatInt(c.TailTokens, 10), strconv.FormatInt(c.RoundMarginTokens, 10))
	return hex.EncodeToString(h.Sum(nil))
}

// defaultProfileId names the one registered profile.
const defaultProfileId = "default"

// defaultProfile is the compile-date constants as one value, with the
// limits the BOXER_CHAT_* variables resolve to.
func defaultProfile() (p ProfileT) {
	s := turnSeamsNow()
	mode, _ := s.toolMode.Get()
	p = ProfileT{Id: defaultProfileId, PromptBook: promptBookId,
		Inject: s.inject.Names(), Configure: s.configure.Names(), Recover: s.recover.Names(), ToolMode: mode, Stop: s.stop.Names(),
		PixelsMax: PixelsMaxEnv.Get(), PixelsLocalRequired: PixelsLocalRequiredEnv.Get(),
		ImageBudgetBytes: ImageBudgetBytes.Get(), ImageBudgetCount: ImageBudgetCount.Get(), ImageMaxPixels: ImageMaxPixels.Get(),
		Compact:               projection.Constants{KeepTurns: 2, PinBytes: 4096, ElideMinBytes: 512, SummaryTokens: 1024, TailTokens: 2048, RoundMarginTokens: 1024},
		CompactTriggerPercent: 70, CompactContextFloor: 16384, AnchorCadence: headstore.DefaultCadence()}
	// One round would be the last, with no tools at all.
	p.RoundsMin = max(int(RoundsMinEnv.Get()), 2)
	p.RoundsMax = max(int(RoundsMaxEnv.Get()), p.RoundsMin)
	for i := range coordinatorTools {
		p.Tools = append(p.Tools, coordinatorTools[i].name)
	}
	return
}

// ProfileEnv selects the harness profile a chat runs under (ADR-0009: a
// variable selects, it does not toggle); one profile is registered.
var ProfileEnv = env.NewCategorialString(env.Spec{
	Name:        "BOXER_CHAT_PROFILE",
	Default:     defaultProfileId,
	Description: "the harness profile a chat runs under — the tool set, prompt book, seam elements, route and constants as one named, fingerprinted value (ADR-0310); default is the only profile registered",
	Category:    env.CategoryE("boxer-chat"),
}, []string{defaultProfileId})

// profiles is the registry: built once, every profile validated; a
// profile that does not validate is a programming error.
var profiles = sync.OnceValue(func() (r *harness.Registry[ProfileT]) {
	r = &harness.Registry[ProfileT]{}
	for _, p := range []ProfileT{defaultProfile()} {
		if err := p.Validate(); err != nil {
			panic("chat: the harness profile " + p.Id + " does not validate: " + err.Error())
		}
		if err := r.Add(p.Id, p); err != nil {
			panic("chat: " + err.Error())
		}
		harness.PublishProfile(harness.ProfileRow{App: string(ManifestId), Id: p.Id, Fingerprint: p.Fingerprint(), Fields: p.fields()})
	}
	return
})

// profileRef is what the run's profile is recorded as on a call's row: the
// id with the fingerprint's first twelve hex digits, and the route the
// profile names.
func (inst *coordinator) profileRef() (profile string, route string) {
	inst.mu.Lock()
	p, fp := inst.profile, inst.fingerprint
	inst.mu.Unlock()
	if p.Id == "" {
		return "", ""
	}
	return p.Id + "@" + clip(fp, 12), p.Route
}

// fields is the profile as the harness_profiles table shows it: every
// constant by name.
func (inst ProfileT) fields() map[string]string {
	c := inst.Compact
	return map[string]string{
		"prompt_book": inst.PromptBook, "tools": strings.Join(inst.Tools, ","),
		"inject": strings.Join(inst.Inject, ","), "configure": strings.Join(inst.Configure, ","), "recover": strings.Join(inst.Recover, ","),
		"tool_mode": inst.ToolMode, "stop": strings.Join(inst.Stop, ","), "route": inst.Route,
		"rounds_min": strconv.Itoa(inst.RoundsMin), "rounds_max": strconv.Itoa(inst.RoundsMax),
		"pixels_max": inst.PixelsMax, "pixels_local_required": strconv.FormatBool(inst.PixelsLocalRequired),
		"image_budget_bytes": strconv.FormatInt(inst.ImageBudgetBytes, 10), "image_budget_count": strconv.FormatInt(inst.ImageBudgetCount, 10), "image_max_pixels": strconv.FormatInt(inst.ImageMaxPixels, 10),
		"compact_trigger_percent": strconv.Itoa(inst.CompactTriggerPercent), "compact_context_floor": strconv.FormatInt(inst.CompactContextFloor, 10),
		"compact_keep_turns": strconv.Itoa(c.KeepTurns), "compact_pin_bytes": strconv.Itoa(c.PinBytes), "compact_elide_min_bytes": strconv.Itoa(c.ElideMinBytes),
		"compact_summary_tokens": strconv.FormatInt(c.SummaryTokens, 10), "compact_tail_tokens": strconv.FormatInt(c.TailTokens, 10), "compact_round_margin_tokens": strconv.FormatInt(c.RoundMarginTokens, 10),
		"anchor_cadence": inst.AnchorCadence.String(),
	}
}

// profileForTest, when set, is the profile profileNow returns
// (setProfileForTest).
var profileForTest atomic.Pointer[ProfileT]

// profileNow is the profile the selector names, the default when the name
// is not registered.
func profileNow() (p ProfileT) {
	if t := profileForTest.Load(); t != nil {
		return *t
	}
	p, ok := profiles().Lookup(ProfileEnv.Get())
	if !ok {
		p, _ = profiles().Lookup(defaultProfileId)
	}
	return
}

// logProfile logs the profile a chat runs under and its fingerprint, once
// per process, on the first turn; the call rows carry the same reference.
var logProfile = sync.OnceFunc(func() {
	p := profileNow()
	log.Debug().Str("profile", p.Id).Str("fingerprint", p.Fingerprint()).Str("prompts", promptFingerprint()).Msg("chat: harness profile")
})
