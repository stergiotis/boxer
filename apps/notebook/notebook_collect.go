package notebook

// Collection (ADR-0371 §SD8, §SD10): each record's statement is run once on
// the server, as the command line runs it, and the text with every file it
// names is published as a new snapshot under the app's mount tag. The
// window then opens that snapshot.
//
// The person states the tree's confidentiality tier when they confirm
// (ADR-0341 §SD3): the stored results are whatever the records read, which
// the window cannot know. Whether the tree is confined is not stated but
// derived: a notebook whose records name sealed data is refused before
// anything runs, since the store holds none.

import (
	"context"
	"io"
	"io/fs"
	"path"
	"sort"
	"strings"

	"github.com/stergiotis/boxer/apps/play"
	"github.com/stergiotis/boxer/public/db/clickhouse/chclient"
	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stergiotis/boxer/public/identity/tagmint"
	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/bgjob"
	"github.com/stergiotis/boxer/public/keelson/runtime/ladingpublish"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/semistructured/markdown/mdnotebook"
)

// mountTag is the tag of the mounts collected notebooks are published
// under (ADR-0198 §SD3): one mount per notebook, minted by the publish
// service at its first collection.
const mountTag identifier.TagValue = 2178324

// The claim keeps another claimant in the link set off the tag value;
// testdata/idstream-claims.golden pins it, since a moved value orphans every
// mount already minted under it. The declared cardinality is the one the
// value's class serves (2^31, width 32); the value is kept as it was first
// minted rather than moved into the class 2^24 would advise.
var mountClaim = tagmint.MustClaim("notebookMount", mountTag, 1<<31)

// idStreams lists the streams this package claims, for the golden.
var idStreams = []tagmint.ClaimedTagValue{mountClaim}

// blobDirSuffix follows a notebook's stem to name the folder of its blobs.
const blobDirSuffix = ".blobs"

// collected is one collection that was published.
type collected struct {
	ref     snapshotRef
	records []string
	files   uint64
	bytes   uint64
	expires string
}

// collectState is the window's collection: the confirmation, the run and
// what the last one came to.
type collectState struct {
	job bgjob.Runner[collected]
	// armed says the confirmation row is shown; tier is what it states.
	armed bool
	tier  agentrun.TierE
	// seq counts collections started, done the ones that ended; last and
	// lastErr are how the latest ended.
	seq, done uint64
	last      *collected
	lastErr   string
	// mount is a mount of this window's own from an earlier collection,
	// written again as a new snapshot; zero mints one.
	mount uint64
}

// defaultTier is the tier the confirmation starts on: the highest the
// store may hold, so a person who changes nothing understates nothing the
// store would take.
func defaultTier() agentrun.TierE { return ladingpublish.LimitsFromEnv().Ceiling }

// runOnServer sends a statement on a connection made from the
// environment, as `boxer markdown notebook collect` does.
func runOnServer() mdnotebook.RunFunc {
	client := chclient.New(chclient.ConfigFromEnv(), nil)
	return func(ctx context.Context, statement string) (stream []byte, err error) {
		body, err := client.Query(ctx, statement)
		if err != nil {
			return nil, err
		}
		defer body.Close()
		return io.ReadAll(body)
	}
}

// publishOnBus asks the store's write path whether it would take the tree,
// then publishes it.
func publishOnBus(ctx context.Context, bus app.BusI) func(req ladingpublish.Request) (ladingpublish.Result, error) {
	return func(req ladingpublish.Request) (res ladingpublish.Result, err error) {
		if err = ladingpublish.Check(ctx, bus, mountTag, req); err != nil {
			return
		}
		return ladingpublish.Publish(ctx, bus, mountTag, req)
	}
}

// collectable says whether a collection may start, and why not.
func (inst *App) collectable() (reason string) {
	switch {
	case inst.doc == nil || inst.nb == nil:
		return "no notebook is open"
	case inst.collect.job.Running():
		return "a collection is running; wait for it to end"
	case len(inst.records()) == 0:
		return "the notebook has no record to collect"
	case inst.publish == nil && inst.bus == nil:
		return "this host gives the window no bus to reach the lading store through"
	}
	sql, err := mdnotebook.Buffer(inst.nb)
	if err != nil {
		return "the notebook's records: " + err.Error()
	}
	if sealed := play.SealedNames(sql); len(sealed) > 0 {
		return "a record reads sealed data (" + strings.Join(sealed, ", ") + "), which the lading store does not hold"
	}
	return ""
}

