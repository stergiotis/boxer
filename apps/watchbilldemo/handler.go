package watchbilldemo

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/task"
	"github.com/stergiotis/boxer/public/keelson/runtime/watchbill/watchbillstore"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// KindSleep is the demo's job kind: sleep for the duration the subject
// names, reporting progress, and fail at the end when asked to.
const KindSleep = "demo.sleep"

// The subject is the whole description of the work, the ADR-0223 §SD1
// rule with no table to point at: a Go duration, and the word `fail` after
// it to make the attempt end in error. "3s" sleeps three seconds; "3s fail"
// sleeps three seconds and fails, so a retry policy has something to do.
const subjectFail = "fail"

// reportTick is how often the handler reports progress; the task's own
// humanized-change gate throttles what reaches the bus.
const reportTick = 50 * time.Millisecond

// progressTotal is the synthetic item count the progress bar counts to.
const progressTotal = uint64(100)

var errAskedToFail = errors.New("watchbilldemo: the subject asked for a failure")

// sleepHandler is the [watchbill.HandlerI] for [KindSleep].
type sleepHandler struct{}

func (sleepHandler) Kind() (kind string) { return KindSleep }

// RunE sleeps the subject's duration, reporting progress on the task, and
// returns as soon as ctx ends — the cancel or the timeout. A subject it
// cannot read fails the attempt, so a typo is visible on the row.
func (sleepHandler) RunE(ctx context.Context, job watchbillstore.Job, h task.HandleI) (err error) {
	d, fail, err := parseSubject(job.Subject)
	if err != nil {
		return
	}
	started := time.Now()
	ticker := time.NewTicker(reportTick)
	defer ticker.Stop()
	for {
		elapsed := time.Since(started)
		if elapsed >= d {
			h.Report(task.ProgressReport{Current: progressTotal, Total: progressTotal, Unit: task.UnitItems})
			if fail {
				return errAskedToFail
			}
			return nil
		}
		h.Report(task.ProgressReport{Current: uint64(float64(progressTotal) * float64(elapsed) / float64(d)), Total: progressTotal, Unit: task.UnitItems})
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// parseSubject reads "<duration>[ fail]".
func parseSubject(subject string) (d time.Duration, fail bool, err error) {
	fields := strings.Fields(subject)
	if len(fields) == 0 || len(fields) > 2 {
		return 0, false, eb.Build().Str("subject", subject).Errorf("watchbilldemo: subject is \"<duration>\" or \"<duration> fail\"")
	}
	if d, err = time.ParseDuration(fields[0]); err != nil {
		return 0, false, eb.Build().Str("subject", subject).Errorf("watchbilldemo: subject duration: %w", err)
	}
	if d < 0 {
		return 0, false, eb.Build().Str("subject", subject).Errorf("watchbilldemo: subject duration is negative")
	}
	if len(fields) == 2 {
		if fields[1] != subjectFail {
			return 0, false, eb.Build().Str("subject", subject).Errorf("watchbilldemo: the second word is %q or nothing", subjectFail)
		}
		fail = true
	}
	return
}

// Subject composes the subject the handler reads.
func Subject(d time.Duration, fail bool) (subject string) {
	subject = d.String()
	if fail {
		subject += " " + subjectFail
	}
	return
}
