package chat

// Exporting the artefact to the lading store (ADR-0342, proposed): the
// person's gesture writes the head revision's text and the screenshots it
// names as one snapshot of the conversation's mount, through the host's
// publish service (ADR-0341, proposed), which checks the label before any
// row is written. Confirming endorses the head for publishing when no
// endorsement covers it (ADR-0311 §SD3, law L15). Open in mdedit takes the
// same path when the artefact has screenshots, and hands mdedit the snapshot.

import (
	"context"
	"encoding/json/v2"
	"io/fs"
	"strconv"
	"time"

	"github.com/stergiotis/boxer/apps/mdedit/launchcfg"
	"github.com/stergiotis/boxer/public/fs/lading"
	"github.com/stergiotis/boxer/public/fs/lading/ladingadapter"
	"github.com/stergiotis/boxer/public/fs/lading/ladingschema"
	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stergiotis/boxer/public/identity/tagmint"
	"github.com/stergiotis/boxer/public/keelson/data/chclient"
	"github.com/stergiotis/boxer/public/keelson/data/storeexec"
	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	"github.com/stergiotis/boxer/public/keelson/runtime/ladingpublish"
	"github.com/stergiotis/boxer/public/keelson/runtime/windowhost"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// exportMountTag is the tag of the chat's artefact mounts (ADR-0198 §SD3):
// one mount per conversation, minted by the publish service at the first
// export.
const exportMountTag identifier.TagValue = 2178323

// The claim keeps another claimant in the link set off the tag value.
var _ = tagmint.MustClaim("chatArtefactMount", exportMountTag, 1<<24)

const (
	// exportTtlDays is how long an export is kept.
	exportTtlDays = 30
	// exportDocPath is the text's path in the snapshot.
	exportDocPath = "artefact.md"
	// exportEntryName and exportEntryForm mark the journal entry an export
	// leaves: extension-private, skipped by the projection.
	exportEntryName = "artefact"
	exportEntryForm = "exported"

	tipArtExport  = "Write this revision and its screenshots to the lading store, where anyone who can read the store can read them until the retention ends. You confirm first."
	tipExportDo   = "Export now. If no endorsement covers this revision, confirming also endorses it for publishing."
	tipExportStop = "Close this without exporting."
)

var (
	atomsArtExport = c.Atoms().Text(icons.PhExport + " Export").Keep()
	atomsExportDo  = c.Atoms().Text(icons.PhCheck + " Export").Keep()
	atomsExportOpn = c.Atoms().Text(icons.PhCheck + " Export and open").Keep()
	atomsExportNo  = c.Atoms().Text(icons.PhX + " Cancel").Keep()
)

// exportRecord is the body of an export's journal entry.
type exportRecord struct {
	Revision   int           `json:"revision"`
	Mount      uint64        `json:"mount"`
	SnapNanos  int64         `json:"snapNanos"`
	TextDigest string        `json:"textDigest"`
	Images     []exportImage `json:"images,omitempty"`
}

type exportImage struct {
	Name string `json:"name"`
	Hash string `json:"hash"`
}

// exportPlan is what an export would write and under which label, computed
// when the person asks so the confirmation shows what Confirm does. It holds
// the conversation it was asked in, taken on the frame goroutine, so the
// export runs against that one whatever the frame does meanwhile.
type exportPlan struct {
	coord  *coordinator
	art    *artefact
	rev    int
	text   string
	images []artImage
	bytes  int64
	label  agentrun.Label
	// confined says the conversation holds sealed data, which the store
	// may not hold; endorse that confirming also endorses the head.
	confined bool
	endorse  bool
	// thenMdedit opens the export in mdedit once it is written.
	thenMdedit bool
}

// exportLabel is the label an export carries: the run's, with the model's
// integrity, as a copy carries it; confined when the conversation is.
func (inst *coordinator) exportLabel() (label agentrun.Label, confined bool) {
	label = agentrun.Label{Level: agentrun.LevelOwnModel}
	if w := inst.journalNow(); w != nil {
		label = label.Join(w.Run().Label)
	}
	_, _, confined = inst.state()
	return label, confined || label.Tier >= inst.confinedTier
}

// exportMount is the conversation's mount: the one its last export named,
// zero before the first.
func (inst *coordinator) exportMount() (mount uint64) {
	inst.mu.Lock()
	mount = inst.lastExportMount
	inst.mu.Unlock()
	if mount != 0 {
		return
	}
	if w := inst.journalNow(); w != nil {
		if rec, ok := lastExport(w.Entries()); ok {
			mount = rec.Mount
		}
	}
	return
}

// revisionsNamed is the highest artefact revision the journal names, by an
// endorsement or an export. A resumed artefact numbers its revisions above
// it, so a number from before the resume is not handed out again and an
// endorsement of it does not seem to name a new revision (ADR-0342 §SD7).
func revisionsNamed(entries []agentrun.Entry) (n int) {
	for _, e := range entries {
		switch {
		case e.Kind == agentrun.KindEndorse && e.Key == endorseKeyArtefact:
			n = max(n, revisionOf(e))
		case e.Kind == agentrun.KindIgnorable && e.Name == exportEntryName && e.Form == exportEntryForm:
			var rec exportRecord
			if json.Unmarshal([]byte(e.Body), &rec) == nil {
				n = max(n, rec.Revision)
			}
		}
	}
	return
}

// lastExport is the latest export entry's record.
func lastExport(entries []agentrun.Entry) (rec exportRecord, ok bool) {
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		if e.Kind != agentrun.KindIgnorable || e.Name != exportEntryName || e.Form != exportEntryForm {
			continue
		}
		if json.Unmarshal([]byte(e.Body), &rec) == nil {
			return rec, true
		}
	}
	return
}

