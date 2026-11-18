package markdown

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	cli "github.com/urfave/cli/v3"

	"github.com/stergiotis/boxer/public/keelson/data/chclient"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/markdown/mdnotebook"
)

// newNotebookCommand is `boxer markdown notebook`: the steps of a sealed
// notebook's life that need no window (ADR-0371). A notebook is written as
// text, by a person, a programme or an agent; `buffer` shows its records as
// the one statement to try in play, `collect` computes and stores them, and
// `check` says whether what is stored is what the text claims.
func newNotebookCommand() *cli.Command {
	return &cli.Command{
		Name:  "notebook",
		Usage: "sealed notebooks: collect a notebook's records, check one, or print its play buffer",
		Commands: []*cli.Command{
			{
				Name:      "collect",
				Usage:     "compute every record on the server, store the results beside the notebook and seal it",
				ArgsUsage: "<notebook.md>",
				Description: "Runs each record's statement once, the records before it as WITH queries, " +
					"and writes each result as a blob named by its content into <notebook>.blobs/. " +
					"The notebook's frontmatter is rewritten to list the blobs, and every figure " +
					"(a val: link) to read as its stored value. Presentation cells and prose are " +
					"left as they are. Blobs no longer listed are not removed. " +
					"Needs CLICKHOUSE_ENDPOINT (and the other CLICKHOUSE_* variables).",
				Flags: []cli.Flag{
					&cli.DurationFlag{Name: "timeout", Value: 10 * time.Minute,
						Usage: "bound on the whole run"},
				},
				Action: runNotebookCollect,
			},
			{
				Name:      "check",
				Usage:     "say whether a notebook is sealed, and answer its presentation cells",
				ArgsUsage: "<notebook.md>",
				Description: "Reads the blobs the frontmatter lists, checks each against its name, compares " +
					"every figure with its stored value and every record's statement with the one its " +
					"blob was computed from, and answers each presentation cell. Connects to nothing. " +
					"Exits non-zero when the notebook is not sealed or a cell has no answer.",
				Action: runNotebookCheck,
			},
			{
				Name:      "buffer",
				Usage:     "print the notebook's records as one statement, to try in play",
				ArgsUsage: "<notebook.md>",
				Action:    runNotebookBuffer,
			},
		},
	}
}

func readNotebook(cmd *cli.Command) (file string, nb *mdnotebook.Notebook, err error) {
	if cmd.NArg() != 1 {
		return "", nil, eh.Errorf("one notebook file is required")
	}
	file = cmd.Args().First()
	src, err := os.ReadFile(file)
	if err != nil {
		return "", nil, eb.Build().Str("path", file).Errorf("read: %w", err)
	}
	nb, err = mdnotebook.Read(src)
	if err != nil {
		return "", nil, eb.Build().Str("path", file).Errorf("read notebook: %w", err)
	}
	return file, nb, nil
}

func runNotebookBuffer(_ context.Context, cmd *cli.Command) (err error) {
	_, nb, err := readNotebook(cmd)
	if err != nil {
		return
	}
	sql, err := mdnotebook.Buffer(nb)
	if err != nil {
		return
	}
	_, err = fmt.Fprintln(cmd.Root().Writer, sql)
	return
}

func runNotebookCollect(_ context.Context, cmd *cli.Command) (err error) {
	file, nb, err := readNotebook(cmd)
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), cmd.Duration("timeout"))
	defer cancel()
	client := chclient.New(chclient.ConfigFromEnv(), nil)
	run := func(ctx context.Context, statement string) (stream []byte, rErr error) {
		body, rErr := client.Query(ctx, statement)
		if rErr != nil {
			return nil, rErr
		}
		defer func() { _ = body.Close() }()
		return io.ReadAll(body)
	}
	dir := filepath.Dir(file)
	blobDir := strings.TrimSuffix(filepath.Base(file), filepath.Ext(file)) + ".blobs"
	out, err := mdnotebook.Collect(ctx, nb, blobDir, run)
	if err != nil {
		return
	}
	// Blobs first: a notebook that lists a blob which is not there is not
	// sealed, and one whose blobs are there but unlisted is only untidy.
	paths := make([]string, 0, len(out.Blobs))
	for p := range out.Blobs {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		if err = writeFileAtomic(filepath.Join(dir, filepath.FromSlash(p)), out.Blobs[p]); err != nil {
			return
		}
	}
	if err = writeFileAtomic(file, out.Text); err != nil {
		return
	}
	w := cmd.Root().Writer
	for _, r := range out.Records {
		if _, err = fmt.Fprintf(w, "record %s\t%s\n", r.Name, r.Blob); err != nil {
			return
		}
	}
	_, err = fmt.Fprintf(w, "sealed\tdata %s\n", mdnotebook.PinOf(out.Records))
	return
}

