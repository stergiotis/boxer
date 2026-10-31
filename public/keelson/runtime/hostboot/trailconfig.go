package hostboot

import (
	"context"
	"strconv"
	"time"

	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore/chstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/headstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/llm"
	"github.com/stergiotis/boxer/public/keelson/runtime/runinfo"
	"github.com/stergiotis/boxer/public/keelson/runtime/trail"
	"github.com/stergiotis/boxer/public/storage/recordstore/evidence/anchor"
)

// trailSettings are the audit-relevant settings a host's trail starts
// under (ADR-0296, the Update of 2026-10-31): each a short code, none
// personal, the anchor store's URL never.
type trailSettings struct {
	// Required is BOXER_TRAIL_REQUIRED.
	Required bool
	// Retain is BOXER_LLM_RETAIN's ceiling: off, ring or durable.
	Retain string
	// LogFacts is BOXER_LOG_FACTS read as loghost reads it.
	LogFacts bool
	// AnchorURL says BOXER_ANCHOR_STORE_URL is set; AnchorStore is the
	// store the host anchors in: org, or memory when the URL is unset or
	// the store could not be reached.
	AnchorURL   bool
	AnchorStore string
	// FlushInterval is the recorder's own flush interval.
	FlushInterval time.Duration
	// VcsRevision and VcsModified are the build's, when it carries them.
	VcsRevision string
	VcsModified bool
}

// trailSettingsOf reads the settings from the env registry (ADR-0009), the
// anchor store the host built and the run's build info.
func trailSettingsOf(anchors anchor.StoreI, ri *runinfo.Inst) (s trailSettings) {
	s = trailSettings{
		Required:      trail.RequiredEnv.Get(),
		Retain:        llm.Retain.Get(),
		LogFacts:      chstore.LogFactsOn(),
		AnchorURL:     headstore.OrgStoreURL.Get() != "",
		AnchorStore:   "org",
		FlushInterval: trailFlushInterval,
	}
	if _, mem := anchors.(*anchor.MemStore); mem || anchors == nil {
		s.AnchorStore = "memory"
	}
	if ri != nil {
		s.VcsRevision, s.VcsModified = ri.VcsRevision, ri.VcsModified
	}
	return
}

// trailConfigRow is the trail's config event for s: the recorder's own
// domain, outcome ok, one attribute per setting.
func trailConfigRow(s trailSettings) (row trail.AuditEvent) {
	row = trail.AuditEvent{Domain: trail.TrailDomain, Action: trail.ActionAuditConfig, Outcome: trail.OutcomeOk, Retention: trail.RetentionTrail}
	add := func(k string, v string) {
		if v != "" {
			row.AttrKeys, row.AttrValues = append(row.AttrKeys, k), append(row.AttrValues, trail.Bounded(v))
		}
	}
	add("trail-required", strconv.FormatBool(s.Required))
	add("llm-retain", s.Retain)
	add("log-facts", strconv.FormatBool(s.LogFacts))
	anchorURL := "unset"
	if s.AnchorURL {
		anchorURL = "set"
	}
	add("anchor-url", anchorURL)
	add("anchor-store", s.AnchorStore)
	add("flush-interval", s.FlushInterval.String())
	if s.VcsRevision != "" {
		add("vcs-revision", s.VcsRevision)
		add("vcs-modified", strconv.FormatBool(s.VcsModified))
	}
	return
}

// recordTrailConfig writes the config event as the host's own row: the
// recorder's origin, established by the host. It is the baseline a reader
// of the trail's later rows reads them against. A trail that is not
// durable records nothing.
func recordTrailConfig(ctx context.Context, rec *trail.Recorder, s trailSettings, log zerolog.Logger) {
	if !rec.Durable() {
		return
	}
	if err := rec.Event(ctx, time.Now().UTC(), trail.Context{Origin: rec.OriginOf("", 0)}, trailConfigRow(s)); err != nil {
		log.Warn().Err(err).Msg("trail: write the config event")
	}
	rec.FlushSoon()
}
