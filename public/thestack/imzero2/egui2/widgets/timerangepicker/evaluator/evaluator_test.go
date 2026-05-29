//go:build llm_generated_opus47

package evaluator_test

import (
	"context"
	"os/exec"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/public/keelson/data/chlocalbroker"
	"github.com/stergiotis/boxer/public/keelson/data/chlocalpool"
	runtimeapp "github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/timerangepicker"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/timerangepicker/evaluator"
)

const testPoolName = "timerangepicker_test"

func skipIfNoClickHouseLocal(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("clickhouse-local"); err != nil {
		if _, fallbackErr := exec.LookPath("clickhouse"); fallbackErr != nil {
			t.Skipf("clickhouse-local not on PATH: %v", err)
		}
	}
}

// setupTestEvaluator stands up an in-proc bus + chlocalbroker.Service
// and returns an Evaluator bound to a test bus client with the
// timerangepicker pool cap. The broker (and its pool) is torn down on
// test cleanup. Skips when clickhouse-local is not on PATH.
func setupTestEvaluator(t *testing.T) (ev *evaluator.Evaluator) {
	t.Helper()
	skipIfNoClickHouseLocal(t)
	logger := zerolog.New(zerolog.NewTestWriter(t))
	bus := inprocbus.NewInst(logger)
	bus.SetRequestTimeout(15 * time.Second)

	poolCfg := chlocalpool.Config{
		BaseTmpDir:       t.TempDir(),
		MinIdle:          1,
		MaxConcurrent:    2,
		SpawnConcurrency: 1,
		SpawnTimeout:     5 * time.Second,
	}
	svc, err := chlocalbroker.NewService(bus, poolCfg, logger)
	if err != nil {
		t.Fatalf("chlocalbroker.NewService: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = svc.Stop(ctx)
	})

	caller := bus.NewClient("test.timerangepicker", []runtimeapp.SubjectFilter{
		{Pattern: chlocalbroker.SubjectExecPrefix + testPoolName, Direction: runtimeapp.CapDirectionPub, Reason: "test"},
	})

	ev, err = evaluator.NewEvaluator(caller, testPoolName)
	if err != nil {
		t.Fatalf("NewEvaluator: %v", err)
	}
	t.Cleanup(func() { _ = ev.Close() })
	return
}

func TestEvalLast24HoursAgainstFixedAnchor(t *testing.T) {
	ev := setupTestEvaluator(t)

	anchor := time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC)
	fromMs, toMs, err := ev.Eval(context.Background(), anchor, timerangepicker.TzIDUTC,
		"anchor_now - INTERVAL 24 HOUR", "anchor_now")
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	wantTo := anchor.UnixMilli()
	wantFrom := anchor.Add(-24 * time.Hour).UnixMilli()
	if toMs != wantTo {
		t.Errorf("toMs: want %d, got %d", wantTo, toMs)
	}
	if fromMs != wantFrom {
		t.Errorf("fromMs: want %d, got %d", wantFrom, fromMs)
	}
}

func TestEvalToStartOfDaySnaps(t *testing.T) {
	ev := setupTestEvaluator(t)

	anchor := time.Date(2026, 4, 27, 14, 30, 0, 0, time.UTC)
	fromMs, toMs, err := ev.Eval(context.Background(), anchor, timerangepicker.TzIDUTC,
		"toStartOfDay(anchor_now)", "anchor_now")
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	wantFrom := time.Date(2026, 4, 27, 0, 0, 0, 0, time.UTC).UnixMilli()
	if fromMs != wantFrom {
		t.Errorf("fromMs: want %d (start of day), got %d", wantFrom, fromMs)
	}
	if toMs != anchor.UnixMilli() {
		t.Errorf("toMs: want %d (anchor), got %d", anchor.UnixMilli(), toMs)
	}
}

func TestEvalRejectsInvalidExpression(t *testing.T) {
	ev := setupTestEvaluator(t)

	anchor := time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC)
	_, _, err := ev.Eval(context.Background(), anchor, timerangepicker.TzIDUTC,
		"this is not sql", "anchor_now")
	if err == nil {
		t.Error("expected error for invalid expression, got nil")
	}
}

func TestEvalTzShiftsAnchor(t *testing.T) {
	ev := setupTestEvaluator(t)

	tokyoID, err := timerangepicker.LookupTz("Asia/Tokyo")
	if err != nil {
		t.Fatalf("LookupTz Asia/Tokyo: %v", err)
	}
	anchor := time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC)

	// toStartOfDay in UTC for the 2026-04-27 12:00 UTC anchor yields
	// 2026-04-27 00:00 UTC. Same wall-clock anchor reinterpreted in
	// Tokyo (UTC+9) is 2026-04-27 21:00 Tokyo, whose toStartOfDay is
	// 2026-04-27 00:00 Tokyo = 2026-04-26 15:00 UTC. So Tokyo's
	// start-of-day epoch-ms is 9 hours earlier than UTC's.
	utcFromMs, _, err := ev.Eval(context.Background(), anchor, timerangepicker.TzIDUTC,
		"toStartOfDay(anchor_now)", "anchor_now")
	if err != nil {
		t.Fatalf("Eval UTC: %v", err)
	}
	tokyoFromMs, _, err := ev.Eval(context.Background(), anchor, tokyoID,
		"toStartOfDay(anchor_now)", "anchor_now")
	if err != nil {
		t.Fatalf("Eval Tokyo: %v", err)
	}
	if utcFromMs == tokyoFromMs {
		t.Errorf("expected toStartOfDay to differ between UTC and Tokyo for same anchor; both = %d", utcFromMs)
	}
	deltaHours := (utcFromMs - tokyoFromMs) / 3600000
	if deltaHours != 9 {
		t.Errorf("expected UTC start-of-day to be 9h later than Tokyo's; got delta %d hours", deltaHours)
	}
}

func TestNewEvaluatorRejectsNilBus(t *testing.T) {
	_, err := evaluator.NewEvaluator(nil, "any")
	if err == nil {
		t.Fatal("expected ErrEvaluatorUnavailable on nil bus, got nil")
	}
}

func TestNewEvaluatorRejectsEmptyPool(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))
	bus := inprocbus.NewInst(logger)
	caller := bus.NewClient("test.timerangepicker", nil)
	_, err := evaluator.NewEvaluator(caller, "")
	if err == nil {
		t.Fatal("expected error on empty poolName, got nil")
	}
}
