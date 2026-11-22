package attention

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"sync"
	"syscall"
	"time"

	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/public/config/env"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// RestartPendingFileEnv names the file a deployment lists pending restarts
// in. A host whose service is listed there runs an older program than the
// one installed, and asks the person when it may restart.
var RestartPendingFileEnv = env.NewString(env.Spec{
	Name:        "KEELSON_RESTART_PENDING_FILE",
	Default:     "",
	Description: "file in which whoever deploys this host lists the services whose restart is pending: a JSON object with `units`, each naming its `service`, the time the restart became pending (`since`) and, optionally, the `deadline` it is restarted at regardless; while the host finds itself listed it asks the person whether to restart now, and restarts by ending its process; unset = the host never asks",
	Category:    env.CategorySystem,
})

// RestartPendingServiceEnv is the name this host goes by in
// RestartPendingFileEnv's file.
var RestartPendingServiceEnv = env.NewString(env.Spec{
	Name:        "KEELSON_RESTART_PENDING_SERVICE",
	Default:     "",
	Description: "the service name this host is listed under in KEELSON_RESTART_PENDING_FILE; unset = the host never asks",
	Category:    env.CategorySystem,
})

// RestartSnoozeEnv is how long the host waits before it asks again.
var RestartSnoozeEnv = env.NewDuration(env.Spec{
	Name:        "BOXER_ATTENTION_RESTART_SNOOZE",
	Default:     "2h",
	Description: "how long the host waits before it asks again whether to restart, after the person chose later or skipped the question; with a deadline it asks again no later than half the time that is left",
	Category:    env.CategorySystem,
})

// restartAskApp is the sender the restart question is posted as.
const restartAskApp app.AppIdT = "keelson/restart"

// IsHostQuestion reports whether sender is the host asking in its own
// words, not an app asking for a model: a card then names no model.
func IsHostQuestion(sender app.AppIdT) (ok bool) {
	ok = sender == restartAskApp
	return
}

// restartAskPoll is how often a running RestartAsk looks at the file and at
// its question.
const restartAskPoll = 5 * time.Second

// PendingRestart is one service's entry in the pending restarts' file.
type PendingRestart struct {
	Service string `json:"service"`
	// Since is when the restart became pending.
	Since time.Time `json:"since"`
	// Deadline is when the service is restarted regardless; zero for none.
	Deadline time.Time `json:"deadline,omitzero"`
}

// ReadPendingRestart reads the pending restarts at path and returns the one
// of service; pending is false when the file is absent or does not list it.
func ReadPendingRestart(path string, service string) (p PendingRestart, pending bool, err error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		err = nil
		return
	}
	if err != nil {
		err = eb.Build().Str("path", path).Errorf("unable to read the pending restarts: %w", err)
		return
	}
	var doc struct {
		Units []PendingRestart `json:"units"`
	}
	if err = json.Unmarshal(b, &doc); err != nil {
		err = eb.Build().Str("path", path).Errorf("unable to read the pending restarts: %w", err)
		return
	}
	for _, u := range doc.Units {
		if u.Service == service {
			p, pending = u, true
			return
		}
	}
	return
}

// RestartAsk asks the person, through the queue, when a host that runs an
// older program than the one installed may restart. While a restart is
// pending it keeps one question open: "Restart now" restarts, "Later" and a
// skip ask again after the snooze. A restart that is no longer pending takes
// the question back. Create it with NewRestartAsk and call Tick regularly.
type RestartAsk struct {
	mu        sync.Mutex
	questions *Questions
	read      func() (p PendingRestart, pending bool, err error)
	restart   func()
	snooze    time.Duration
	log       zerolog.Logger

	// since identifies the pending restart the state below belongs to.
	since time.Time
	// key is the open question's, empty when none is open.
	key string
	// nextAsk is the earliest time the next question is posted.
	nextAsk time.Time
	// done is set once the person chose to restart.
	done bool
	// readFailed keeps a failing read from being logged every tick.
	readFailed bool
}

// NewRestartAsk asks through questions about the pending restart read
// answers, and calls restart once the person agrees.
func NewRestartAsk(questions *Questions, read func() (PendingRestart, bool, error), restart func(), snooze time.Duration, log zerolog.Logger) (inst *RestartAsk) {
	if snooze <= 0 {
		snooze = time.Hour
	}
	inst = &RestartAsk{questions: questions, read: read, restart: restart, snooze: snooze, log: log}
	return
}

