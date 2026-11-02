package runinfo

import (
	"strconv"
	"unicode"
	"unicode/utf8"

	"github.com/stergiotis/boxer/public/config/env"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// The benchmark take a run belongs to, which a window host writes as its run
// row's driver stamp (ADR-0344 §SD5, the update of 2026-11-02). A child
// process inherits the variables with the environment, as it inherits the
// run id.
var (
	RunDriverJob = env.NewString(env.Spec{
		Name:        "BOXER_RUN_DRIVER_JOB",
		Description: "the benchmark take a window host runs, named suite/task/take; set, the host's run row names driver bench and this job",
		Category:    env.CategoryE("runinfo"),
	})
	RunDriverAttempt = env.NewInt(env.Spec{
		Name:        "BOXER_RUN_DRIVER_ATTEMPT",
		Description: "the attempt of the benchmark take BOXER_RUN_DRIVER_JOB names, a uint32, written as the run row's driver attempt",
		Category:    env.CategoryE("runinfo"),
	})
	RunDriverJobKind = env.NewString(env.Spec{
		Name:        "BOXER_RUN_DRIVER_JOB_KIND",
		Description: "the suite version of the benchmark take BOXER_RUN_DRIVER_JOB names, written as the run row's driver job kind",
		Category:    env.CategoryE("runinfo"),
	})
)

// MaxBenchJobBytes and MaxBenchKindBytes bound a take's job and kind.
const (
	MaxBenchJobBytes  = 256
	MaxBenchKindBytes = 128
)

// BenchTake is the benchmark take a run belongs to: the take as
// <suite>/<task>/<take>, its attempt and the suite's version. The zero value
// is a run that belongs to none.
type BenchTake struct {
	Job     string
	Attempt uint32
	Kind    string
}

// IsZero says the run belongs to no take.
func (inst BenchTake) IsZero() bool { return inst.Job == "" }

// BenchTakeFromEnv is the take the environment names, zero when
// BOXER_RUN_DRIVER_JOB is unset. An attempt or a kind without a job, an
// attempt that is not a uint32, and a value that is too long, is not UTF-8
// or holds a character that does not print are errors.
func BenchTakeFromEnv() (take BenchTake, err error) {
	job := RunDriverJob.Get()
	kind := RunDriverJobKind.Get()
	rawAttempt, attemptSet := RunDriverAttempt.Lookup()
	if job == "" {
		if kind != "" || attemptSet {
			err = eh.Errorf("BOXER_RUN_DRIVER_ATTEMPT and BOXER_RUN_DRIVER_JOB_KIND need BOXER_RUN_DRIVER_JOB")
		}
		return
	}
	if err = checkBenchField(RunDriverJob.Spec().Name, job, MaxBenchJobBytes); err != nil {
		return
	}
	if err = checkBenchField(RunDriverJobKind.Spec().Name, kind, MaxBenchKindBytes); err != nil {
		return
	}
	if attemptSet {
		var a uint64
		if a, err = strconv.ParseUint(rawAttempt, 10, 32); err != nil {
			err = eb.Build().Str("value", rawAttempt).Errorf("BOXER_RUN_DRIVER_ATTEMPT is not an attempt number: %w", err)
			return
		}
		take.Attempt = uint32(a)
	}
	take.Job, take.Kind = job, kind
	return
}

// checkBenchField refuses a value longer than maxBytes, one that is not
// UTF-8, and one holding a character that does not print.
func checkBenchField(name string, value string, maxBytes int) (err error) {
	switch {
	case len(value) > maxBytes:
		return eb.Build().Str("variable", name).Int("bytes", len(value)).Int("max", maxBytes).Errorf("the benchmark take's value is too long")
	case !utf8.ValidString(value):
		return eb.Build().Str("variable", name).Errorf("the benchmark take's value is not UTF-8")
	}
	for _, r := range value {
		if !unicode.IsPrint(r) {
			return eb.Build().Str("variable", name).Int32("rune", r).Errorf("the benchmark take's value holds a character that does not print")
		}
	}
	return
}
