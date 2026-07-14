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
	// permissive grant); it tries to read A's handle. The bus permits the
	// publish (B has the cap), but the service trusts the subject — for
	// M2.6 hygiene-mode this means B succeeds. Documenting the gap: real
	// enforcement requires M4 NKey identity. For now this test asserts
	// the existing behaviour so a regression is caught.
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
	// Today: succeeds because the service does not check Msg.Sender
	// against handle.appId. M4 NKey identity will tighten this.
	assert.Equal(t, []byte("secret"), bReply, "hygiene-mode: documenting cross-app handle access")
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
