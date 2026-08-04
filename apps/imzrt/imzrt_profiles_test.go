package imzrt

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/adhocdata"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/windowhost"
	"github.com/stergiotis/boxer/public/observability/profiling/pprofarrow"
)

// TestProfileCapsAuthorize runs the manifest's cap declaration through the
// real bus enforcement (the adhocdemo precedent): a client minted from
// these caps must be able to Request the publish and open subjects, and an
// uncapped client must be refused — declaring a cap and having it
// authorize the call are two different things.
func TestProfileCapsAuthorize(t *testing.T) {
	bus := inprocbus.NewInst(zerolog.Nop())
	bus.SetRequestTimeout(2 * time.Second)

	subjects := []string{adhocdata.SubjectPublish, windowhost.OpenSubject}
	for _, subject := range subjects {
		replier := bus.NewClient(app.AppIdT("test.replier."+subject), []app.SubjectFilter{
			{Pattern: subject, Direction: app.CapDirectionSub},
			{Pattern: inprocbus.InboxPrefix + ">", Direction: app.CapDirectionPub},
		})
		unsub, err := replier.Subscribe(subject, func(msg *app.Msg) {
			_ = replier.Publish(msg.Reply, []byte("ok"))
		})
		require.NoError(t, err)
		t.Cleanup(unsub)
	}

	caller := bus.NewClient(manifest.Id, manifest.Caps)
	for _, subject := range subjects {
		reply, err := caller.Request(subject, []byte("payload"))
		require.NoError(t, err, subject)
		assert.Equal(t, []byte("ok"), reply, subject)
	}

	uncapped := bus.NewClient("test.uncapped", nil)
	for _, subject := range subjects {
		_, err := uncapped.Request(subject, []byte("payload"))
		assert.ErrorIs(t, err, inprocbus.ErrPermissionViolation, subject)
	}
}

// TestInstantCapturesConvert captures the real instantaneous profiles of
// this test process and requires the converter to accept them — and to
// infer the kind WITHOUT the hint, pinning that the capture step produces
// what the alias claims.
func TestInstantCapturesConvert(t *testing.T) {
	for _, spec := range profileKinds {
		if spec.key == "cpu" {
			continue
		}
		raw, err := spec.capture(context.Background(), func(uint64, uint64, string) {})
		require.NoError(t, err, spec.key)
		require.NotEmpty(t, raw, spec.key)

		res, err := pprofarrow.Convert(bytes.NewReader(raw))
		require.NoError(t, err, spec.key)
		assert.Equal(t, spec.key, res.Kind, spec.key)
		if spec.key == "goroutine" {
			assert.Positive(t, res.Rows, "a live process has goroutines")
		}
	}
}

// TestCPUCaptureShortWindow runs a truncated CPU capture and requires a
// convertible cpu-kind profile (row count is load-dependent and not
// asserted).
func TestCPUCaptureShortWindow(t *testing.T) {
	raw, err := captureCPU(200*time.Millisecond)(context.Background(), func(uint64, uint64, string) {})
	require.NoError(t, err)
	require.NotEmpty(t, raw)

	res, err := pprofarrow.Convert(bytes.NewReader(raw))
	require.NoError(t, err)
	assert.Equal(t, "cpu", res.Kind)
}

// TestCPUCaptureHonorsCancel pins that cancellation aborts the sampling
// window promptly instead of running it out.
func TestCPUCaptureHonorsCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	_, err := captureCPU(time.Hour)(ctx, func(uint64, uint64, string) {})
	require.ErrorIs(t, err, context.Canceled)
	assert.Less(t, time.Since(start), 5*time.Second)
}

// TestProfileSeedSqlShape pins the explore buffer against the icicle panel's
// folded contract: a list-typed `stack` and each stack's OWN `value`. The
// panel resolves that from the Arrow schema, so a projection that renamed or
// rolled up either column opens a window that runs and draws nothing.
func TestProfileSeedSqlShape(t *testing.T) {
	for _, spec := range profileKinds {
		sql := profileSeedSql("h_abc123", spec)
		assert.Contains(t, sql, "keelson('h_abc123')", spec.key)
		assert.Contains(t, sql, "SELECT stack, ", spec.key)
		assert.Contains(t, sql, " AS value,", spec.key)
		assert.Contains(t, sql, "'"+spec.unit+"' AS unit", spec.key)
		// Rolling paths into their prefixes is what the panel does; doing it
		// in SQL would fold the stacks away before it ever saw them.
		assert.NotContains(t, sql, "GROUP BY", spec.key)
		// grammar1 parses a single statement; a stray semicolon would fail
		// the whole buffer.
		assert.False(t, strings.Contains(sql, ";"), spec.key)
		// `value / d AS value` is a cyclic alias in ClickHouse — the rescale
		// has to read from an inner name.
		assert.NotContains(t, sql, "value / ", spec.key)
	}

	// The rescale is per kind, and it reaches the buffer as a plain integer:
	// exponent form parses, but the buffer is something a reader edits.
	assert.Contains(t, profileSeedSql("h", profileKinds[0]), "v / 1000000 AS value")
	assert.Contains(t, profileSeedSql("h", profileKinds[3]), "SELECT stack, v AS value",
		"a count needs no rescale")
}
