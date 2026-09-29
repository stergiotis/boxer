package fsbroker_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/fsbroker"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
)

// newSetup spins up an Inst + fsbroker.Service + an app client. The app
// client is granted only fs.dialog.read at construction; the broker
// augments its caps to include fs.handle.{uuid}.> on Resolve.
func newSetup(t *testing.T) (inst *inprocbus.Inst, svc *fsbroker.Service, appBus *inprocbus.Client, cleanup func()) {
	t.Helper()
	inst = inprocbus.NewInst(zerolog.Nop())
	inst.SetRequestTimeout(500 * time.Millisecond)
	svc, err := fsbroker.NewService(inst, zerolog.Nop())
	require.NoError(t, err)
	appBus = inst.NewClient("test.app", []app.SubjectFilter{
		{Pattern: fsbroker.SubjectDialogRead, Direction: app.CapDirectionPub, Reason: "test app may request reads"},
	})
	cleanup = func() {
		svc.Close()
	}
	return
}

// pendingOnce waits until exactly one dialog is pending or fails the test.
// Used to synchronise the asynchronous "app issues Request" against the
// main goroutine's "broker has accepted request".
func pendingOnce(t *testing.T, svc *fsbroker.Service) (req fsbroker.PendingRequest) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		all := svc.Pending()
		if len(all) == 1 {
			req = all[0]
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("no dialog pending after timeout")
	return
}

func TestNewService_NilInstRejected(t *testing.T) {
	_, err := fsbroker.NewService(nil, zerolog.Nop())
	require.Error(t, err)
}

func TestService_DialogRead_ResolveGrantsHandleAndReads(t *testing.T) {
	inst, svc, appBus, cleanup := newSetup(t)
	defer cleanup()
	_ = inst

	tmp := t.TempDir()
	path := filepath.Join(tmp, "hello.txt")
	want := []byte("hello world")
	require.NoError(t, os.WriteFile(path, want, 0o644))

	type res struct {
		reply []byte
		err   error
	}
	resultCh := make(chan res, 1)
	go func() {
		reply, err := appBus.Request(fsbroker.SubjectDialogRead, nil)
		resultCh <- res{reply, err}
	}()

	req := pendingOnce(t, svc)
	assert.Equal(t, "read", req.Op)
	assert.Equal(t, app.AppIdT("test.app"), req.AppId)

	handleUuid, err := svc.Resolve(req.Id, path)
	require.NoError(t, err)
	require.NotEmpty(t, handleUuid)

	r := <-resultCh
	require.NoError(t, r.err)
	reply, err := fsbroker.UnmarshalDialogReply(r.reply)
	require.NoError(t, err)
	require.True(t, reply.Granted)
	require.Equal(t, fsbroker.HandleSubjectPrefix+handleUuid, reply.HandleSubjectPrefix)
	// The one fact about the file's identity the Powerbox reveals: the
	// basename, never the path.
	assert.Equal(t, "hello.txt", reply.DisplayName)
	assert.NotContains(t, reply.DisplayName, string(filepath.Separator))

	// Now read via the granted handle subject. The app's caps must have
	// been augmented by Resolve so this Publish doesn't trip
	// ErrPermissionViolation.
	content, err := appBus.Request(reply.HandleSubjectPrefix+".read", nil)
	require.NoError(t, err)
	assert.Equal(t, want, content)
}

func TestService_DialogRead_Cancel(t *testing.T) {
	inst, svc, appBus, cleanup := newSetup(t)
	defer cleanup()
	_ = inst

	type res struct {
		reply []byte
		err   error
	}
	resultCh := make(chan res, 1)
	go func() {
		reply, err := appBus.Request(fsbroker.SubjectDialogRead, nil)
		resultCh <- res{reply, err}
	}()

	req := pendingOnce(t, svc)
	require.NoError(t, svc.Cancel(req.Id))

	r := <-resultCh
	require.NoError(t, r.err)
	reply, err := fsbroker.UnmarshalDialogReply(r.reply)
	require.NoError(t, err)
	assert.False(t, reply.Granted)
	assert.Contains(t, reply.Reason, "cancel")
	assert.Empty(t, reply.DisplayName, "a denial names nothing")
}

func TestService_Resolve_UnknownRequest(t *testing.T) {
	inst, svc, _, cleanup := newSetup(t)
	defer cleanup()
	_ = inst
	_, err := svc.Resolve("nope", "/etc/passwd")
	require.Error(t, err)
}

