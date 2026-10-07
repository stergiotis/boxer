package play

// A window shows an ad-hoc bundle (ADR-0288 (proposed) §SD4): the bundle's
// applet document becomes the buffer, its datasets are bound under the
// local names the document reads, and the window follows the bundle — a
// republish reloads the document and rebinds, a retract leaves the window
// saying what it waits for. A window opens one by its launch config
// (launchcfg.PlayLaunch.Bundle) or an agent's open_bundle.
//
// Nothing here asks the bus on the render goroutine. A resolve runs on a
// goroutine of its own and its answer waits in the state's mailbox until
// the next frame applies it, so a window not drawn for a while shows the
// bundle as it stands when it is next drawn (§SD7).
//
// The applet document format is sqlapplet's (ADR-0132), and sqlapplet
// imports play, so play reaches the parser through SetAppletDocParser,
// which sqlapplet installs when it is linked.

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/apps/play/launchcfg"
	"github.com/stergiotis/boxer/public/keelson/runtime/adhocdata"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect/keelsonquery"
)

// AppletDoc is an applet document reduced to what a play window applies.
type AppletDoc struct {
	Title    string
	Sql      string
	BandsSql string
	// Datasets are the local names the document reads in keelson('…').
	Datasets []string
	// Tab is the first pane the document names, empty for none.
	Tab string
	// Introspection is true when the document asks for the introspection
	// endpoint, where ad-hoc datasets resolve.
	Introspection bool
	Preamble      []byte
	// Runnable is true when the document's SQL is a plain read, which a
	// window runs on open, as an applet's (ADR-0132 §SD5).
	Runnable bool
}

// AppletDocParserFunc parses an applet document.
type AppletDocParserFunc func(path string, src []byte) (doc AppletDoc, err error)

var appletDocParser atomic.Pointer[AppletDocParserFunc]

// SetAppletDocParser installs the applet document parser play opens bundles
// with; sqlapplet installs its own at init. Without one a bundle is refused
// with the reason.
func SetAppletDocParser(fn AppletDocParserFunc) {
	appletDocParser.Store(&fn)
}

func parseAppletDoc(path string, src []byte) (doc AppletDoc, err error) {
	fn := appletDocParser.Load()
	if fn == nil {
		return doc, app.RefuseOperation("no applet document parser is linked into this host, so a bundle cannot be opened")
	}
	return (*fn)(path, src)
}

// bundlePollInterval paces re-asking for a bundle that is not live when no
// bundle events reach the window.
const bundlePollInterval = adhocdata.DefaultPollInterval

// bundleState is the bundle a window follows. The render goroutine owns
// the applied fields; mu guards the mailbox the event handler and the
// resolve goroutine write.
type bundleState struct {
	alias string
	bus   app.BusI
	log   zerolog.Logger
	// obo is the agent call that opened the bundle; the first resolve is
	// its work and is attested as such, later ones are the window's own.
	obo *app.OnBehalfOf

	// Applied on the render goroutine.
	revision uint64
	digest   string
	locals   []string
	follower *adhocdata.Follower
	unsub    func()
	polled   bool
	nextPoll time.Time
	// runnable is the applied document's: a plain read, run on open and
	// on a rebind.
	runnable bool

	mu        sync.Mutex
	dirty     bool
	inflight  bool
	arrived   *adhocdata.BundleResult
	waiting   string
	withdrawn bool
}

// openBundle makes the window follow alias, replacing a bundle it followed
// before. It returns at once; the next frames resolve and apply it.
func (inst *PlayLauncher) openBundle(alias string, obo *app.OnBehalfOf) {
	inst.closeBundle()
	st := &bundleState{alias: alias, bus: inst.bus, log: inst.log, obo: obo, dirty: true, waiting: "not asked yet"}
	if inst.bus != nil {
		unsub, err := inst.bus.Subscribe(adhocdata.SubjectBundleEventAll, func(msg *app.Msg) {
			ev, dErr := adhocdata.DecodeEvent(msg.Subject, msg.Payload)
			if dErr != nil || ev.Bundle != alias {
				return
			}
			st.mu.Lock()
			st.dirty = true
			st.withdrawn = ev.Op == adhocdata.EventOpRetracted
			st.mu.Unlock()
		})
		if err == nil {
			st.unsub = unsub
		} else {
			st.polled = true
		}
	}
	inst.bundle = st
}

