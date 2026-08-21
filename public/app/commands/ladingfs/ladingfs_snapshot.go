package ladingfs

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	cli "github.com/urfave/cli/v2"

	"github.com/stergiotis/boxer/public/fs/lading"
	"github.com/stergiotis/boxer/public/fs/lading/ladingdata"
	"github.com/stergiotis/boxer/public/fs/lading/ladingingest"
	"github.com/stergiotis/boxer/public/fs/lading/ladingmeta"
	"github.com/stergiotis/boxer/public/fs/lading/ladingpolicy"
	"github.com/stergiotis/boxer/public/fs/lading/ladingremote"
	"github.com/stergiotis/boxer/public/fs/lading/ladingschema"
	"github.com/stergiotis/boxer/public/keelson/data/chclient"
	"github.com/stergiotis/boxer/public/keelson/data/storeexec"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// newSnapshotCommand is `boxer fs snapshot`: walk one tree into the store as
// one snapshot (ADR-0198 §SD6). It is the command-line route ADR-0200 §SD5
// relies on — the browser takes no snapshots itself — and the one a first
// reader of the how-to reaches for before writing Go.
func newSnapshotCommand() *cli.Command {
	return &cli.Command{
		Name:      "snapshot",
		Usage:     "walk a directory (or an rclone remote) into the store as one snapshot",
		ArgsUsage: "<dir | remote:path>",
		Description: "Writes one complete snapshot of the tree under the mount id given. " +
			"The store is provisioned idempotently first unless --no-provision is set. " +
			"A failed walk leaves nothing a query can see; retry by running again. " +
			"With --remote the argument is an rclone remote (\"remote:path\"), served " +
			"through `rclone serve sftp --stdio`; a bare local path is refused there, " +
			"so the only way to read an ungranted local tree is to say so with a path.",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "mount", Required: true,
				Usage: "the mount id, decimal or 0x-prefixed hex — a tagged id the caller minted; the store claims none"},
			&cli.UintFlag{Name: "ttl-days", Value: uint(ladingingest.TtlClass30d),
				Usage: "retention class in whole days (7, 30, 90 are the named ones)"},
			&cli.Uint64Flag{Name: "inline-max", Value: ladingingest.DefaultPolicy().InlineMax,
				Usage: "files up to this many bytes have their content stored; larger ones record size, mtime and hash only"},
			&cli.StringFlag{Name: "text-rule", Value: "sniff",
				Usage: "sniff (cut text files at newlines; exact line numbers) or never (fixed-offset blocks)"},
			&cli.BoolFlag{Name: "meta-only", Usage: "stat only: one row per node, no content blocks"},
			&cli.StringFlag{Name: "profile", Value: "corpus",
				Usage: "table profile for provisioning: corpus (few mounts, large files) or fleet (very many small trees); fixed at table creation"},
			&cli.BoolFlag{Name: "no-provision", Usage: "do not create or finish the tables; only verify them"},
			&cli.StringFlag{Name: "name",
				Usage: "record the mount's declared policy under this name in boxer.facts (ladingingest.RecordPolicy); the book and the browser show it"},
			&cli.StringFlag{Name: "store", Value: ladingschema.DatabaseName,
				Usage: "the store name written into the policy record beside --name"},
			&cli.BoolFlag{Name: "remote", Usage: "the argument is an rclone remote (\"remote:path\"), not a local directory"},
			&cli.StringSliceFlag{Name: "filter",
				Usage: "with --remote: an rclone filter argument passed to the serving side (repeatable), e.g. --filter=--exclude --filter='*.tmp'"},
		},
		Action: runSnapshot,
	}
}