// exportOf is the latest export of revision n, if it exported text as it
// stands.
func exportOf(entries []agentrun.Entry, n int, text string) (rec exportRecord, ok bool) {
	rec, ok = lastExport(entries)
	if !ok || rec.Revision != n || rec.TextDigest != agentrun.ContentDigest(text) {
		return exportRecord{}, false
	}
	return rec, true
}

// journalExport records an export the store wrote.
func (inst *coordinator) journalExport(ctx context.Context, rec exportRecord) (err error) {
	inst.mu.Lock()
	inst.lastExportMount = rec.Mount
	inst.mu.Unlock()
	w := inst.journalNow()
	if w == nil {
		return
	}
	body, err := json.Marshal(rec)
	if err != nil {
		return eh.Errorf("encode the export record: %w", err)
	}
	_, err = inst.append(journalCtx(ctx), w, agentrun.Entry{Kind: agentrun.KindIgnorable, Name: exportEntryName, Form: exportEntryForm,
		Key: strconv.FormatUint(rec.Mount, 16), Body: string(body), Label: harnessLabel()})
	return
}

// planExport opens the confirmation for the head revision.
func (inst *App) planExport(thenMdedit bool) {
	art := inst.conv.art
	n, text, set := art.headWithImages()
	p := &exportPlan{coord: inst.coord, art: art, rev: n, text: text, bytes: int64(len(text)), thenMdedit: thenMdedit}
	for _, e := range set {
		if !e.purged {
			p.images = append(p.images, e)
			p.bytes += e.bytes
		}
	}
	if coord := inst.coord; coord != nil {
		p.label, p.confined = coord.exportLabel()
		p.endorse = coord.journalNow() != nil && !coord.endorsedHead()
	}
	inst.artView.export = p
}

// renderExportAsk is the confirmation: what goes where, for how long, who
// can read it, and whether confirming also endorses.
func (inst *App) renderExportAsk(p *exportPlan, head int) {
	section("Export to the lading store")
	files := plural(1+len(p.images), "file")
	c.Label("revision " + strconv.Itoa(p.rev) + " · " + files + " · " + humanBytes(p.bytes) + " · " + p.label.Tier.String()).Selectable(false).Send()
	weak(exportDocPath + " and " + plural(len(p.images), "screenshot") + ", as one snapshot of this conversation's mount.")
	weak("Kept for " + strconv.Itoa(exportTtlDays) + " days. Anyone who can read the lading store can read these files until then.")
	if p.thenMdedit {
		weak("Then mdedit opens the export, with its screenshots.")
	}
	if p.endorse {
		weak("No endorsement covers this revision: confirming also endorses it for publishing.")
	}
	if p.rev != head {
		weak("The artefact moved since you asked; close this and export again.")
	}
	if p.confined {
		weak("This conversation holds sealed data, which the lading store may not hold: it cannot be exported.")
	}
	for range c.HorizontalTop().KeepIter() {
		if !p.confined && p.rev == head {
			atoms := atomsExportDo
			if p.thenMdedit {
				atoms = atomsExportOpn
			}
			for range c.HoverText(tipExportDo).KeepIter() {
				if c.Button(inst.ids.PrepareStr("export-do"), atoms).SendResp().HasPrimaryClicked() {
					inst.artView.export = nil
					inst.export(p)
				}
			}
		}
		for range c.HoverText(tipExportStop).KeepIter() {
			if c.Button(inst.ids.PrepareStr("export-no"), atomsExportNo).SendResp().HasPrimaryClicked() {
				inst.artView.export = nil
			}
		}
	}
}

// export runs exportNow off the frame goroutine.
func (inst *App) export(p *exportPlan) {
	name := "Artefact"
	if inst.conv.title != "" {
		name = inst.conv.title + " — artefact"
	}
	inst.runAction("chat-export", "export the artefact", func(ctx context.Context) (note string, err error) {
		return inst.exportNow(ctx, p, name)
	}, nil)
}