// writeFileAtomic writes data beside path and renames it into place, so a
// reader never meets half a file. A file it replaces keeps its permissions,
// and one named through a symbolic link is replaced where it lies: a rename
// over the link would leave the notebook itself as it was.
func writeFileAtomic(path string, data []byte) (err error) {
	mode := os.FileMode(0o644)
	if resolved, rErr := filepath.EvalSymlinks(path); rErr == nil {
		path = resolved
		if info, sErr := os.Stat(path); sErr == nil {
			mode = info.Mode().Perm()
		}
	}
	if err = os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return eb.Build().Str("path", path).Errorf("create directory: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return eb.Build().Str("path", path).Errorf("create temporary file: %w", err)
	}
	defer func() {
		if err != nil {
			_ = os.Remove(tmp.Name())
		}
	}()
	if _, err = tmp.Write(data); err != nil {
		_ = tmp.Close()
		return eb.Build().Str("path", path).Errorf("write: %w", err)
	}
	if err = tmp.Close(); err != nil {
		return eb.Build().Str("path", path).Errorf("write: %w", err)
	}
	if err = os.Chmod(tmp.Name(), mode); err != nil {
		return eb.Build().Str("path", path).Errorf("write: %w", err)
	}
	if err = os.Rename(tmp.Name(), path); err != nil {
		return eb.Build().Str("path", path).Errorf("write: %w", err)
	}
	return nil
}

func runNotebookCheck(_ context.Context, cmd *cli.Command) (err error) {
	file, nb, err := readNotebook(cmd)
	if err != nil {
		return
	}
	ld := mdnotebook.Load(nb, os.DirFS(filepath.Dir(file)))
	defer ld.Release()
	w := cmd.Root().Writer
	line := func(format string, args ...any) {
		if err == nil {
			_, err = fmt.Fprintf(w, format, args...)
		}
	}
	unsealed := 0
	for _, p := range mdnotebook.Problems(nb, ld) {
		unsealed++
		if p.Cell < 0 {
			line("problem\t%s\n", p.Text)
		} else {
			line("problem\tcell %d: %s\n", p.Cell, p.Text)
		}
	}
	for _, name := range mdnotebook.StaleRecords(nb, ld) {
		unsealed++
		line("stale\trecord %s was edited since collection\n", name)
	}
	for _, d := range mdnotebook.DerivedStaleRecords(nb, ld) {
		unsealed++
		line("stale\trecord %s reads %s, which was edited since collection\n", d.Record, d.Edited)
	}
	unanswered := 0
	for i := range nb.Cells {
		c := &nb.Cells[i]
		if c.Kind != mdnotebook.CellKindPresentation {
			continue
		}
		res, eErr := mdnotebook.Evaluate(c.Body, ld.Records)
		if eErr != nil {
			unanswered++
			line("cell %d\tsql %s: %s\n", i, c.Renderer, eErr.Error())
			continue
		}
		line("cell %d\tsql %s: %d rows × %d columns\n", i, c.Renderer, res.Main.NumRows(), res.Main.NumCols())
		res.Release()
	}
	status := "sealed"
	if unsealed > 0 {
		status = "not sealed"
	}
	line("%s\tdata %s\n", status, mdnotebook.PinOf(nb.Records))
	if err != nil {
		return
	}
	if unsealed > 0 || unanswered > 0 {
		return eb.Build().Str("path", file).Int("notSealed", unsealed).Int("cellsWithoutAnswer", unanswered).Errorf("the notebook is not sealed, or a cell has no answer")
	}
	return
}
