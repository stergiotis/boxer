// Package ladingfs is the CLI in front of the lading store's SFTP head
// (ADR-0198 §SD9).
//
// One subcommand, and it speaks SFTP on stdin/stdout:
//
//	rclone mount ':sftp,ssh="boxer fs sftp-stdio",shell_type=unix:/<mount>/latest' /mnt/x
//
// rclone's `sftp` backend runs the `ssh=` command in place of ssh and talks to
// its pipes, so there is no socket, no port and no credential anywhere in
// this — possession of the pipe is the authorisation, which is what makes it
// legal under the runtime's refusal to bind a non-loopback address before
// ADR-0082.
package ladingfs

import (
	"os"
	"strconv"

	cli "github.com/urfave/cli/v2"

	"github.com/stergiotis/boxer/public/fs/lading"
	"github.com/stergiotis/boxer/public/fs/lading/ladingdata"
	"github.com/stergiotis/boxer/public/fs/lading/ladingmeta"
	"github.com/stergiotis/boxer/public/fs/lading/ladingsftp"
	"github.com/stergiotis/boxer/public/fs/lading/ladingsql"
	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stergiotis/boxer/public/keelson/data/chclient"
	"github.com/stergiotis/boxer/public/keelson/data/storeexec"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// NewCliCommand is the `fs` command group.
func NewCliCommand() *cli.Command {
	return &cli.Command{
		Name:  "fs",
		Usage: "serve a lading snapshot store as a file system",
		Subcommands: []*cli.Command{
			newSftpStdioCommand(),
		},
	}
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
		},
		Action: runSftpStdio,
	}
}

func runSftpStdio(c *cli.Context) (err error) {
	vis, err := visibilityOf(c)
	if err != nil {
		return
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

	meta := ladingmeta.NewMetaStore(exec, nil, ladingmeta.MetaStoreConfig{})
	defer meta.Close()
	data := ladingdata.NewDataStore(exec, nil, ladingdata.DataStoreConfig{})
	defer data.Close()

	// Read-only from here on, so the tables are verified rather than
	// provisioned: this command must not be the thing that creates a store.
	err = lading.Verify(c.Context, exec)
	if err != nil {
		return eh.Errorf("%w", err)
	}

	head, err := ladingsftp.New(ladingsftp.Config{
		Exec:       exec,
		Stores:     lading.Stores{Meta: meta, Data: data},
		Visibility: vis,
		Ctx:        c.Context,
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
func visibilityOf(c *cli.Context) (vis ladingsql.MountVisibilityI, err error) {
	if c.Bool("all-mounts") {
		if len(c.StringSlice("mount")) > 0 {
			err = eh.Errorf("--all-mounts and --mount are exclusive")
			return
		}
		return ladingsql.VisibleAll{}, nil
	}
	raw := c.StringSlice("mount")
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
		err = eb.Build().Str("mount", s).Errorf("%q is not a valid tagged id", s)
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
