// Package watchbill is the `watchbill` command group (ADR-0223 §SD8): the
// headless way onto the job table, against the ClickHouse server the
// CLICKHOUSE_* variables name. `run` is the worker a machine with no window
// host stands; the rest write or read rows.
//
// A worker started here has no facts store to read heartbeats from, so it
// sweeps nothing: abandoned claims are the host's worker's to notice. It
// runs the handlers this binary links, which is what makes a consumer's
// kind runnable from a shell: link the package, register at init.
package watchbill

import (
	"context"
	"fmt"
	"os/signal"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/urfave/cli/v2"

	"github.com/stergiotis/boxer/public/keelson/data/chclient"
	"github.com/stergiotis/boxer/public/keelson/data/storeexec"
	"github.com/stergiotis/boxer/public/keelson/runtime/runid"
	"github.com/stergiotis/boxer/public/keelson/runtime/watchbill"
	"github.com/stergiotis/boxer/public/keelson/runtime/watchbill/watchbillstore"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/storage/recordstore"
)

// NewCliCommand returns the `watchbill` group.
func NewCliCommand() *cli.Command {
	database := &cli.StringFlag{
		Name:  "database",
		Value: watchbillstore.DatabaseName,
		Usage: "database the job tables live in (the layout's)",
	}
	return &cli.Command{
		Name:  "watchbill",
		Usage: "durable work as job rows: run a worker, add, list, cancel and retry jobs (ADR-0223)",
		Flags: []cli.Flag{database},
		Subcommands: []*cli.Command{
			newRunCommand(),
			newAddCommand(),
			newListCommand(),
			newCancelCommand(),
			newRetryCommand(),
		},
	}
}

// open connects to the server and provisions the tables in the layout.
func open(c *cli.Context) (store *watchbill.SqlStore, err error) {
	client := chclient.New(chclient.ConfigFromEnv(), nil)
	exec, err := storeexec.New(client, nil)
	if err != nil {
		return nil, eh.Errorf("executor: %w", err)
	}
	layout := watchbillstore.Layout{Database: c.String("database")}
	if err = watchbillstore.ProvisionIn(c.Context, exec, layout); err != nil {
		return nil, eh.Errorf("provision: %w", err)
	}
	store = watchbill.NewSqlStore(exec, layout)
	return
}

func newRunCommand() *cli.Command {
	return &cli.Command{
		Name:  "run",
		Usage: "stand a watch: drain the queue with this binary's handlers until interrupted",
		Flags: []cli.Flag{
			&cli.IntFlag{Name: "max-workers", Value: watchbill.DefaultMaxWorkers, Usage: "runs in flight per kind"},
			&cli.DurationFlag{Name: "poll", Usage: "queue read interval (default: $KEELSON_WATCHBILL_POLL)"},
		},
		Action: func(c *cli.Context) (err error) {
			store, err := open(c)
			if err != nil {
				return
			}
			defer store.Close()
			kinds := watchbill.DefaultRegistry.Kinds()
			if len(kinds) == 0 {
				return eh.Errorf("no handler is registered in this binary; there is nothing to run")
			}
			worker, err := watchbill.New(watchbill.Config{
				Store: store, RunId: runid.Mint("watchbill", "cli"), Log: log.Logger,
				MaxWorkers: c.Int("max-workers"), Poll: c.Duration("poll"),
			})
			if err != nil {
				return
			}
			ctx, stop := signal.NotifyContext(c.Context, syscall.SIGINT, syscall.SIGTERM)
			defer stop()
			if err = worker.Start(ctx); err != nil {
				return
			}
			log.Info().Strs("kinds", kinds).Msg("watchbill: worker started")
			<-ctx.Done()
			worker.Stop()
			return nil
		},
	}
}