// startCollect starts a collection that asserts tier. A refusal names what
// stands in the way; nothing has run then.
func (inst *App) startCollect(tier agentrun.TierE) (err error) {
	if reason := inst.collectable(); reason != "" {
		return app.RefuseOperation(reason)
	}
	if tier > agentrun.TierRed {
		return app.RefuseOperation("a tier is CLEAR, GREEN, AMBER, AMBER+STRICT or RED")
	}
	run, publish := inst.run, inst.publish
	if run == nil {
		run = runOnServer()
	}
	if publish == nil {
		publish = publishOnBus(inst.ioCtx, inst.bus)
	}
	nb, doc, mount := inst.nb, inst.doc, inst.collect.mount
	total := uint64(len(inst.records()))
	inst.collect.armed = false
	inst.collect.seq++
	inst.collect.lastErr = ""
	inst.note = ""
	inst.collect.job.StartReporting(inst.tasks, bgjob.Spec{Kind: "notebook.collect", Title: "Collect " + doc.name},
		func(ctx context.Context, report bgjob.Reporter) (*collected, error) {
			var ran uint64
			counted := func(ctx context.Context, statement string) ([]byte, error) {
				report(ran, total, "record "+itoa(ran+1)+" of "+itoa(total))
				stream, rErr := run(ctx, statement)
				ran++
				return stream, rErr
			}
			return collectAndPublish(ctx, nb, doc, mount, tier, counted, publish, func() { report(total, total, "publishing the snapshot") })
		})
	return nil
}

// collectAndPublish collects nb and publishes its text with every file it
// names: the new blobs, and what it kept — a stored result no cell
// computes, a recording, an image — read from where the notebook was
// opened. The tree is the notebook's own: its text at the root and its
// files beside it.
//
// n is the notebook's records, f its files: the work is one statement per
// record on the server and O(bytes of f) here.
func collectAndPublish(ctx context.Context, nb *mdnotebook.Notebook, doc *document, mount uint64, tier agentrun.TierE,
	run mdnotebook.RunFunc, publish func(ladingpublish.Request) (ladingpublish.Result, error), publishing func()) (out *collected, err error) {
	stem := strings.TrimSuffix(doc.name, path.Ext(doc.name))
	col, err := mdnotebook.Collect(ctx, nb, stem+blobDirSuffix, run)
	if err != nil {
		return nil, err
	}
	sealed, err := mdnotebook.Read(col.Text)
	if err != nil {
		return nil, eh.Errorf("read the collected text: %w", err)
	}
	files := map[string][]byte{doc.name: col.Text}
	for p, data := range col.Blobs {
		files[p] = data
	}
	keep := func(p string) {
		if _, have := files[p]; have || p == "" || strings.Contains(p, "://") {
			return
		}
		// A file that cannot be read is a problem of the notebook as it was
		// opened, and stays one in the snapshot.
		if data, rErr := fs.ReadFile(doc.fsys, path.Clean(p)); rErr == nil {
			files[path.Clean(p)] = data
		}
	}
	for _, r := range col.Records {
		keep(r.Blob)
	}
	for i := range sealed.Cells {
		if cell := &sealed.Cells[i]; cell.Kind == mdnotebook.CellKindMedia {
			keep(cell.Path)
		}
	}
	for _, img := range sealed.Images {
		keep(img.Path)
	}
	req := ladingpublish.Request{Mount: mount, Name: stem, Tier: uint8(tier)}
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		req.Files = append(req.Files, ladingpublish.File{Path: p, Data: files[p]})
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	publishing()
	res, err := publish(req)
	if err != nil {
		return nil, err
	}
	out = &collected{
		ref:     snapshotRef{mount: identifier.TaggedId(res.Mount), snap: res.Snap(), path: doc.name},
		files:   res.Files,
		bytes:   res.Bytes,
		expires: res.Expires().Format("2006-01-02"),
	}
	for _, r := range col.Records {
		out.records = append(out.records, r.Name)
	}
	return out, nil
}

// landCollect takes a finished collection: the window opens the snapshot
// that was written, and the status row says how it ended.
func (inst *App) landCollect() {
	cs := &inst.collect
	if res, _, ok := cs.job.TakeResult(); ok {
		cs.done, cs.last, cs.lastErr = cs.seq, res, ""
		cs.mount = res.ref.mount.Value()
		ref := res.ref
		inst.opening = &ref
		inst.touched = true
		inst.note = "collected " + itoa(uint64(len(res.records))) + " records and published " + itoa(res.files) +
			" files to the lading store as " + ref.String() + ", kept until " + res.expires
		return
	}
	if cs.done < cs.seq {
		if snap := cs.job.Snapshot(); snap.State == bgjob.StateFailed || snap.State == bgjob.StateIdle {
			cs.done, cs.last = cs.seq, nil
			cs.lastErr = "the collection was cancelled"
			if snap.Err != nil {
				cs.lastErr = snap.Err.Error()
			}
			inst.note = "not collected: " + cs.lastErr
		}
	}
}