func TestService_Handle_UnknownUuid(t *testing.T) {
	inst, svc, _, cleanup := newSetup(t)
	defer cleanup()
	_ = svc

	bus := inst.NewClient("test.app2", []app.SubjectFilter{
		{Pattern: "fs.handle.>", Direction: app.CapDirectionPub},
	})
	reply, err := bus.Request("fs.handle.deadbeef.read", nil)
	require.NoError(t, err)
	dr, err := fsbroker.UnmarshalDialogReply(reply)
	require.NoError(t, err)
	assert.False(t, dr.Granted)
	assert.Contains(t, dr.Reason, "unknown handle")
}

func TestService_DialogWrite_ResolveGrantsHandleAndWrites(t *testing.T) {
	inst, svc, _, cleanup := newSetup(t)
	defer cleanup()

	// The write path needs its own client: newSetup's app is granted only
	// fs.dialog.read. The broker augments fs.handle.{uuid}.> on Resolve.
	writer := inst.NewClient("test.writer", []app.SubjectFilter{
		{Pattern: fsbroker.SubjectDialogWrite, Direction: app.CapDirectionPub, Reason: "test app may request writes"},
	})

	tmp := t.TempDir()
	path := filepath.Join(tmp, "out.structdto")
	want := []byte("structdto-container-bytes")

	type res struct {
		reply []byte
		err   error
	}
	resultCh := make(chan res, 1)
	go func() {
		reply, err := writer.Request(fsbroker.SubjectDialogWrite, nil)
		resultCh <- res{reply, err}
	}()

	req := pendingOnce(t, svc)
	assert.Equal(t, "write", req.Op)
	assert.Equal(t, app.AppIdT("test.writer"), req.AppId)
	assert.Empty(t, req.SuggestedName, "nil payload carries no filename hint")

	handleUuid, err := svc.Resolve(req.Id, path)
	require.NoError(t, err)
	require.NotEmpty(t, handleUuid)

	r := <-resultCh
	require.NoError(t, r.err)
	reply, err := fsbroker.UnmarshalDialogReply(r.reply)
	require.NoError(t, err)
	require.True(t, reply.Granted)
	require.Equal(t, fsbroker.HandleSubjectPrefix+handleUuid, reply.HandleSubjectPrefix)
	assert.Equal(t, "out.structdto", reply.DisplayName,
		"a write grant names its file too — the save target's title")

	// Write via the granted handle subject; the ack is a DialogReply so the
	// app can tell success (Granted) from a filesystem error (Reason).
	ackRaw, err := writer.Request(reply.HandleSubjectPrefix+".write", want)
	require.NoError(t, err)
	ack, err := fsbroker.UnmarshalDialogReply(ackRaw)
	require.NoError(t, err)
	require.True(t, ack.Granted, "write ack should be granted, got reason %q", ack.Reason)

	// The bytes landed at the resolved path, byte-for-byte.
	got, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestService_DialogWrite_SurfacesSuggestedName(t *testing.T) {
	inst, svc, _, cleanup := newSetup(t)
	defer cleanup()

	writer := inst.NewClient("test.writer", []app.SubjectFilter{
		{Pattern: fsbroker.SubjectDialogWrite, Direction: app.CapDirectionPub, Reason: "test app may request writes"},
	})

	payload, err := fsbroker.MarshalDialogRequest(fsbroker.DialogRequest{SuggestedName: "out.structdto"})
	require.NoError(t, err)

	type res struct {
		reply []byte
		err   error
	}
	resultCh := make(chan res, 1)
	go func() {
		reply, rerr := writer.Request(fsbroker.SubjectDialogWrite, payload)
		resultCh <- res{reply, rerr}
	}()

	// The suggested filename rides the pending request through to the picker
	// bridge, which pre-fills the "Save as" dialog with it.
	req := pendingOnce(t, svc)
	assert.Equal(t, "write", req.Op)
	assert.Equal(t, "out.structdto", req.SuggestedName)

	// Complete the dialog so the app goroutine doesn't leak on the reply inbox.
	_, err = svc.Resolve(req.Id, filepath.Join(t.TempDir(), "out.structdto"))
	require.NoError(t, err)
	<-resultCh
}

func TestService_Handle_WriteRejectedOnReadHandle(t *testing.T) {
	inst, svc, appBus, cleanup := newSetup(t)
	defer cleanup()
	_ = inst

	tmp := t.TempDir()
	path := filepath.Join(tmp, "ro.txt")
	require.NoError(t, os.WriteFile(path, []byte("existing"), 0o644))

	type res struct {
		reply []byte
		err   error
	}
	resultCh := make(chan res, 1)
	go func() {
		reply, err := appBus.Request(fsbroker.SubjectDialogRead, nil)
		resultCh <- res{reply, err}
	}()
	req := pendingOnce(t, svc)
	_, err := svc.Resolve(req.Id, path)
	require.NoError(t, err)
	r := <-resultCh
	require.NoError(t, r.err)
	dr, err := fsbroker.UnmarshalDialogReply(r.reply)
	require.NoError(t, err)
	require.True(t, dr.Granted)

	// A write on a read-mode handle is refused by the service, and the file on
	// disk is left untouched — the mode gate is what keeps a read grant from
	// being escalated into a write.
	ackRaw, err := appBus.Request(dr.HandleSubjectPrefix+".write", []byte("nope"))
	require.NoError(t, err)
	ack, err := fsbroker.UnmarshalDialogReply(ackRaw)
	require.NoError(t, err)
	assert.False(t, ack.Granted)
	assert.Contains(t, ack.Reason, "not opened for write")

	got, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, []byte("existing"), got, "read handle must not permit overwriting")
}

