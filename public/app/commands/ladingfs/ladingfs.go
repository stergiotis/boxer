// Package ladingfs is the CLI in front of the lading store's SFTP head
// (ADR-0198 §SD9).
//
// One subcommand, and it speaks SFTP on stdin/stdout:
//
//	rclone mount ':sftp,ssh="boxer fs sftp-stdio --mount <id>",shell_type=unix:/<mount>/latest' /mnt/x
//
// rclone's `sftp` backend runs the `ssh=` command in place of ssh and talks to
// its pipes, so there is no socket, no port and no credential anywhere in
// this — possession of the pipe is the authorisation for the store, which is
// what makes it legal under the runtime's refusal to bind a non-loopback
// address before ADR-0082. Which of the store's mounts the pipe may see is
// the command's own `--mount` (repeatable) or `--all-mounts`; it refuses to
// start with neither.
//
// Beside it, `boxer fs snapshot` walks a tree in and `boxer fs purge` takes a
// mount out. Every verb takes `--database` for a store that lives outside the
// default database (ladingschema.Layout).
package ladingfs

import (
	"context"
	"fmt"
	"os"
	"strconv"

	cli "github.com/urfave/cli/v3"

	"github.com/stergiotis/boxer/public/fs/lading"
	"github.com/stergiotis/boxer/public/fs/lading/ladingschema"
	"github.com/stergiotis/boxer/public/fs/lading/ladingsftp"
	"github.com/stergiotis/boxer/public/fs/lading/ladingsql"
	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stergiotis/boxer/public/keelson/data/chclient"
	"github.com/stergiotis/boxer/public/keelson/data/storeexec"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/storage/recordstore"
)

// NewCliCommand is the `fs` command group.
func NewCliCommand() *cli.Command {
	return &cli.Command{
		Name:  "fs",
		Usage: "the lading snapshot store: take snapshots, serve them as a file system",
		Commands: []*cli.Command{
			newSftpStdioCommand(),
			newSnapshotCommand(),
			newPurgeCommand(),
		},
	}
}

// databaseFlag is the layout every verb takes: where the store's three tables
// live. Empty is the default database beside boxer.facts.
func databaseFlag() cli.Flag {
	return &cli.StringFlag{
		Name:  "database",
		Usage: "the ClickHouse database the store's tables live in; empty is the default beside boxer.facts",
	}
}

func layoutOf(cmd *cli.Command) ladingschema.Layout {
	return ladingschema.Layout{Database: cmd.String("database")}
}

// connect reaches the server the environment names and returns the executor
// every verb writes and reads through.
func connect(ctx context.Context) (exec recordstore.ExecutorI, err error) {
	client := chclient.New(chclient.ConfigFromEnv(), nil)
	err = client.Ping(ctx)
	if err != nil {
		return nil, eh.Errorf("ClickHouse not reachable: %w", err)
	}
	exec, err = storeexec.New(client, nil)
	if err != nil {
		return nil, eh.Errorf("executor: %w", err)
	}
	return
}

func newPurgeCommand() *cli.Command {
	return &cli.Command{
		Name:  "purge",
		Usage: "remove every row of one mount from the store",
		Description: "The per-mount purge of ADR-0198 §SD1: one lightweight DELETE per table on the mount id. " +
			"Retention is declarative, so the normal answer is to do nothing and let the rows expire; " +
			"this is for when a mount has to be gone sooner. The mount's policy record in boxer.facts is kept.",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "mount", Required: true,
				Usage: "the mount id to remove, decimal or 0x-prefixed hex"},
			databaseFlag(),
		},
		Action: runPurge,
	}
}

func runPurge(ctx context.Context, cmd *cli.Command) (err error) {
	mount, err := parseMount(cmd.String("mount"))
	if err != nil {
		return
	}
	exec, err := connect(ctx)
	if err != nil {
		return
	}
	layout := layoutOf(cmd)
	err = lading.VerifyIn(ctx, exec, layout)
	if err != nil {
		return eh.Errorf("%w", err)
	}
	err = lading.PurgeIn(ctx, exec, layout, mount)
	if err != nil {
		return
	}
	_, err = fmt.Fprintf(cmd.Root().Writer, "purged mount=0x%X database=%s\n", mount.Value(), layout.DatabaseName())
	return
}