// closeBundle stops following the bundle and unbinds its datasets.
func (inst *PlayLauncher) closeBundle() {
	st := inst.bundle
	if st == nil {
		return
	}
	if st.unsub != nil {
		st.unsub()
	}
	if st.follower != nil {
		st.follower.Close()
	}
	if inst.inner != nil {
		for _, local := range st.locals {
			_ = inst.inner.UnbindDataset(local)
			inst.inner.client.setDatasetOrigin(local, "", "")
		}
	}
	inst.bundle = nil
}

// syncBundle is the bundle's frame step: start a resolve when one is due,
// apply an answer that arrived, keep the datasets bound.
func (inst *PlayLauncher) syncBundle() {
	st := inst.bundle
	if st == nil || inst.inner == nil {
		return
	}
	now := time.Now()
	st.mu.Lock()
	if st.polled && !st.inflight && now.After(st.nextPoll) && (st.withdrawn || st.revision == 0) {
		st.dirty = true
	}
	start := st.dirty && !st.inflight && st.bus != nil
	if start {
		st.dirty, st.inflight = false, true
		st.nextPoll = now.Add(bundlePollInterval)
	}
	arrived := st.arrived
	st.arrived = nil
	waiting, withdrawn := st.waiting, st.withdrawn
	st.mu.Unlock()
	if start {
		obo := st.obo
		st.obo = nil
		go st.resolve(obo)
	}
	if arrived != nil {
		inst.applyBundle(st, *arrived)
	}
	if st.follower != nil {
		if bound, _ := st.follower.Sync(inst.inner); bound && st.runnable {
			inst.inner.RequestRun()
		}
	}
	switch {
	case withdrawn:
		inst.inner.SetDatasetNotice([]byte("**Bundle `" + st.alias + "` was withdrawn.** Its publisher retracted it or closed its window. The window reloads it by itself if it is published again."))
	case st.revision == 0 && waiting != "":
		inst.inner.SetDatasetNotice([]byte("**Waiting for bundle `" + st.alias + "`:** " + waiting + ". The window opens it by itself once it is published."))
	}
}

// resolve asks the service for the bundle off the render goroutine and
// leaves the answer in the mailbox.
func (inst *bundleState) resolve(obo *app.OnBehalfOf) {
	res, err := adhocdata.ResolveBundleRequest(inst.bus, inst.alias, obo)
	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.inflight = false
	if err != nil {
		inst.waiting = err.Error()
		return
	}
	inst.waiting, inst.withdrawn = "", false
	inst.arrived = &res
}

// applyBundle applies a resolved bundle on the render goroutine: a new
// document replaces the buffer and what came with it, a changed set of
// local names rebinds, and either runs the buffer.
func (inst *PlayLauncher) applyBundle(st *bundleState, res adhocdata.BundleResult) {
	p := inst.inner
	if res.Revision == st.revision && res.DocumentDigest == st.digest {
		return
	}
	doc, err := parseAppletDoc(st.alias+".md", res.Document)
	if err != nil {
		p.SetDatasetNotice([]byte("**Bundle `" + st.alias + "` cannot be opened:** " + err.Error()))
		st.revision, st.digest = res.Revision, res.DocumentDigest
		return
	}
	changed := false
	if res.DocumentDigest != st.digest {
		p.swapSql(doc.Sql)
		if doc.BandsSql != "" {
			p.SetTimelineBandsSql(doc.BandsSql)
		}
		p.SetDefinitionMarkdown(res.Document)
		p.SetPreambleMarkdown(doc.Preamble)
		if doc.Tab != "" {
			if tabErr := p.ActivateTab(doc.Tab); tabErr != nil {
				inst.log.Debug().Err(tabErr).Str("tab", doc.Tab).Msg("play: bundle tab not activated")
			}
		}
		changed = true
	}
	locals := make([]string, 0, len(res.Datasets))
	aliases := make([]string, 0, len(res.Datasets))
	names := make(map[string]string, len(res.Datasets))
	for _, d := range res.Datasets {
		locals = append(locals, d.LocalName)
		aliases = append(aliases, d.Alias)
		names[d.Alias] = d.LocalName
	}
	if !slices.Equal(locals, st.locals) || st.follower == nil {
		if st.follower != nil {
			st.follower.Close()
		}
		for _, local := range st.locals {
			_ = p.UnbindDataset(local)
			p.client.setDatasetOrigin(local, "", "")
		}
		st.follower = adhocdata.NewDeferredFollower(adhocdata.FollowerConfig{Bus: st.bus, Log: st.log, LocalNames: names})
		if st.follower != nil {
			for _, a := range aliases {
				st.follower.FollowAs(a, names[a])
				// A grant names the dataset by its bundle, never by the
				// local name the document reads (§SD3).
				p.client.setDatasetOrigin(names[a], a, st.alias)
			}
		}
		st.locals = locals
		changed = true
	}
	st.revision, st.digest = res.Revision, res.DocumentDigest
	st.runnable = doc.Runnable
	p.SetDatasetNotice(nil)
	if changed && doc.Runnable {
		p.RequestRun()
	}
}

