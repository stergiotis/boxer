package adhocdemo

// The tree half of the demo (ADR-0222 §SD7). The dataset half above publishes
// computed rows and queries them in an embedded applet; this publishes a
// computed *tree* as a short-lived lading mount and opens tally on it, so one
// window shows both ad-hoc shapes and what each is for. The launch config
// carries a path-set query as well as the location, which exercises the other
// half of ADR-0222 in the same gesture: the opened window browses the mount
// in pane A and the query's rows in its Results pane.

import (
	"context"
	"fmt"
	"strings"
	"testing/fstest"
	"time"

	"github.com/stergiotis/boxer/apps/tally/launchcfg"
	"github.com/stergiotis/boxer/public/fs/lading/ladingadhoc"
	"github.com/stergiotis/boxer/public/fs/lading/ladingpolicy"
	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stergiotis/boxer/public/keelson/data/storeexec"
	"github.com/stergiotis/boxer/public/keelson/runtime/bgjob"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/windowhost"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// treeName is what the published tree is called in tally's Mounts pane — the
// alias, in ad-hoc dataset terms.
const treeName = "adhoc demo tree"

// treeTimeout bounds the whole gesture: connect, verify, walk, and the
// launch. Generous for a first connection, bounded so a wrong endpoint fails
// as a status line rather than a stuck button.
const treeTimeout = 30 * time.Second

// demoTree is the tree each publish walks: a handful of files whose content
// changes with the generation, so republishing shows a diff against the
// previous snapshot of the same mount rather than a second mount.
func demoTree(gen int) (fsys fstest.MapFS) {
	stamp := fmt.Sprintf("generation %d\n", gen)
	fsys = fstest.MapFS{
		"README.md": &fstest.MapFile{
			Data: []byte("# Ad-hoc tree\n\nPublished by adhocdemo. " + stamp),
			Mode: 0o644,
		},
		"notes/today.md": &fstest.MapFile{
			Data: []byte("- the tree is walked into the lading store\n- " + stamp),
			Mode: 0o644,
		},
		"data/series.csv": &fstest.MapFile{
			Data: []byte(seriesCSV(gen)),
			Mode: 0o644,
		},
		"data/nested/deep/leaf.txt": &fstest.MapFile{
			Data: []byte(strings.Repeat("leaf "+stamp, 8)),
			Mode: 0o644,
		},
	}
	return
}

// seriesCSV is the same shape the dataset half publishes as Arrow, written
// as a file — the two halves describe one computation in the two forms.
func seriesCSV(gen int) (text string) {
	var b strings.Builder
	b.WriteString("x,y\n")
	for i := 0; i < 32; i++ {
		fmt.Fprintf(&b, "%d,%d\n", i, (i*i+gen)%97)
	}
	return b.String()
}

// treeQuery is the path-set query the opened window's Results pane runs: the
// markdown in the published tree, largest first. Handle-form — the mount and
// the snapshot are literals — because the opened window inherits nothing from
// this one, the rule ad-hoc datasets follow for the same reason.
//
// `ext` carries its dot, as the store records it.
func treeQuery(mount identifier.TaggedId, snap time.Time) (sql string) {
	return fmt.Sprintf(`-- The markdown in the published tree.
SELECT path, mount, snap, is_dir, size
FROM fs(0x%X, %d)
WHERE NOT is_dir AND ext = '.md'
ORDER BY size DESC`, mount.Value(), snap.UnixNano())
}

// publishTree is the gesture, whole, as a task: connect, publish
// (republishing into the held mount so repeated presses leave one mount
// behind, not one per press), then open tally on it. Frame takes the
// result; a press while one runs is dropped by the runner.
func (inst *App) publishTree() {
	gen := inst.treeGen + 1
	mount := inst.treeMount
	started := inst.treeJob.Start(inst.tasks, bgjob.Spec{Kind: "adhocdemo.tree", Title: "publish the tree and open tally"},
		func(ctx context.Context) (res *ladingadhoc.PublishResult, err error) {
			publish := inst.publishTreeFn
			if publish == nil {
				publish = inst.publishAndOpen
			}
			r, err := publish(ctx, gen, mount)
			if err != nil {
				return
			}
			return &r, nil
		})
	if started {
		inst.treeGen = gen
		inst.treeNote = ""
	}
}

func (inst *App) publishAndOpen(ctx context.Context, gen int, mount identifier.TaggedId) (res ladingadhoc.PublishResult, err error) {
	if inst.bus == nil {
		err = eh.Errorf("no bus wired — opening tally needs the app runtime")
		return
	}
	ctx, cancel := context.WithTimeout(ctx, treeTimeout)
	defer cancel()

	exec, err := storeexec.ConnectFromEnv(ctx)
	if err != nil {
		return
	}
	res, err = ladingadhoc.Publish(ctx, exec, ladingadhoc.PublishInput{
		FS:        demoTree(gen),
		Name:      treeName,
		Publisher: string(ManifestId),
		Mount:     mount,
		Origin:    ladingpolicy.Origin{Run: inst.runId, App: string(ManifestId), Instance: inst.instance},
	})
	if err != nil {
		return
	}
	cfg := launchcfg.TallyLaunch{
		At:       time.Now().UTC(),
		MountA:   fmt.Sprintf("%016x", res.Mount.Value()),
		SnapA:    res.Snap.UTC().Format(time.RFC3339Nano),
		DirA:     ".",
		Target:   "A",
		Sql:      treeQuery(res.Mount, res.Snap),
		SqlLabel: treeName + " · markdown",
		Tab:      launchcfg.TabResults,
	}
	cfgBytes, err := buscodec.Encode(cfg)
	if err != nil {
		err = eh.Errorf("encode tally launch config: %w", err)
		return
	}
	if _, err = windowhost.RequestOpen(inst.bus, launchcfg.AppId, launchcfg.Kind, cfgBytes); err != nil {
		err = eh.Errorf("open tally: %w", err)
		return
	}
	return
}