func newAddCommand() *cli.Command {
	return &cli.Command{
		Name:      "add",
		Usage:     "enqueue one job",
		ArgsUsage: "KIND [SUBJECT]",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "queue", Value: "default"},
			&cli.UintFlag{Name: "priority", Value: 0, Usage: "lower runs first"},
			&cli.UintFlag{Name: "max-attempts", Value: 1},
			&cli.StringFlag{Name: "backoff", Value: watchbillstore.BackoffNone, Usage: "none | linear | exponential"},
			&cli.DurationFlag{Name: "backoff-base", Value: 10 * time.Second},
			&cli.DurationFlag{Name: "timeout", Usage: "per attempt; zero is none"},
			&cli.DurationFlag{Name: "after", Usage: "due this long from now; zero is now"},
		},
		Action: func(c *cli.Context) (err error) {
			if c.NArg() < 1 {
				return eh.Errorf("a kind is required")
			}
			store, err := open(c)
			if err != nil {
				return
			}
			defer store.Close()
			req := watchbill.Request{
				Kind: c.Args().Get(0), Subject: c.Args().Get(1), Queue: c.String("queue"),
				Priority: uint32(c.Uint("priority")), MaxAttempts: uint32(c.Uint("max-attempts")),
				Backoff: c.String("backoff"), BackoffBase: c.Duration("backoff-base"), Timeout: c.Duration("timeout"),
				RequesterRun: runid.Mint("watchbill", "cli"),
			}
			if d := c.Duration("after"); d > 0 {
				req.RunAfter = time.Now().Add(d)
			}
			id, err := watchbill.Enqueue(c.Context, store, req)
			if err != nil {
				return
			}
			_, err = fmt.Fprintln(c.App.Writer, id)
			return
		},
	}
}

func newListCommand() *cli.Command {
	return &cli.Command{
		Name:  "list",
		Usage: "print the job rows, one per line",
		Flags: []cli.Flag{&cli.IntFlag{Name: "limit", Value: 200}},
		Action: func(c *cli.Context) (err error) {
			store, err := open(c)
			if err != nil {
				return
			}
			defer store.Close()
			jobs, err := store.ListJobs(c.Context, c.Int("limit"))
			if err != nil {
				return
			}
			tw := tabwriter.NewWriter(c.App.Writer, 0, 0, 2, ' ', 0)
			if _, err = fmt.Fprintln(tw, "id\tkind\tstate\tattempt\trun_after\tworker_run\tsubject\tlast_error"); err != nil {
				return
			}
			for _, j := range jobs {
				if _, err = fmt.Fprintf(tw, "%s\t%s\t%s\t%d/%d\t%s\t%s\t%s\t%s\n",
					j.ID, j.Kind, j.State, j.Attempt, j.MaxAttempts, j.RunAfter.UTC().Format(time.RFC3339), j.WorkerRun, j.Subject, j.LastError); err != nil {
					return
				}
			}
			if err = tw.Flush(); err != nil {
				return
			}
			_, err = fmt.Fprintf(c.App.Writer, "\n%d job(s)\n", len(jobs))
			return
		},
	}
}

func newCancelCommand() *cli.Command {
	return &cli.Command{
		Name:      "cancel",
		Usage:     "ask for a job to stop: a queued one is cancelled now, a running one by its worker within a poll",
		ArgsUsage: "ID",
		Action:    verb("cancel", watchbill.RequestCancel),
	}
}

func newRetryCommand() *cli.Command {
	return &cli.Command{
		Name:      "retry",
		Usage:     "put a finished job back in the queue with its attempts reset",
		ArgsUsage: "ID",
		Action:    verb("retry", watchbill.Retry),
	}
}

func verb(name string, fn func(context.Context, watchbill.StoreI, string, string, time.Time) (bool, error)) cli.ActionFunc {
	return func(c *cli.Context) (err error) {
		if c.NArg() != 1 {
			return eh.Errorf("exactly one job id")
		}
		store, err := open(c)
		if err != nil {
			return
		}
		defer store.Close()
		ok, err := fn(c.Context, store, c.Args().Get(0), runid.Mint("watchbill", "cli"), time.Now())
		if err != nil {
			return
		}
		if !ok {
			return eh.Errorf("the job is not in a state this verb applies to")
		}
		_, err = fmt.Fprintf(c.App.Writer, "%s: %s\n", name, c.Args().Get(0))
		return
	}
}

var _ recordstore.ExecutorI = (*storeexec.Executor)(nil)