// bundleLaunchEndpoint reports whether a launch config takes the window to
// the introspection endpoint: asked for, or implied by a bundle.
func bundleLaunchEndpoint(launch *launchcfg.PlayLaunch) (introspection bool) {
	return launch != nil && (launch.Endpoint == launchcfg.EndpointIntrospection || launch.Bundle != "")
}

const (
	opListBundles = "list_bundles"
	opOpenBundle  = "open_bundle"

	opsResBundle = "bundle"
)

// OpenBundleArgs is open_bundle's argument.
type OpenBundleArgs struct {
	Alias string `desc:"the bundle's alias, as list_bundles names it"`
}

// OpenBundleResult is open_bundle's result.
type OpenBundleResult struct {
	Alias      string `desc:"the bundle the window now follows"`
	Endpoint   string `desc:"the endpoint the window queries: the introspection endpoint, where the bundle's datasets resolve"`
	Retargeted bool   `json:",omitzero" desc:"true when the window queried another endpoint before; the buffer it had is replaced as well"`
}

// BundleListArgs is list_bundles' argument.
type BundleListArgs struct{}

// BundleInfo is one live bundle.
type BundleInfo struct {
	Alias          string   `desc:"the bundle's alias, what open_bundle takes"`
	Publisher      string   `desc:"the app that published it"`
	Revision       uint64   `desc:"its revision; a republish bumps it"`
	LocalNames     []string `desc:"its datasets as its document reads them, keelson('<local name>') once it is open"`
	DatasetAliases []string `desc:"the same datasets' global aliases, index for index"`
	Task           string   `json:",omitzero" desc:"the agent task whose call published the live revision"`
	Turn           string   `json:",omitzero" desc:"and the conversation turn that call belongs to"`
}

// BundleList is list_bundles' result.
type BundleList struct {
	Bundles []BundleInfo `desc:"the live bundles, by alias"`
	Open    string       `json:",omitzero" desc:"the bundle this window follows, if any"`
	// LastPublish is this window's last publish_result.
	LastPublish *LastPublish `json:",omitzero" desc:"this window's last publish_result: the bundle, the revision it made or why it failed, or that it is in flight"`
}

// bundleListSql reads the catalog with every column as text the decoder
// takes without guessing at 64-bit integer quoting.
const bundleListSql = "SELECT alias, publisher, toString(revision) AS revision, local_names, dataset_aliases, task, turn FROM keelson('" +
	adhocdata.BundleCatalogTableName + "') ORDER BY alias"

// bundleListTimeout bounds list_bundles' read of the catalog.
const bundleListTimeout = 5 * time.Second