// exportNow gathers the files, asks the service whether it would write them,
// endorses when it must, publishes, journals, and opens mdedit when asked.
// Everything that can refuse runs before the endorsement, so the journal
// holds no consent to a publish that could not happen.
func (inst *App) exportNow(ctx context.Context, p *exportPlan, name string) (note string, err error) {
	bus, coord, art := inst.bus, p.coord, p.art
	if n, _ := art.head(); n != p.rev {
		return "", eh.Errorf("the artefact moved since the export was asked for; export again")
	}
	// The label as it stands now: a turn may have read more since the
	// confirmation opened.
	label, confined := p.label, p.confined
	if coord != nil {
		label, confined = coord.exportLabel()
		confined = confined || p.confined
	}
	req := ladingpublish.Request{Name: name, TtlDays: exportTtlDays, Tier: uint8(label.Tier), Confined: confined,
		Files: []ladingpublish.File{{Path: exportDocPath, Data: []byte(p.text)}}}
	rec := exportRecord{Revision: p.rev, TextDigest: agentrun.ContentDigest(p.text)}
	for _, e := range p.images {
		var data []byte
		if data, err = art.store.read(e.hash); err != nil {
			return "", eb.Build().Str("image", e.name).Errorf("read the screenshot: %w", err)
		}
		req.Files = append(req.Files, ladingpublish.File{Path: e.name, Data: data})
		rec.Images = append(rec.Images, exportImage{Name: e.name, Hash: e.hash})
	}
	if coord != nil {
		req.Mount = coord.exportMount()
	}
	if err = ladingpublish.Check(bus, exportMountTag, req); err != nil {
		return "", err
	}
	if p.endorse && coord != nil {
		if err = coord.endorseArtefact(ctx, p.rev, p.text); err != nil {
			return "", eh.Errorf("endorse the revision: %w", err)
		}
	}
	res, err := ladingpublish.Publish(bus, exportMountTag, req)
	if err != nil {
		return "", err
	}
	rec.Mount, rec.SnapNanos = res.Mount, res.SnapNanos
	if coord != nil {
		if err = coord.journalExport(ctx, rec); err != nil {
			return "", eh.Errorf("the export was written but not journaled: %w", err)
		}
	}
	note = "exported revision " + strconv.Itoa(p.rev) + " to the lading store, kept until " + res.Expires().Format("2006-01-02")
	if !p.thenMdedit {
		return
	}
	l := launchcfg.MdeditLaunch{Text: p.text, Name: name, Mount: res.Mount, SnapNanos: res.SnapNanos, Path: exportDocPath}
	cfg, err := buscodec.Encode(l)
	if err != nil {
		return "", eh.Errorf("encode the mdedit launch config: %w", err)
	}
	if _, err = windowhost.RequestOpen(bus, launchcfg.AppId, launchcfg.Kind, cfg); err != nil {
		return "", eh.Errorf("open mdedit: %w", err)
	}
	return note + ", and opened it in mdedit", nil
}

// readExport reads an export back into a fresh artefact whose one revision
// is the exported one, under its own number, so an endorsement of it still
// names it. The store is read directly, as every lading reader reads it
// (ADR-0341); the text and each screenshot are checked against the digests
// the export recorded.
func readExport(ctx context.Context, rec exportRecord) (art *artefact, err error) {
	client := chclient.New(chclient.ConfigFromEnv(), nil)
	if err = client.Ping(ctx); err != nil {
		return nil, eh.Errorf("ClickHouse not reachable: %w", err)
	}
	exec, err := storeexec.New(client, nil)
	if err != nil {
		return
	}
	if err = lading.Verify(ctx, exec); err != nil {
		return nil, eh.Errorf("the lading store: %w", err)
	}
	stores := lading.NewStores(exec, ladingschema.Layout{})
	defer stores.Meta.Close()
	defer stores.Data.Close()
	v, err := ladingadapter.Open(stores, identifier.TaggedId(rec.Mount), time.Unix(0, rec.SnapNanos), ladingadapter.WithContext(ctx))
	if err != nil {
		return nil, eh.Errorf("open the export's snapshot: %w", err)
	}
	art = newArtefact()
	if err = restoreFrom(v, rec, art); err != nil {
		art.close()
		return nil, err
	}
	return
}

// restoreFrom fills art, which has no revision yet, from an export's files.
func restoreFrom(v fs.FS, rec exportRecord, art *artefact) (err error) {
	text, err := fs.ReadFile(v, exportDocPath)
	if err != nil {
		return eh.Errorf("read the text: %w", err)
	}
	if agentrun.ContentDigest(string(text)) != rec.TextDigest {
		return eh.Errorf("the text is not the one exported")
	}
	set := make([]artImage, 0, len(rec.Images))
	for _, img := range rec.Images {
		var data []byte
		if data, err = fs.ReadFile(v, img.Name); err != nil {
			return eb.Build().Str("image", img.Name).Errorf("read the screenshot: %w", err)
		}
		hash, w, h, perr := art.store.put(data)
		if perr == nil && hash != img.Hash {
			perr = eh.Errorf("its bytes are not the ones exported")
		}
		if perr != nil {
			return eb.Build().Str("image", img.Name).Errorf("restore the screenshot: %w", perr)
		}
		set = append(set, artImage{name: img.Name, hash: hash, w: w, h: h, bytes: int64(len(data)), source: imageSourceRestored, root: hash})
	}
	art.mu.Lock()
	art.lastN = max(rec.Revision-1, 0)
	art.mu.Unlock()
	_, err = art.commit(0, artRevision{text: string(text), images: set, ownImages: true, source: revSourceModel,
		title: "restored from the export of revision " + strconv.Itoa(rec.Revision)})
	return
}