// Tick looks at the pending restart and at the open question, and acts on
// what changed since the last call.
func (inst *RestartAsk) Tick(now time.Time) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if inst.done {
		return
	}
	p, pending, err := inst.read()
	if err != nil {
		if !inst.readFailed {
			inst.log.Warn().Err(err).Msg("attention: the pending restarts are unreadable; the person is not asked until they are")
			inst.readFailed = true
		}
		return
	}
	inst.readFailed = false
	if !pending || !p.Since.Equal(inst.since) {
		// Nothing pending any more, or another restart than the one asked
		// about: the open question is stale.
		inst.withdraw()
		inst.since, inst.nextAsk = time.Time{}, time.Time{}
		if !pending {
			return
		}
		inst.since = p.Since
	}
	if inst.key != "" {
		a := inst.questions.Status(restartAskApp, 0, inst.key)
		if a.Phase == PhaseWaiting {
			return
		}
		inst.key = ""
		if a.Phase == PhaseAnswered && len(a.Choices) == 1 && len(a.Choices[0]) == 1 && a.Choices[0][0] == 0 {
			inst.done = true
			inst.log.Info().Time("pendingSince", p.Since).Msg("attention: the person chose to restart now")
			inst.restart()
			return
		}
		inst.nextAsk = now.Add(snoozeFor(inst.snooze, p.Deadline, now))
		return
	}
	if now.Before(inst.nextAsk) || (!p.Deadline.IsZero() && !now.Before(p.Deadline)) {
		// Asked recently, or the deadline has come and whoever set it
		// restarts the host.
		return
	}
	key, reason := inst.questions.Post(restartAskApp, 0, restartQuestion(p), now)
	if key == "" {
		inst.log.Debug().Str("reason", reason).Msg("attention: the restart question was not admitted; asking again shortly")
		inst.nextAsk = now.Add(time.Minute)
		return
	}
	inst.key = key
}

// Close takes an open question back.
func (inst *RestartAsk) Close() {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.withdraw()
}

func (inst *RestartAsk) withdraw() {
	if inst.key != "" {
		inst.questions.Withdraw(restartAskApp, 0, inst.key)
		inst.key = ""
	}
}

// snoozeFor is how long to wait before asking again: the snooze, and with a
// deadline no longer than half the time left, at least a minute.
func snoozeFor(snooze time.Duration, deadline time.Time, now time.Time) (d time.Duration) {
	d = snooze
	if !deadline.IsZero() {
		if half := deadline.Sub(now) / 2; half < d {
			d = half
		}
	}
	if d < time.Minute {
		d = time.Minute
	}
	return
}

// restartQuestion is the question asked about p: restart now, or later.
func restartQuestion(p PendingRestart) (q Question) {
	text := "A newer version of this application is installed, and this one still runs the old. Restart now? Its windows close and open again."
	if !p.Deadline.IsZero() {
		text += " Without an answer it restarts on " + p.Deadline.Local().Format("Mon 2 Jan at 15:04") + "."
	}
	q = Question{
		Kind:   KindQuestion,
		Title:  "Update installed",
		Header: "Restart",
		Text:   text,
		Options: []QuestionOption{
			{Label: "Restart now", Description: "end this application; it starts again on the new version"},
			{Label: "Later", Description: "keep working; the question comes again"},
		},
	}
	return
}

// EndProcess ends this process the way its service manager does when it
// stops it, so the service manager starts the installed program in its
// place.
func EndProcess() {
	_ = syscall.Kill(os.Getpid(), syscall.SIGTERM)
}

// StartRestartAsk asks the person about this host's pending restart for as
// long as ctx lives, when RestartPendingFileEnv and RestartPendingServiceEnv
// say where it is listed; restart is what "Restart now" does. It returns
// the function that stops asking, nil when the environment names no file.
func StartRestartAsk(ctx context.Context, svc *Service, restart func(), log zerolog.Logger) (stop func()) {
	path, service := RestartPendingFileEnv.Get(), RestartPendingServiceEnv.Get()
	if path == "" || service == "" || svc == nil || svc.Questions == nil {
		return
	}
	ask := NewRestartAsk(svc.Questions, func() (PendingRestart, bool, error) {
		return ReadPendingRestart(path, service)
	}, restart, RestartSnoozeEnv.Get(), log)
	ctx, cancel := context.WithCancel(ctx)
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		ticker := time.NewTicker(restartAskPoll)
		defer ticker.Stop()
		for {
			ask.Tick(time.Now())
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	stop = func() {
		cancel()
		<-finished
		ask.Close()
	}
	return
}
