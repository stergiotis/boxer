package appletstore

import (
	"regexp"
	"slices"
	"strings"
	"sync"

	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/analysis"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/persist"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// AppId is the synthetic identity the store service runs under. It is never
// a registered app; its SubjectAlias namespaces the persisted documents, so
// changing it would orphan every stored applet.
const AppId app.AppIdT = "runtime.appletstore"

const (
	// keyPrefix + slug is one stored document. Persist keys must be single
	// NATS subject tokens (no dots), so the separator is an underscore and
	// slugs keep their dash charset.
	keyPrefix = "applet_"
	// indexKey enumerates the stored slugs, newline-joined: StorageI has no
	// listing, so the index is kept beside the documents (ADR-0132 Update
	// "O4" D1). Slugs cannot contain newlines ([SlugPattern]), so the join
	// is unambiguous.
	indexKey = "index"
)

// SlugPattern is the shape of an applet slug, stored or committed. The slug
// becomes the basename of a minted app id and a persist key, so it is kept
// to lowercase alphanumerics and dashes.
var SlugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// Definition is an applet document as the store gates and persists it: the
// fields the gate reads, and the document it keeps. The minter fills it from
// its parse; the store sets Doc to the bytes it persists.
type Definition struct {
	// Slug is the applet's durable name; it keys the stored document.
	Slug string
	// AppId is the id the applet's manifest registers under. The collision
	// rule (O4-D3) reads the registry at it.
	AppId app.AppIdT
	// Class is the ADR-0132 §SD5 security class of the buffer.
	Class analysis.QuerySecurityClassE
	// Doc is the document, persisted verbatim.
	Doc []byte
}

// MinterI is the slot an applet host fills when it starts the store: the
// parser the committed books go through, which the gate runs on every
// submitted and every stored document, and the registration of what the
// gate admits. Which documents reach Mint and Replace is the store's
// decision; what an applet is, is the minter's.
type MinterI interface {
	// Parse reads doc as the applet document named slug and classifies its
	// buffer. applet is false, with a nil error, for a document that
	// carries no sql fence.
	Parse(slug string, doc []byte) (def Definition, applet bool, err error)
	// Mint registers def's manifest in reg. The store calls it once per
	// stored slug per boot, after the collision rule.
	Mint(reg *app.Registry, def Definition) (err error)
	// Replace swaps the definition a slug Mint registered opens with
	// (O4-D4); the manifest stays as registered until the next boot.
	Replace(def Definition) (err error)
}

// Service is the runtime applet store (ADR-0132 Update "O4"): it serves
// [SubjectSave], validating each submitted document with the minter's parser,
// persisting it through the runtime persist facility and minting it live.
// It is the moderation gate between an authoring app and the launcher.
type Service struct {
	log       zerolog.Logger
	reg       *app.Registry
	minter    MinterI
	storage   app.StorageI
	busClient *inprocbus.Client
	unsub     func()

	mu     sync.Mutex
	stored map[string]struct{} // slugs this store minted this boot
}

// Start loads the stored applet corpus, mints it into reg through minter,
// and subscribes the save endpoint. Call it once at startup, after the
// committed books are minted into reg: the collision rule ("curation
// outranks runtime state", O4-D3) reads the registry as the source of truth.
func Start(reg *app.Registry, bus *inprocbus.Inst, minter MinterI, logger zerolog.Logger) (svc *Service, err error) {
	if reg == nil || bus == nil || minter == nil {
		err = eb.Build().Bool("reg", reg != nil).Bool("bus", bus != nil).Bool("minter", minter != nil).
			Errorf("appletstore: Start needs a registry, a bus and a minter")
		return
	}
	busClient := bus.NewClient(AppId, []app.SubjectFilter{
		{Pattern: SubjectSave, Direction: app.CapDirectionBoth,
			Reason: "applet store serves save requests (ADR-0132 O4)"},
		{Pattern: inprocbus.InboxPrefix + ">", Direction: app.CapDirectionPub,
			Reason: "publish save replies to caller inboxes"},
		{Pattern: persist.SubjectPrefix + AppId.SubjectAlias() + ".>", Direction: app.CapDirectionPub,
			Reason: "persist stored applet documents"},
	})
	storage, err := persist.NewClient(busClient, AppId)
	if err != nil {
		err = eh.Errorf("appletstore: Start: persist client: %w", err)
		return
	}
	svc = &Service{
		log:       logger,
		reg:       reg,
		minter:    minter,
		storage:   storage,
		busClient: busClient,
		stored:    make(map[string]struct{}, 4),
	}
	svc.loadStored()
	unsub, subErr := busClient.Subscribe(SubjectSave, svc.handleSave)
	if subErr != nil {
		err = eh.Errorf("appletstore: Start: subscribe: %w", subErr)
		svc = nil
		return
	}
	svc.unsub = unsub
	return
}

// Stop unsubscribes the save endpoint. Idempotent, nil-safe.
func (inst *Service) Stop() {
	if inst != nil && inst.unsub != nil {
		inst.unsub()
		inst.unsub = nil
	}
}

func (inst *Service) isStored(slug string) (stored bool) {
	inst.mu.Lock()
	_, stored = inst.stored[slug]
	inst.mu.Unlock()
	return
}

// mint registers def through the minter and records the slug as this
// store's, so a later save of it is an overwrite.
func (inst *Service) mint(def Definition) (err error) {
	if err = inst.minter.Mint(inst.reg, def); err != nil {
		return
	}
	inst.mu.Lock()
	inst.stored[def.Slug] = struct{}{}
	inst.mu.Unlock()
	return
}

// loadStored parses and mints every stored document at boot (O4-D3):
// best-effort per document. A stored document that no longer parses, or
// whose slug a committed applet took meanwhile, is skipped with a warning.
// Boot never blocks on runtime state.
func (inst *Service) loadStored() {
	raw, found, err := inst.storage.Get(indexKey)
	if err != nil || !found || len(raw) == 0 {
		if err != nil {
			inst.log.Warn().Err(err).Msg("appletstore: store index unreadable; stored applets skipped")
		}
		return
	}
	for slug := range strings.SplitSeq(strings.TrimSpace(string(raw)), "\n") {
		if slug == "" {
			continue
		}
		doc, docFound, gerr := inst.storage.Get(keyPrefix + slug)
		if gerr != nil || !docFound {
			inst.log.Warn().Err(gerr).Str("slug", slug).Msg("appletstore: indexed applet has no stored document; skipped")
			continue
		}
		def, applet, perr := inst.minter.Parse(slug, doc)
		if perr != nil || !applet {
			inst.log.Warn().Err(perr).Str("slug", slug).Msg("appletstore: stored applet no longer parses; skipped")
			continue
		}
		def.Doc = doc
		if _, taken := inst.reg.LookupManifest(def.AppId); taken {
			inst.log.Warn().Str("slug", def.Slug).Msg("appletstore: stored applet collides with a committed one; committed wins")
			continue
		}
		if merr := inst.mint(def); merr != nil {
			inst.log.Warn().Err(merr).Str("slug", def.Slug).Msg("appletstore: stored applet failed to mint; skipped")
		}
	}
}

// handleSave is the SubjectSave endpoint: validate exactly as the committed
// corpus gate does, persist, mint (first save) or swap the live definition
// (overwrite). Every failure is a structured reply: a bad document is
// refused, never half-saved.
func (inst *Service) handleSave(msg *app.Msg) {
	if msg.Reply == "" {
		inst.log.Warn().Msg("appletstore: save request without reply inbox")
		return
	}
	reply := func(rep SaveReply) {
		b, eerr := EncodeSaveReply(rep)
		if eerr != nil {
			inst.log.Warn().Err(eerr).Msg("appletstore: encode save reply")
			return
		}
		if perr := inst.busClient.Publish(msg.Reply, b); perr != nil {
			inst.log.Warn().Err(perr).Msg("appletstore: publish save reply")
		}
	}
	req, err := DecodeSaveRequest(msg.Payload)
	if err != nil {
		reply(SaveReply{Error: err.Error()})
		return
	}
	if !SlugPattern.MatchString(req.Slug) {
		reply(SaveReply{Error: "slug must match " + SlugPattern.String()})
		return
	}
	def, applet, err := inst.minter.Parse(req.Slug, req.Doc)
	if err != nil {
		reply(SaveReply{Error: err.Error()})
		return
	}
	if !applet {
		reply(SaveReply{Error: "document carries no sql fence — not an applet"})
		return
	}
	def.Doc = req.Doc
	_, registered := inst.reg.LookupManifest(def.AppId)
	overwrite := inst.isStored(def.Slug)
	if registered && !overwrite {
		reply(SaveReply{Error: "slug " + def.Slug + " collides with a committed applet"})
		return
	}
	if err = inst.storage.Set(keyPrefix+def.Slug, req.Doc); err != nil {
		reply(SaveReply{Error: "persist document: " + err.Error()})
		return
	}
	if err = inst.updateIndex(def.Slug); err != nil {
		reply(SaveReply{Error: "persist index: " + err.Error()})
		return
	}
	if overwrite {
		err = inst.minter.Replace(def)
	} else {
		err = inst.mint(def)
	}
	if err != nil {
		reply(SaveReply{Error: "mint: " + err.Error()})
		return
	}
	inst.log.Info().Str("slug", def.Slug).Str("class", def.Class.String()).
		Str("sender", string(msg.Sender)).Bool("overwrite", overwrite).
		Msg("appletstore: applet saved")
	reply(SaveReply{OK: true, Class: def.Class.String()})
}

// updateIndex appends slug to the stored-slug index if absent.
func (inst *Service) updateIndex(slug string) (err error) {
	raw, _, err := inst.storage.Get(indexKey)
	if err != nil {
		return
	}
	slugs := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if slices.Contains(slugs, slug) {
		return
	}
	slugs = append(slugs, slug)
	joined := strings.TrimSpace(strings.Join(slugs, "\n"))
	err = inst.storage.Set(indexKey, []byte(joined))
	return
}

// AdmitUnreviewed is the gate for an applet document that neither a
// committed book nor this store stands behind, such as one a browser tab
// fetches from its page's origin (ADR-0299 §SD3). With no review behind it,
// only a buffer §SD5 classifies read is admitted, and a slug a registered
// applet already holds is refused, as in O4-D3.
func AdmitUnreviewed(reg *app.Registry, def Definition) (err error) {
	if def.Class != analysis.QuerySecurityRead {
		err = eb.Build().Str("slug", def.Slug).Str("class", def.Class.String()).
			Errorf("appletstore: a document no review stands behind must be read-class")
		return
	}
	if _, taken := reg.LookupManifest(def.AppId); taken {
		err = eb.Build().Str("slug", def.Slug).Errorf("appletstore: the slug is a committed applet's; rename the document")
	}
	return
}