func TestService_Handle_CloseEvictsHandle(t *testing.T) {
	inst, svc, appBus, cleanup := newSetup(t)
	defer cleanup()
	_ = inst

	tmp := t.TempDir()
	path := filepath.Join(tmp, "x.txt")
	require.NoError(t, os.WriteFile(path, []byte("x"), 0o644))

	type res struct {
		reply []byte
		err   error
	}
	resultCh := make(chan res, 1)
	go func() {
		reply, err := appBus.Request(fsbroker.SubjectDialogRead, nil)
		resultCh <- res{reply, err}
	}()
	req := pendingOnce(t, svc)
	_, err := svc.Resolve(req.Id, path)
	require.NoError(t, err)
	r := <-resultCh
	require.NoError(t, r.err)
	dr, err := fsbroker.UnmarshalDialogReply(r.reply)
	require.NoError(t, err)
	require.True(t, dr.Granted)

	// Close the handle.
	_, err = appBus.Request(dr.HandleSubjectPrefix+".close", nil)
	require.NoError(t, err)

	// Subsequent access is denied at the bus layer: closing the handle
	// revokes the fs.handle.{uuid}.> cap, so the app can no longer even
	// publish to the handle subject (defense in depth on top of the
	// service-side handle eviction).
	_, err = appBus.Request(dr.HandleSubjectPrefix+".read", nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, inprocbus.ErrPermissionViolation)
}

func TestService_AppCannotAccessOtherAppsHandle(t *testing.T) {
	// App A obtains a handle. App B has fs.handle.> pub cap (an unusually
	// permissive grant); it tries to read and to close A's handle. The bus
	// permits the publishes (B has the cap); the broker refuses them because
	// the handle was granted to A, not to the message's sender.
	inst, svc, appA, cleanup := newSetup(t)
	defer cleanup()

	tmp := t.TempDir()
	path := filepath.Join(tmp, "secret.txt")
	require.NoError(t, os.WriteFile(path, []byte("secret"), 0o644))

	type res struct {
		reply []byte
		err   error
	}
	resA := make(chan res, 1)
	go func() {
		r, err := appA.Request(fsbroker.SubjectDialogRead, nil)
		resA <- res{r, err}
	}()
	req := pendingOnce(t, svc)
	_, err := svc.Resolve(req.Id, path)
	require.NoError(t, err)
	rA := <-resA
	drA, err := fsbroker.UnmarshalDialogReply(rA.reply)
	require.NoError(t, err)
	require.True(t, drA.Granted)

	// App B with a permissive cap tries to read A's handle.
	appB := inst.NewClient("test.appB", []app.SubjectFilter{
		{Pattern: "fs.handle.>", Direction: app.CapDirectionPub},
	})
	bReply, err := appB.Request(drA.HandleSubjectPrefix+".read", nil)
	require.NoError(t, err)
	denied, err := fsbroker.UnmarshalDialogReply(bReply)
	require.NoError(t, err, "a refusal, not the file's bytes")
	assert.False(t, denied.Granted)
	assert.NotContains(t, string(bReply), "secret")

	_, err = appB.Request(drA.HandleSubjectPrefix+".close", nil)
	require.NoError(t, err)
	aReply, err := appA.Request(drA.HandleSubjectPrefix+".read", nil)
	require.NoError(t, err)
	assert.Equal(t, []byte("secret"), aReply, "another app's close must not revoke A's grant")
}