func runSnapshot(c *cli.Context) (err error) {
	if c.NArg() != 1 {
		return eh.Errorf("exactly one argument: a directory, or with --remote an rclone remote")
	}
	mount, err := parseMount(c.String("mount"))
	if err != nil {
		return
	}
	pol := ladingingest.DefaultPolicy()
	pol.Ttl = ladingingest.TtlClassE(c.Uint("ttl-days"))
	pol.InlineMax = c.Uint64("inline-max")
	pol.MetaOnly = c.Bool("meta-only")
	switch strings.ToLower(c.String("text-rule")) {
	case "sniff":
		pol.Text = ladingingest.TextRuleSniff
	case "never":
		pol.Text = ladingingest.TextRuleNever
	default:
		return eb.Build().Str("text-rule", c.String("text-rule")).Errorf("--text-rule must be sniff or never")
	}
	switch strings.ToLower(c.String("profile")) {
	case "corpus":
		pol.Profile = ladingschema.ProfileCorpus
	case "fleet":
		pol.Profile = ladingschema.ProfileFleet
	default:
		return eb.Build().Str("profile", c.String("profile")).Errorf("--profile must be corpus or fleet")
	}

	client := chclient.New(chclient.ConfigFromEnv(), nil)
	err = client.Ping(c.Context)
	if err != nil {
		return eh.Errorf("ClickHouse not reachable: %w", err)
	}
	exec, err := storeexec.New(client, nil)
	if err != nil {
		return eh.Errorf("executor: %w", err)
	}
	if !c.Bool("no-provision") {
		err = lading.Provision(c.Context, exec, pol.Profile)
		if err != nil {
			return eh.Errorf("provision: %w", err)
		}
	}
	err = lading.Verify(c.Context, exec)
	if err != nil {
		return eh.Errorf("%w", err)
	}
	meta := ladingmeta.NewMetaStore(exec, nil, ladingmeta.MetaStoreConfig{})
	defer meta.Close()
	data := ladingdata.NewDataStore(exec, nil, ladingdata.DataStoreConfig{})
	defer data.Close()

	arg := c.Args().First()
	var src fs.FS
	if c.Bool("remote") {
		if !strings.Contains(arg, ":") {
			return eb.Build().Str("arg", arg).Errorf("--remote takes an rclone remote of the form remote:path; a bare path is a local directory, name it without --remote")
		}
		opts := []ladingremote.Option{}
		if filters := c.StringSlice("filter"); len(filters) > 0 {
			opts = append(opts, ladingremote.WithFilters(filters...))
		}
		var rem *ladingremote.Remote
		rem, err = ladingremote.Serve(c.Context, arg, opts...)
		if err != nil {
			return eh.Errorf("rclone serve: %w", err)
		}
		defer func() { _ = rem.Close() }()
		src = rem
	} else {
		var abs string
		abs, err = filepath.Abs(arg)
		if err != nil {
			return eh.Errorf("%w", err)
		}
		var st os.FileInfo
		st, err = os.Stat(abs)
		if err != nil {
			return eh.Errorf("%w", err)
		}
		if !st.IsDir() {
			return eb.Build().Str("path", abs).Errorf("not a directory")
		}
		src = os.DirFS(abs)
	}

	if name := c.String("name"); name != "" {
		policies := ladingpolicy.NewPolicyStore(exec, nil, ladingpolicy.PolicyStoreConfig{})
		defer policies.Close()
		err = ladingingest.RecordPolicy(c.Context, policies, mount, pol, name, c.String("store"))
		if err != nil {
			return eh.Errorf("record policy: %w", err)
		}
	}

	started := time.Now()
	res, err := ladingingest.Snapshot(c.Context, src, mount, pol, lading.Stores{Meta: meta, Data: data})
	if err != nil {
		return eh.Errorf("snapshot: %w", err)
	}
	_, err = fmt.Fprintf(c.App.Writer,
		"mount=0x%X snapshot=%s expires=%s entries=%d bytes=%d blocks=%d stored=%d referenced=%d skipped=%d errors=%d took=%s\n",
		mount.Value(), res.Snap.UTC().Format(time.RFC3339Nano), res.ExpiresAt.UTC().Format(time.RFC3339),
		res.Entries, res.Bytes, res.Blocks, res.Stored, res.Referenced, res.Skipped, res.Errors,
		time.Since(started).Round(time.Millisecond))
	return
}
