package claudemine

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/keelson/data/chlocalpool"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/urfave/cli/v2"
)

func rootFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{Name: "projects-dir", Usage: "Claude Code projects directory (default: ~/.claude/projects)"},
		&cli.StringFlag{Name: "repo-parent", Usage: "directory whose immediate children are treated as repositories (default: parent of the working directory)"},
		&cli.StringSliceFlag{Name: "repo", Usage: "explicit repo root, name=/abs/path (repeatable); takes precedence over --repo-parent"},
	}
}

// NewCliCommand wires the `boxer claudemine` command and its subcommands.
func NewCliCommand() *cli.Command {
	return &cli.Command{
		Name:  "claudemine",
		Usage: "mine Claude Code session transcripts into a ClickHouse-queryable Arrow event table (tokens, prompts, files, commits per repo)",
		Subcommands: []*cli.Command{
			{
				Name:   "build",
				Usage:  "parse every session transcript and emit claude_events.arrow",
				Flags:  append(rootFlags(), &cli.StringFlag{Name: "out", Value: ".claudeminecache", Usage: "output directory for the Arrow file"}),
				Action: actionBuild,
			},
			{
				Name:   "overview",
				Usage:  "print the canned spend/usage/reference report via clickhouse-local (plain-text board if it is absent)",
				Flags:  rootFlags(),
				Action: actionOverview,
			},
			{
				Name:      "query",
				Usage:     "run an arbitrary SQL query against the `events` table",
				ArgsUsage: "<SQL>",
				Flags:     append(rootFlags(), &cli.StringFlag{Name: "format", Value: "PrettyCompact", Usage: "clickhouse-local --output-format"}),
				Action:    actionQuery,
			},
		},
	}
}

func resolveProjectsDir(c *cli.Context) (string, error) {
	if d := c.String("projects-dir"); d != "" {
		return d, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", eh.Errorf("unable to resolve home dir (pass --projects-dir): %w", err)
	}
	return filepath.Join(home, ".claude", "projects"), nil
}

func resolveClassifier(c *cli.Context) (*repoClassifier, error) {
	repoParent := c.String("repo-parent")
	if repoParent == "" {
		wd, err := os.Getwd()
		if err != nil {
			return nil, eh.Errorf("unable to resolve working dir (pass --repo-parent): %w", err)
		}
		repoParent = filepath.Dir(wd)
	}
	explicit := map[string]string{}
	for _, spec := range c.StringSlice("repo") {
		name, path, ok := strings.Cut(spec, "=")
		if !ok || name == "" || path == "" {
			return nil, eh.Errorf("bad --repo %q, want name=/abs/path", spec)
		}
		explicit[path] = name
	}
	var claudeDirs []string
	if home, err := os.UserHomeDir(); err == nil {
		claudeDirs = append(claudeDirs, filepath.Join(home, ".claude"))
	}
	return newClassifier(repoParent, explicit, claudeDirs), nil
}

// buildArtifacts parses the corpus and emits the Arrow file into outDir.
func buildArtifacts(c *cli.Context, outDir string) (rows []eventRow, arrowPath string, err error) {
	projectsDir, err := resolveProjectsDir(c)
	if err != nil {
		return nil, "", err
	}
	cl, err := resolveClassifier(c)
	if err != nil {
		return nil, "", err
	}
	if rows, err = ParseCorpus(projectsDir, cl); err != nil {
		return nil, "", err
	}
	if err = os.MkdirAll(outDir, 0o755); err != nil {
		return nil, "", eh.Errorf("unable to create out dir %q: %w", outDir, err)
	}
	arrowPath = filepath.Join(outDir, eventsArrowName)
	if err = WriteEventsArrow(arrowPath, rows); err != nil {
		return nil, "", err
	}
	return rows, arrowPath, nil
}

func countByKind(rows []eventRow) map[string]int {
	m := map[string]int{}
	for i := range rows {
		m[rows[i].Kind]++
	}
	return m
}

func actionBuild(c *cli.Context) error {
	outDir := c.String("out")
	if !filepath.IsAbs(outDir) {
		if wd, err := os.Getwd(); err == nil {
			outDir = filepath.Join(wd, outDir)
		}
	}
	rows, arrowPath, err := buildArtifacts(c, outDir)
	if err != nil {
		return err
	}
	k := countByKind(rows)
	log.Info().Int("rows", len(rows)).Int("sessions", k["session"]).
		Int("user_inputs", k["user_input"]).Int("assistant", k["assistant"]).
		Int("file_reads", k["file_read"]).Int("file_writes", k["file_write"]).
		Int("file_edits", k["file_edit"]).Int("commits", k["commit"]).
		Str("arrow", arrowPath).Msg("emitted Claude session event table")
	return nil
}

func actionOverview(c *cli.Context) error {
	tmp, err := os.MkdirTemp("", "boxer-claudemine-")
	if err != nil {
		return eh.Errorf("unable to create temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	rows, arrowPath, err := buildArtifacts(c, tmp)
	if err != nil {
		return err
	}
	ok, err := RunOverview(arrowPath, os.Stdout)
	if err != nil {
		return err
	}
	if !ok {
		log.Warn().Str("lookedAt", chlocalpool.DefaultBinaryPath).
			Msg("clickhouse-local not found; printing the plain-text board instead")
		return RenderSummaryASCII(rows, os.Stdout)
	}
	return nil
}

func actionQuery(c *cli.Context) error {
	sql := c.Args().First()
	if sql == "" {
		return eh.Errorf("provide a SQL query as the argument, e.g. boxer claudemine query \"SELECT project_repo, sum(output_tokens) FROM events WHERE kind='assistant' GROUP BY project_repo ORDER BY 2 DESC\"")
	}
	tmp, err := os.MkdirTemp("", "boxer-claudemine-")
	if err != nil {
		return eh.Errorf("unable to create temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	_, arrowPath, err := buildArtifacts(c, tmp)
	if err != nil {
		return err
	}
	ok, err := RunQuery(arrowPath, sql, c.String("format"), os.Stdout)
	if err != nil {
		return err
	}
	if !ok {
		return eh.Errorf("clickhouse-local not found (looked at %s and $PATH); install it or run `boxer claudemine build` and query the Arrow file yourself", chlocalpool.DefaultBinaryPath)
	}
	return nil
}
