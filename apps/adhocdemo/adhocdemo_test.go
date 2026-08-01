package adhocdemo

import (
	"bytes"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/apps/sqlapplet"
	"github.com/stergiotis/boxer/public/keelson/runtime/adhocdata"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/clipboardbroker"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/windowhost"
)

func TestItemsDocParses(t *testing.T) {
	def, err := sqlapplet.ParseDocSource(string(ManifestId), "items.md", []byte(itemsDoc))
	require.NoError(t, err)
	require.NotNil(t, def)
	assert.Equal(t, "items", def.Slug)
	assert.Equal(t, sqlapplet.EndpointIntrospection, def.Endpoint)
	assert.Equal(t, []string{datasetAlias}, def.Datasets)
	assert.Contains(t, def.SQL, "keelson('items')")
}

func TestSeriesEncodes(t *testing.T) {
	inst := &App{log: zerolog.Nop()}
	b0 := inst.series(0)
	require.NotEmpty(t, b0)

	rdr, err := ipc.NewReader(bytes.NewReader(b0))
	require.NoError(t, err)
	defer rdr.Release()
	require.Len(t, rdr.Schema().Fields(), 2)
	var rows int64
	for rdr.Next() {
		rows += rdr.RecordBatch().NumRows()
	}
	assert.Equal(t, int64(24), rows)

	// Each generation differs, so Regenerate is visible.
	assert.NotEqual(t, b0, inst.series(1))
}

// TestManifestCapsCoverEmbeddedApplet pins the cap contract: the embedded
// applet's capabilities ride this manifest (ADR-0132 §SD8), so the two
// escape hatches the minimal toolbar renders must be declared here or the
// buttons are dead — the Definition drawer's Copy a silent no-op, Open in
// Playground a permission refusal.
func TestManifestCapsCoverEmbeddedApplet(t *testing.T) {
	patterns := make(map[string]app.CapDirectionE, len(manifest.Caps))
	for _, cap := range manifest.Caps {
		patterns[cap.Pattern] = cap.Direction
		assert.NotEmpty(t, cap.Reason, cap.Pattern)
	}

	for _, want := range []string{
		adhocdata.SubjectPublish,
		adhocdata.SubjectRetract,
		clipboardbroker.SubjectWrite,
		windowhost.OpenSubject,
	} {
		dir, ok := patterns[want]
		assert.True(t, ok, want)
		assert.Equal(t, app.CapDirectionPub, dir, want)
	}
}

// TestManifestCapsAuthorizeEscapeHatches runs the declaration through the
// real enforcement: a bus client minted from this manifest's caps must be
// able to Request both escape-hatch subjects. Declaring a cap and having
// it authorize the call are two different things — Client.Request gates on
// canPublish — and the failure this pins was a live one.
func TestManifestCapsAuthorizeEscapeHatches(t *testing.T) {
	bus := inprocbus.NewInst(zerolog.Nop())
	bus.SetRequestTimeout(2 * time.Second)

	// A replier per subject, so an authorized request completes rather
	// than timing out; repliers need the inbox Pub cap to answer.
	for _, subject := range []string{clipboardbroker.SubjectWrite, windowhost.OpenSubject} {
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

	caller := bus.NewClient(ManifestId, manifest.Caps)
	for _, subject := range []string{clipboardbroker.SubjectWrite, windowhost.OpenSubject} {
		reply, err := caller.Request(subject, []byte("payload"))
		require.NoError(t, err, subject)
		assert.Equal(t, []byte("ok"), reply, subject)
	}

	// The inverse, so the assertion above means something: without the
	// caps the same requests are refused — the pre-fix behaviour.
	uncapped := bus.NewClient("test.uncapped", nil)
	for _, subject := range []string{clipboardbroker.SubjectWrite, windowhost.OpenSubject} {
		_, err := uncapped.Request(subject, []byte("payload"))
		assert.ErrorIs(t, err, inprocbus.ErrPermissionViolation, subject)
	}
}