func newSftpStdioCommand() *cli.Command {
	return &cli.Command{
		Name:  "sftp-stdio",
		Usage: "speak SFTP on stdin/stdout, serving snapshots read-only",
		Description: "Reads a lading store and serves it as /<mount>/<snapshot>/<path>, " +
			"with /<mount>/latest a symlink to the newest complete snapshot. " +
			"Every write is refused: the store has no update path. " +
			"Intended to be run BY rclone as its ssh command, not by hand.",
		Flags: []cli.Flag{
			&cli.StringSliceFlag{
				Name: "mount",
				Usage: "a mount id this session may read, decimal or 0x-prefixed hex; " +
					"repeatable. Required — a head that served every mount of a " +
					"store would make the pipe a grant over all of them at once",
			},
			&cli.BoolFlag{
				Name: "all-mounts",
				Usage: "serve every mount of the store, taking possession of the pipe " +
					"as the grant. Say it out loud rather than defaulting to it",
			},
			// rclone runs this command in place of ssh and appends ssh's
			// subsystem request, `-s sftp` (ADR-0198 §SD9). There is only one
			// subsystem here, so the flag is accepted and discarded. Without
			// it the invocation is refused for an undefined flag and the usage
			// message goes to stdout, which is the peer's stream: rclone reads
			// it as a version packet and reports `packet too long`, naming
			// neither the flag nor this command.
			&cli.StringFlag{
				Name:   "s",
				Hidden: true,
				Usage:  "ignored; ssh's subsystem request, which rclone passes as `-s sftp`",
			},
			databaseFlag(),
		},
		Action: runSftpStdio,
	}
}

func runSftpStdio(ctx context.Context, cmd *cli.Command) (err error) {
	vis, err := visibilityOf(ctx, cmd)
	if err != nil {
		return
	}

	exec, err := connect(ctx)
	if err != nil {
		return
	}
	layout := layoutOf(cmd)
	stores := lading.NewStores(exec, layout)
	defer stores.Meta.Close()
	defer stores.Data.Close()

	// Read-only from here on, so the tables are verified rather than
	// provisioned: this command must not be the thing that creates a store.
	err = lading.VerifyIn(ctx, exec, layout)
	if err != nil {
		return eh.Errorf("%w", err)
	}

	head, err := ladingsftp.New(ladingsftp.Config{
		Exec:       exec,
		Layout:     layout,
		Stores:     stores,
		Visibility: vis,
		Ctx:        ctx,
	})
	if err != nil {
		return
	}
	// stdin and stdout are one bidirectional stream to the peer. Nothing else
	// may write to stdout for the length of the session — a stray log line on
	// it is a protocol violation, which is why the runtime's logging goes to
	// stderr.
	return head.Serve(stdio{})
}

// visibilityOf turns the flags into the mount set this session may read.
func visibilityOf(ctx context.Context, cmd *cli.Command) (vis ladingsql.MountVisibilityI, err error) {
	if cmd.Bool("all-mounts") {
		if len(cmd.StringSlice("mount")) > 0 {
			err = eh.Errorf("--all-mounts and --mount are exclusive")
			return
		}
		return ladingsql.VisibleAll{}, nil
	}
	raw := cmd.StringSlice("mount")
	if len(raw) == 0 {
		err = eh.Errorf("no mounts named; pass --mount <id> (repeatable) or --all-mounts")
		return
	}
	set := make(ladingsql.VisibleSet, len(raw))
	for _, s := range raw {
		var id identifier.TaggedId
		id, err = parseMount(s)
		if err != nil {
			return
		}
		set[id] = struct{}{}
	}
	return set, nil
}

// parseMount reads a mount id written decimal or 0x-prefixed, the same two
// spellings the SQL macros accept.
func parseMount(s string) (mount identifier.TaggedId, err error) {
	base := 10
	text := s
	if len(text) > 2 && (text[:2] == "0x" || text[:2] == "0X") {
		text, base = text[2:], 16
	}
	var v uint64
	v, err = parseUint(text, base)
	if err != nil {
		err = eb.Build().Str("mount", s).Errorf("mount id must be a number, decimal or 0x-prefixed")
		return
	}
	mount = identifier.TaggedId(v)
	if !mount.IsValid() {
		err = eb.Build().Str("mount", s).Errorf("mount is not a valid tagged id")
	}
	return
}

// stdio is the peer's stream: stdin in, stdout out, and closing it closes
// nothing — the process exiting is what ends the session.
type stdio struct{}

func (stdio) Read(p []byte) (int, error)  { return os.Stdin.Read(p) }
func (stdio) Write(p []byte) (int, error) { return os.Stdout.Write(p) }
func (stdio) Close() error                { return nil }

// parseUint is strconv's, named locally so the error above can be the only one
// a caller sees.
func parseUint(s string, base int) (uint64, error) { return strconv.ParseUint(s, base, 64) }
