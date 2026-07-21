package sqlappletcreator

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/apps/sqlappletcreator/appletcreatecfg"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appletstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/fsbroker"
)

// fakeSaveBus captures the applet.store.save request the creator makes and
// answers with a canned reply or transport error.
type fakeSaveBus struct {
	mu         sync.Mutex
	gotSubject string
	gotPayload []byte
	reply      appletstore.SaveReply
	err        error
}

var _ app.BusI = (*fakeSaveBus)(nil)

func (f *fakeSaveBus) Publish(subject string, payload []byte) (err error) { return }
func (f *fakeSaveBus) Subscribe(subject string, handler app.MsgHandlerFunc) (unsubscribe func(), err error) {
	return
}
func (f *fakeSaveBus) Request(subject string, payload []byte) (reply []byte, err error) {
	f.mu.Lock()
	f.gotSubject = subject
	f.gotPayload = append([]byte(nil), payload...)
	f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	return appletstore.EncodeSaveReply(f.reply)
}

func TestMountSeedsFromLaunchConfig(t *testing.T) {
	raw, err := buscodec.Encode(appletcreatecfg.AppletCreate{
		Sql:      "SELECT 42",
		Endpoint: appletcreatecfg.EndpointIntrospection,
	})
	require.NoError(t, err)

	inst := &App{}
	mc := app.NewStaticMountContext(appletcreatecfg.AppId, zerolog.Nop(), nil, nil, nil)
	mc.SetLaunchConfig(raw)
	require.NoError(t, inst.Mount(mc))

	assert.Equal(t, "SELECT 42", inst.sql)
	assert.Equal(t, appletcreatecfg.EndpointIntrospection, inst.endpoint)
}

func TestMountPlainOpenLeavesBufferEmpty(t *testing.T) {
	inst := &App{}
	mc := app.NewStaticMountContext(appletcreatecfg.AppId, zerolog.Nop(), nil, nil, nil)
	require.NoError(t, inst.Mount(mc))
	assert.Empty(t, inst.sql)
	assert.Empty(t, inst.endpoint)
}

func TestMountRejectsGarbageConfig(t *testing.T) {
	// The host validated the kind at the boundary, so a decode failure here is
	// a real defect — a failed mount, never a silently empty buffer (§SD4).
	inst := &App{}
	mc := app.NewStaticMountContext(appletcreatecfg.AppId, zerolog.Nop(), nil, nil, nil)
	mc.SetLaunchConfig([]byte{0x01, 0x02, 0x03, 0x04})
	err := inst.Mount(mc)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "decode launch config")
}

func TestSubmitComposesAndSaves(t *testing.T) {
	bus := &fakeSaveBus{reply: appletstore.SaveReply{OK: true, Class: "read"}}
	inst := &App{
		bus:      bus,
		log:      zerolog.Nop(),
		sql:      "SELECT 1",
		slug:     "my-applet",
		title:    "My Applet",
		icon:     "🧩",
		endpoint: appletcreatecfg.EndpointIntrospection,
	}
	inst.submit()

	// The reply lands off the frame goroutine; poll the status line.
	require.Eventually(t, func() bool {
		inst.mu.Lock()
		defer inst.mu.Unlock()
		return strings.Contains(inst.status, "saved my-applet")
	}, 2*time.Second, 10*time.Millisecond)

	bus.mu.Lock()
	defer bus.mu.Unlock()
	assert.Equal(t, appletstore.SubjectSave, bus.gotSubject)
	req, err := appletstore.DecodeSaveRequest(bus.gotPayload)
	require.NoError(t, err)
	assert.Equal(t, "my-applet", req.Slug)
	doc := string(req.Doc)
	assert.Contains(t, doc, `title: "My Applet"`)
	assert.Contains(t, doc, "```sql\nSELECT 1\n```")
	assert.Contains(t, doc, `endpoint: "introspection"`)
}