func TestDialogRequest_RoundTrip(t *testing.T) {
	orig := fsbroker.DialogRequest{SuggestedName: "résultset.structdto"}
	b, err := fsbroker.MarshalDialogRequest(orig)
	require.NoError(t, err)
	require.NotEmpty(t, b)
	got, err := fsbroker.UnmarshalDialogRequest(b)
	require.NoError(t, err)
	assert.Equal(t, orig, got)

	// A nil / empty payload is the "no hints" wire shape and must decode to a
	// zero DialogRequest without error (nil-payload dialog opens stay valid).
	zero, err := fsbroker.UnmarshalDialogRequest(nil)
	require.NoError(t, err)
	assert.Equal(t, fsbroker.DialogRequest{}, zero)
}

// TestService_Pending_InArrivalOrder: the bridge takes Pending()[0], so the
// list is ordered by arrival, not by map iteration.
func TestService_Pending_InArrivalOrder(t *testing.T) {
	inst, svc, _, cleanup := newSetup(t)
	defer cleanup()
	const n = 8
	for i := 0; i < n; i++ {
		c := inst.NewClient(app.AppIdT("test.order"+string(rune('a'+i))), []app.SubjectFilter{
			{Pattern: fsbroker.SubjectDialogRead, Direction: app.CapDirectionPub},
		})
		go func() { _, _ = c.Request(fsbroker.SubjectDialogRead, nil) }()
		deadline := time.Now().Add(time.Second)
		for len(svc.Pending()) != i+1 && time.Now().Before(deadline) {
			time.Sleep(time.Millisecond)
		}
		require.Len(t, svc.Pending(), i+1)
	}
	for round := 0; round < 20; round++ {
		all := svc.Pending()
		require.Len(t, all, n)
		for i, p := range all {
			require.Equal(t, app.AppIdT("test.order"+string(rune('a'+i))), p.AppId, "round %d", round)
		}
	}
}

// TestService_Pending_DroppedWhenInstanceCloses: a window that closes with a
// dialog open leaves nobody on the reply inbox, so the picker must not pop up
// for it and a late Resolve must not mint a grant.
func TestService_Pending_DroppedWhenInstanceCloses(t *testing.T) {
	inst, svc, _, cleanup := newSetup(t)
	defer cleanup()
	window := inst.NewClient("test.window", []app.SubjectFilter{
		{Pattern: fsbroker.SubjectDialogRead, Direction: app.CapDirectionPub},
	})
	window.SetInstanceKey(7)
	other := inst.NewClient("test.window", []app.SubjectFilter{
		{Pattern: fsbroker.SubjectDialogRead, Direction: app.CapDirectionPub},
	})
	other.SetInstanceKey(8)
	go func() { _, _ = window.Request(fsbroker.SubjectDialogRead, nil) }()
	req := pendingOnce(t, svc)
	go func() { _, _ = other.Request(fsbroker.SubjectDialogRead, nil) }()
	deadline := time.Now().Add(time.Second)
	for len(svc.Pending()) != 2 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	require.Len(t, svc.Pending(), 2)

	require.NoError(t, window.Close())
	left := svc.Pending()
	require.Len(t, left, 1, "only the closed window's dialog is dropped")
	assert.NotEqual(t, req.Id, left[0].Id)
	_, err := svc.Resolve(req.Id, "/etc/hostname")
	require.Error(t, err)
}

// A grant lands on the window that opened the dialog, not on the newest
// window of the same app, and Close revokes it from that same window.
func TestService_Resolve_GrantsTheRequestingWindow(t *testing.T) {
	inst, svc, _, cleanup := newSetup(t)
	defer cleanup()
	older := inst.NewClient("test.window", []app.SubjectFilter{
		{Pattern: fsbroker.SubjectDialogRead, Direction: app.CapDirectionPub},
	})
	older.SetInstanceKey(7)
	newer := inst.NewClient("test.window", []app.SubjectFilter{
		{Pattern: fsbroker.SubjectDialogRead, Direction: app.CapDirectionPub},
	})
	newer.SetInstanceKey(8)

	replies := make(chan []byte, 1)
	go func() {
		reply, _ := older.Request(fsbroker.SubjectDialogRead, nil)
		replies <- reply
	}()
	req := pendingOnce(t, svc)
	handleUuid, err := svc.Resolve(req.Id, "/etc/hostname")
	require.NoError(t, err)
	<-replies

	pattern := fsbroker.HandleSubjectPrefix + handleUuid + ".>"
	assert.True(t, hasCapPattern(older, pattern), "the requesting window holds the handle cap")
	assert.False(t, hasCapPattern(newer, pattern), "another window of the app does not")
}

func hasCapPattern(c *inprocbus.Client, pattern string) bool {
	for _, f := range c.Caps() {
		if f.Pattern == pattern {
			return true
		}
	}
	return false
}
