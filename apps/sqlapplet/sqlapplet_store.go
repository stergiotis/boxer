package sqlapplet

import (
	"strings"
	"sync"

	"github.com/rs/zerolog"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appletstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/persist"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// StoreAppId is the synthetic identity the applet store service registers
// under; its SubjectAlias namespaces the persisted documents.
const StoreAppId app.AppIdT = "runtime.appletstore"

const (
	// storeKeyPrefix + slug is one stored document. Persist keys must be
	// single NATS subject tokens (no dots), so the separator is an
	// underscore and slugs keep their dash charset.
	storeKeyPrefix = "applet_"
	// storeIndexKey enumerates the stored slugs, newline-joined —
	// StorageI has no listing, so the index is maintained beside the
	// documents (ADR-0132 Update "O4" D1). Slugs cannot contain newlines
	// (slugPattern), so the join is unambiguous.
	storeIndexKey = "index"
)

// StoreService is the runtime applet store (ADR-0132 Update "O4"): it
// serves `applet.store.save`, validating each submitted document with the
// same parser the committed books go through, persisting it through the
// runtime persist facility, and minting its manifest live. It is the
// moderation gate between an authoring app and the launcher.
type StoreService struct {
	log       zerolog.Logger
	reg       *app.Registry
	storage   app.StorageI
	busClient *inprocbus.Client
	unsub     func()

	mu   sync.Mutex
	defs map[string]*AppletDef // stored applets only; resolved at Open time
}

// StartStore loads the stored applet corpus, mints it into the default app
// registry, and subscribes the save endpoint. Call it once at startup,
// after MintManifests — committed books must already be minted so the
// collision rule ("curation outranks runtime state", O4-D3) can read the
// registry as the source of truth.
func StartStore(bus *inprocbus.Inst, logger zerolog.Logger) (svc *StoreService, err error) {
	return startStore(app.DefaultRegistry, bus, logger)
}

// startStore is StartStore against an explicit registry (tests).
func startStore(reg *app.Registry, bus *inprocbus.Inst, logger zerolog.Logger) (svc *StoreService, err error) {
	if bus == nil {
		err = eh.Errorf("sqlapplet: StartStore: bus is nil")
		return
	}
	busClient := bus.NewClient(StoreAppId, []app.SubjectFilter{
		{Pattern: appletstore.SubjectSave, Direction: app.CapDirectionBoth,
			Reason: "applet store serves save requests (ADR-0132 O4)"},
		{Pattern: inprocbus.InboxPrefix + ">", Direction: app.CapDirectionPub,
			Reason: "publish save replies to caller inboxes"},
		{Pattern: persist.SubjectPrefix + StoreAppId.SubjectAlias() + ".>", Direction: app.CapDirectionPub,
			Reason: "persist stored applet documents"},
	})
	storage, err := persist.NewClient(busClient, StoreAppId)
	if err != nil {
		err = eh.Errorf("sqlapplet: StartStore: persist client: %w", err)
		return
	}
	svc = &StoreService{
		log:       logger,
		reg:       reg,
		storage:   storage,
		busClient: busClient,
		defs:      make(map[string]*AppletDef, 4),
	}
	svc.loadStored()
	unsub, subErr := busClient.Subscribe(appletstore.SubjectSave, svc.handleSave)
	if subErr != nil {
		err = eh.Errorf("sqlapplet: StartStore: subscribe: %w", subErr)
		svc = nil
		return
	}
	svc.unsub = unsub
	return
}

// Stop unsubscribes the save endpoint. Idempotent, nil-safe.
func (inst *StoreService) Stop() {
	if inst != nil && inst.unsub != nil {
		inst.unsub()
		inst.unsub = nil
	}
}

// lookup resolves a stored definition at Open time (O4-D4: an overwrite
// replaces the definition future opens see).
func (inst *StoreService) lookup(slug string) (def *AppletDef) {
	inst.mu.Lock()
	def = inst.defs[slug]
	inst.mu.Unlock()
	return
}

// loadStored parses and mints every stored document at boot (O4-D3):
// best-effort per document — a stored doc that no longer parses, or whose
// slug a committed applet took meanwhile, is skipped with a warning. Boot
// never blocks on runtime state.
func (inst *StoreService) loadStored() {
	raw, found, err := inst.storage.Get(storeIndexKey)
	if err != nil || !found || len(raw) == 0 {
		if err != nil {
			inst.log.Warn().Err(err).Msg("sqlapplet: store index unreadable; stored applets skipped")
		}
		return
	}
	for _, slug := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if slug == "" {
			continue
		}
		doc, docFound, gerr := inst.storage.Get(storeKeyPrefix + slug)
		if gerr != nil || !docFound {
			inst.log.Warn().Err(gerr).Str("slug", slug).Msg("sqlapplet: indexed applet has no stored document; skipped")
			continue
		}
		def, perr := ParseDocSource("store", slug+".md", doc)
		if perr != nil || def == nil {
			inst.log.Warn().Err(perr).Str("slug", slug).Msg("sqlapplet: stored applet no longer parses; skipped")
			continue
		}
		if _, taken := inst.reg.LookupManifest(app.AppIdT(appletIdPrefix + def.Slug)); taken {
			inst.log.Warn().Str("slug", def.Slug).Msg("sqlapplet: stored applet collides with a committed one; committed wins")
			continue
		}
		if merr := inst.mint(def); merr != nil {
			inst.log.Warn().Err(merr).Str("slug", def.Slug).Msg("sqlapplet: stored applet failed to mint; skipped")
		}
	}
}