func TestSubmitGuardsEmptyTitle(t *testing.T) {
	// Title is required (ComposeAppletDoc guard); the guard is synchronous and
	// no request is made.
	bus := &fakeSaveBus{reply: appletstore.SaveReply{OK: true}}
	inst := &App{bus: bus, log: zerolog.Nop(), sql: "SELECT 1", slug: "x"}
	inst.submit()

	inst.mu.Lock()
	status := inst.status
	inst.mu.Unlock()
	assert.Contains(t, status, "title is required")

	bus.mu.Lock()
	defer bus.mu.Unlock()
	assert.Empty(t, bus.gotSubject, "no save request should be made without a title")
}

// fakeFsBus drives the fs Powerbox export flow: it grants a write handle for
// fs.dialog.write and captures the bytes written to the granted handle.
type fakeFsBus struct {
	mu          sync.Mutex
	dialogReq   []byte
	wrote       []byte
	grant       bool
	grantReason string
}

var _ app.BusI = (*fakeFsBus)(nil)

func (f *fakeFsBus) Publish(subject string, payload []byte) (err error) { return }
func (f *fakeFsBus) Subscribe(subject string, handler app.MsgHandlerFunc) (unsubscribe func(), err error) {
	return
}
func (f *fakeFsBus) Request(subject string, payload []byte) (reply []byte, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch {
	case subject == fsbroker.SubjectDialogWrite:
		f.dialogReq = append([]byte(nil), payload...)
		if !f.grant {
			return fsbroker.MarshalDialogReply(fsbroker.DialogReply{Granted: false, Reason: f.grantReason})
		}
		return fsbroker.MarshalDialogReply(fsbroker.DialogReply{Granted: true, HandleSubjectPrefix: "fs.handle.abc123"})
	case strings.HasPrefix(subject, "fs.handle.") && strings.HasSuffix(subject, ".write"):
		f.wrote = append([]byte(nil), payload...)
		return fsbroker.MarshalDialogReply(fsbroker.DialogReply{Granted: true})
	default:
		return nil, fmt.Errorf("fakeFsBus: unexpected subject %q", subject)
	}
}

func TestExportComposesAndWritesFile(t *testing.T) {
	bus := &fakeFsBus{grant: true}
	inst := &App{bus: bus, log: zerolog.Nop(), sql: "SELECT 9", slug: "exp-applet", title: "Exp Applet", icon: "📄"}
	inst.export()

	require.Eventually(t, func() bool {
		inst.mu.Lock()
		defer inst.mu.Unlock()
		return strings.Contains(inst.status, "exported exp-applet.md")
	}, 2*time.Second, 10*time.Millisecond)

	bus.mu.Lock()
	defer bus.mu.Unlock()
	// The save dialog was opened with the slug-derived suggested filename.
	dreq, err := fsbroker.UnmarshalDialogRequest(bus.dialogReq)
	require.NoError(t, err)
	assert.Equal(t, "exp-applet.md", dreq.SuggestedName)
	// The composed document landed at the granted write handle.
	doc := string(bus.wrote)
	assert.Contains(t, doc, `title: "Exp Applet"`)
	assert.Contains(t, doc, "```sql\nSELECT 9\n```")
}

func TestExportCancelledSurfaces(t *testing.T) {
	bus := &fakeFsBus{grant: false, grantReason: "cancelled"}
	inst := &App{bus: bus, log: zerolog.Nop(), sql: "SELECT 1", slug: "x", title: "X"}
	inst.export()

	require.Eventually(t, func() bool {
		inst.mu.Lock()
		defer inst.mu.Unlock()
		return strings.Contains(inst.status, "cancelled")
	}, 2*time.Second, 10*time.Millisecond)

	bus.mu.Lock()
	defer bus.mu.Unlock()
	assert.Empty(t, bus.wrote, "nothing is written when the dialog is not granted")
}

func TestExportGuardsEmptyTitle(t *testing.T) {
	// The compose guard is synchronous; no dialog is opened without a title.
	bus := &fakeFsBus{grant: true}
	inst := &App{bus: bus, log: zerolog.Nop(), sql: "SELECT 1", slug: "x"}
	inst.export()

	inst.mu.Lock()
	status := inst.status
	inst.mu.Unlock()
	assert.Contains(t, status, "title is required")

	bus.mu.Lock()
	defer bus.mu.Unlock()
	assert.Nil(t, bus.dialogReq, "no dialog opened without a title")
}