func addBundleOps(s *appops.Set[*PlayLauncher, opsSnap]) {
	s.Resource(opsResBundle, "the bundle the window follows and the revision it shows", func(inst *PlayLauncher) any {
		if inst.bundle == nil {
			return ""
		}
		return inst.bundle.alias + "@" + strconv.FormatUint(inst.bundle.revision, 10)
	})
	appops.ExternalRead(s, app.OperationSpec{Name: opListBundles, Version: 1,
		Summary: "list the live ad-hoc bundles — an applet document and the datasets it reads, published together — with who published each and the datasets' local names",
		Agents:  true, Untrusted: true,
		Follows: []string{"open_bundle opens one in this window"}},
		func(sn opsSnap, call app.OperationCall, in BundleListArgs) (out BundleList, err error) {
			if sn.bus == nil {
				return out, app.RefuseOperation("the window has no bus to read the bundle catalog over")
			}
			out, err = listBundles(sn.bus)
			out.Open = sn.bundle
			if sn.lastPublish.Bundle != "" {
				last := sn.lastPublish
				out.LastPublish = &last
			}
			return
		})
	appops.Command(s, app.OperationSpec{Name: opOpenBundle, Version: 1,
		Summary: "open an ad-hoc bundle in this window: its document replaces the buffer, its datasets are bound under the names the document reads, and the window follows its republishes",
		Effect:  app.OperationEffectDocument, Writes: []string{opsResSql, opsResBundle}, Agents: true,
		Follows: []string{"the window resolves the bundle off the frame, applies it and runs the buffer; list_panes then says what each pane draws",
			"the window moves to the introspection endpoint, where ad-hoc datasets resolve",
			"a republish reloads the document and rebinds; a retract leaves the window waiting for the bundle",
			"a run reading the bundle's datasets needs keelson-bundle:<alias> in the grant"}},
		func(inst *PlayLauncher, call app.OperationCall, in OpenBundleArgs) (out OpenBundleResult, err error) {
			p := inst.inner
			switch {
			case p == nil:
				return out, app.RefuseOperation("the window has not mounted")
			case !validDatasetIdentifier(in.Alias):
				return out, app.RefuseOperation("a bundle alias is a bare identifier: letters, digits and _, at most 64 bytes")
			case appletDocParser.Load() == nil:
				return out, app.RefuseOperation("no applet document parser is linked into this host, so a bundle cannot be opened")
			}
			ep := introspect.LocalQueryEndpoint()
			if ep == "" {
				return out, app.RefuseOperation("this host serves no introspection endpoint, where a bundle's datasets resolve")
			}
			out = OpenBundleResult{Alias: in.Alias, Endpoint: ep}
			if p.client.URL() != ep {
				p.setEndpoint(ep)
				out.Retargeted = true
			}
			inst.openBundle(in.Alias, call.OnBehalfOf)
			p.markAgent(call.OnBehalfOf)
			return
		})
}

// bundleRow is one catalog row as the read decodes it.
type bundleRow struct {
	Alias          string   `json:"alias"`
	Publisher      string   `json:"publisher"`
	Revision       string   `json:"revision"`
	LocalNames     []string `json:"local_names"`
	DatasetAliases []string `json:"dataset_aliases"`
	Task           string   `json:"task"`
	Turn           string   `json:"turn"`
}

// listBundles reads keelson('adhoc_bundles') over keelson.query.
func listBundles(bus app.BusI) (out BundleList, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), bundleListTimeout)
	defer cancel()
	res, err := keelsonquery.NewClient(bus).Query(ctx, adhocdata.BundleCatalogTableName, bundleListSql, "JSONEachRow")
	if err != nil {
		return out, app.RefuseOperation("the bundle catalog could not be read: " + err.Error())
	}
	out.Bundles, err = decodeBundleRows(res.Body)
	return
}

func decodeBundleRows(body []byte) (bundles []BundleInfo, err error) {
	for line := range bytes.SplitSeq(body, []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var r bundleRow
		if err = json.Unmarshal(line, &r); err != nil {
			return nil, app.RefuseOperation("the bundle catalog row does not decode: " + err.Error())
		}
		rev, _ := strconv.ParseUint(r.Revision, 10, 64)
		bundles = append(bundles, BundleInfo{Alias: r.Alias, Publisher: r.Publisher, Revision: rev,
			LocalNames: r.LocalNames, DatasetAliases: r.DatasetAliases, Task: r.Task, Turn: r.Turn})
	}
	return
}