// mint registers the manifest for a stored definition; the factory
// resolves the definition through the service at every Open, so an
// overwrite (O4-D4) reaches future windows without re-registration.
func (inst *StoreService) mint(def *AppletDef) (err error) {
	m := manifestFor(def, nil)
	if err = m.Validate(); err != nil {
		return
	}
	slug := def.Slug
	if err = inst.reg.RegisterFactory(m, func() (a app.AppI, ctorErr error) {
		live := inst.lookup(slug)
		if live == nil {
			ctorErr = eh.Errorf("sqlapplet: stored applet %q has no live definition", slug)
			return
		}
		a = &appletApp{def: live, m: m}
		return
	}); err != nil {
		return
	}
	inst.mu.Lock()
	inst.defs[slug] = def
	inst.mu.Unlock()
	return
}

// handleSave is the SubjectSave endpoint: validate exactly as the
// committed corpus gate does, persist, mint (first save) or swap the live
// definition (overwrite). Every failure is a structured reply — a bad
// document is refused, never half-saved.
func (inst *StoreService) handleSave(msg *app.Msg) {
	if msg.Reply == "" {
		inst.log.Warn().Msg("sqlapplet: save request without reply inbox")
		return
	}
	reply := func(rep appletstore.SaveReply) {
		b, eerr := appletstore.EncodeSaveReply(rep)
		if eerr != nil {
			inst.log.Warn().Err(eerr).Msg("sqlapplet: encode save reply")
			return
		}
		if perr := inst.busClient.Publish(msg.Reply, b); perr != nil {
			inst.log.Warn().Err(perr).Msg("sqlapplet: publish save reply")
		}
	}
	req, err := appletstore.DecodeSaveRequest(msg.Payload)
	if err != nil {
		reply(appletstore.SaveReply{Error: err.Error()})
		return
	}
	if !slugPattern.MatchString(req.Slug) {
		reply(appletstore.SaveReply{Error: "slug must match " + slugPattern.String()})
		return
	}
	def, err := ParseDocSource("store", req.Slug+".md", req.Doc)
	if err != nil {
		reply(appletstore.SaveReply{Error: err.Error()})
		return
	}
	if def == nil {
		reply(appletstore.SaveReply{Error: "document carries no sql fence — not an applet"})
		return
	}
	_, registered := inst.reg.LookupManifest(app.AppIdT(appletIdPrefix + def.Slug))
	overwrite := inst.lookup(def.Slug) != nil
	if registered && !overwrite {
		reply(appletstore.SaveReply{Error: "slug " + def.Slug + " collides with a committed applet"})
		return
	}
	if err = inst.storage.Set(storeKeyPrefix+def.Slug, req.Doc); err != nil {
		reply(appletstore.SaveReply{Error: "persist document: " + err.Error()})
		return
	}
	if err = inst.updateIndex(def.Slug); err != nil {
		reply(appletstore.SaveReply{Error: "persist index: " + err.Error()})
		return
	}
	if overwrite {
		// O4-D4: the live definition swaps; the minted manifest's display
		// metadata stays as-registered until the next boot.
		inst.mu.Lock()
		inst.defs[def.Slug] = def
		inst.mu.Unlock()
	} else if err = inst.mint(def); err != nil {
		reply(appletstore.SaveReply{Error: "mint: " + err.Error()})
		return
	}
	inst.log.Info().Str("slug", def.Slug).Str("class", def.Class.String()).
		Str("sender", string(msg.Sender)).Bool("overwrite", overwrite).
		Msg("sqlapplet: applet saved")
	reply(appletstore.SaveReply{OK: true, Class: def.Class.String()})
}

// updateIndex appends slug to the stored-slug index if absent.
func (inst *StoreService) updateIndex(slug string) (err error) {
	raw, _, err := inst.storage.Get(storeIndexKey)
	if err != nil {
		return
	}
	slugs := strings.Split(strings.TrimSpace(string(raw)), "\n")
	for _, s := range slugs {
		if s == slug {
			return
		}
	}
	slugs = append(slugs, slug)
	joined := strings.TrimSpace(strings.Join(slugs, "\n"))
	err = inst.storage.Set(storeIndexKey, []byte(joined))
	return
}
