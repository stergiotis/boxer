package persist

import (
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
)

// newSetup constructs Inst + persist Service backed by an in-memory store,
// plus an app client with caps to drive its own persist subjects.
func newSetup(t *testing.T) (inst *inprocbus.Inst, svc *Service, appBus *inprocbus.Client, mem *MemoryBackend) {
	t.Helper()
	inst = inprocbus.NewInst(zerolog.Nop())
	inst.SetRequestTimeout(100 * time.Millisecond)
	mem = NewMemoryBackend()
	svc, err := NewService(inst, zerolog.Nop(), mem)
	require.NoError(t, err)
	appBus = inst.NewClient("play", []app.SubjectFilter{
		{Pattern: "runtime.persist.play.>", Direction: app.CapDirectionPub, Reason: "play persists own state"},
	})
	return
}

func TestService_NilBackend_Rejected(t *testing.T) {
	inst := inprocbus.NewInst(zerolog.Nop())
	_, err := NewService(inst, zerolog.Nop(), nil)
	require.Error(t, err)
}

func TestService_NilInst_Rejected(t *testing.T) {
	_, err := NewService(nil, zerolog.Nop(), NewMemoryBackend())
	require.Error(t, err)
}

func TestService_Set_Then_Get_RoundTrips(t *testing.T) {
	_, svc, bus, _ := newSetup(t)
	defer svc.Close()

	// Set.
	setReplyBytes, err := bus.Request(SubjectFor("play", "tabs", OpSet), []byte("value-bytes"))
	require.NoError(t, err)
	setReply, err := UnmarshalReply(setReplyBytes)
	require.NoError(t, err)
	assert.Empty(t, setReply.Error)
	assert.False(t, setReply.Found)

	// Get.
	getReplyBytes, err := bus.Request(SubjectFor("play", "tabs", OpGet), nil)
	require.NoError(t, err)
	getReply, err := UnmarshalReply(getReplyBytes)
	require.NoError(t, err)
	assert.Empty(t, getReply.Error)
	assert.True(t, getReply.Found)
	assert.Equal(t, []byte("value-bytes"), getReply.Value)
}

func TestService_Get_MissingKey_ReportsNotFound(t *testing.T) {
	_, svc, bus, _ := newSetup(t)
	defer svc.Close()

	replyBytes, err := bus.Request(SubjectFor("play", "absent", OpGet), nil)
	require.NoError(t, err)
	reply, err := UnmarshalReply(replyBytes)
	require.NoError(t, err)
	assert.False(t, reply.Found)
	assert.Empty(t, reply.Error)
}

func TestService_Delete_RemovesKey(t *testing.T) {
	_, svc, bus, mem := newSetup(t)
	defer svc.Close()

	_, err := bus.Request(SubjectFor("play", "tabs", OpSet), []byte("x"))
	require.NoError(t, err)
	assert.Equal(t, 1, mem.Len())

	_, err = bus.Request(SubjectFor("play", "tabs", OpDelete), nil)
	require.NoError(t, err)
	assert.Equal(t, 0, mem.Len())

	getReplyBytes, _ := bus.Request(SubjectFor("play", "tabs", OpGet), nil)
	reply, _ := UnmarshalReply(getReplyBytes)
	assert.False(t, reply.Found)
}

func TestService_UnknownOp_ErrorReply(t *testing.T) {
	inst, svc, _, _ := newSetup(t)
	defer svc.Close()

	// We need a client allowed to publish to runtime.persist.play.tabs.weird,
	// which the default play client's "runtime.persist.play.>" cap covers.
	bus := inst.NewClient("play", []app.SubjectFilter{
		{Pattern: "runtime.persist.play.>", Direction: app.CapDirectionPub},
	})
	replyBytes, err := bus.Request("runtime.persist.play.tabs.weird", nil)
	require.NoError(t, err)
	reply, err := UnmarshalReply(replyBytes)
	require.NoError(t, err)
	assert.Contains(t, reply.Error, "unknown op")
}

func TestService_MalformedSubject_ErrorReply(t *testing.T) {
	inst, svc, _, _ := newSetup(t)
	defer svc.Close()

	// Build a client with a broader cap to send any runtime.persist subject.
	bus := inst.NewClient("play", []app.SubjectFilter{
		{Pattern: "runtime.persist.>", Direction: app.CapDirectionPub},
	})
	replyBytes, err := bus.Request("runtime.persist.play.tabs", nil) // missing op token
	require.NoError(t, err)
	reply, _ := UnmarshalReply(replyBytes)
	assert.Contains(t, reply.Error, "malformed")
}

func TestService_AliasSeparation_TwoAppsDontCollide(t *testing.T) {
	inst, svc, _, _ := newSetup(t)
	defer svc.Close()

	playBus := inst.NewClient("play", []app.SubjectFilter{
		{Pattern: "runtime.persist.play.>", Direction: app.CapDirectionPub},
	})
	imzBus := inst.NewClient("imztop", []app.SubjectFilter{
		{Pattern: "runtime.persist.imztop.>", Direction: app.CapDirectionPub},
	})
	_, err := playBus.Request(SubjectFor("play", "tabs", OpSet), []byte("p"))
	require.NoError(t, err)
	_, err = imzBus.Request(SubjectFor("imztop", "tabs", OpSet), []byte("i"))
	require.NoError(t, err)

	playGet, _ := playBus.Request(SubjectFor("play", "tabs", OpGet), nil)
	playReply, _ := UnmarshalReply(playGet)
	assert.Equal(t, "p", string(playReply.Value))

	imzGet, _ := imzBus.Request(SubjectFor("imztop", "tabs", OpGet), nil)
	imzReply, _ := UnmarshalReply(imzGet)
	assert.Equal(t, "i", string(imzReply.Value))
}

func TestService_AliasMismatch_LoggedButProceeds(t *testing.T) {
	// "imztop" client publishes to "runtime.persist.play.tabs.set" — under
	// hygiene-mode the service logs the mismatch and proceeds. The bus
	// would normally deny via cap, but here we give imztop a permissive cap
	// so the message reaches the service.
	inst, svc, _, mem := newSetup(t)
	defer svc.Close()

	imzBus := inst.NewClient("imztop", []app.SubjectFilter{
		{Pattern: "runtime.persist.>", Direction: app.CapDirectionPub},
	})
	_, err := imzBus.Request(SubjectFor("play", "tabs", OpSet), []byte("x"))
	require.NoError(t, err)
	// The subject's alias is "play", so the value lands under "play"
	// regardless of the actual sender's identity (M4 NKey would enforce).
	got, found, _ := mem.Get(aliasRef("play"), "tabs")
	assert.True(t, found)
	assert.Equal(t, "x", string(got))
}

// refRecorder is a StorageBackendI that captures the ref of the last call
// so the service's attribution rule can be asserted at its own boundary.
type refRecorder struct {
	mu   sync.Mutex
	last StorageRef
}

var _ StorageBackendI = (*refRecorder)(nil)

func (inst *refRecorder) record(ref StorageRef) {
	inst.mu.Lock()
	inst.last = ref
	inst.mu.Unlock()
}

func (inst *refRecorder) Last() (ref StorageRef) {
	inst.mu.Lock()
	ref = inst.last
	inst.mu.Unlock()
	return
}

func (inst *refRecorder) Get(ref StorageRef, key string) (value []byte, found bool, err error) {
	inst.record(ref)
	return
}

func (inst *refRecorder) Set(ref StorageRef, key string, value []byte) (err error) {
	inst.record(ref)
	return
}

func (inst *refRecorder) Delete(ref StorageRef, key string) (err error) {
	inst.record(ref)
	return
}

func newRecorderSetup(t *testing.T) (inst *inprocbus.Inst, svc *Service, rec *refRecorder) {
	t.Helper()
	inst = inprocbus.NewInst(zerolog.Nop())
	inst.SetRequestTimeout(100 * time.Millisecond)
	rec = &refRecorder{}
	svc, err := NewService(inst, zerolog.Nop(), rec)
	require.NoError(t, err)
	return
}

// The service attributes the durable AppId from the bus envelope whenever
// the sender owns the addressed alias. A fact-recording backend stamps that
// id on the row, so an app whose id differs from its alias still joins its
// own grant / audit / launch facts.
func TestService_AttributesSenderAppId(t *testing.T) {
	inst, svc, rec := newRecorderSetup(t)
	defer svc.Close()

	const id app.AppIdT = "elle/factsplay"
	require.Equal(t, "factsplay", id.SubjectAlias())
	bus := inst.NewClient(id, []app.SubjectFilter{
		{Pattern: "runtime.persist.factsplay.>", Direction: app.CapDirectionPub, Reason: "own state"},
	})

	_, err := bus.Request(SubjectFor(id.SubjectAlias(), "lastSql", OpSet), []byte("x"))
	require.NoError(t, err)

	got := rec.Last()
	assert.Equal(t, "factsplay", got.Alias)
	assert.Equal(t, id, got.AppId)
	assert.Equal(t, id, got.StateAppId())
}

// A sender addressing an alias it does not own is not vouched for: the id
// is withheld so a recorded row is never misattributed to the sender. The
// write still proceeds — this stays the hygiene-only check it was.
func TestService_WithholdsAppIdOnAliasMismatch(t *testing.T) {
	inst, svc, rec := newRecorderSetup(t)
	defer svc.Close()

	imzBus := inst.NewClient("imztop", []app.SubjectFilter{
		{Pattern: "runtime.persist.>", Direction: app.CapDirectionPub, Reason: "permissive for the test"},
	})

	_, err := imzBus.Request(SubjectFor("play", "tabs", OpSet), []byte("x"))
	require.NoError(t, err)

	got := rec.Last()
	assert.Equal(t, "play", got.Alias)
	assert.Empty(t, got.AppId, "sender does not own the addressed alias")
	assert.Equal(t, app.AppIdT("play"), got.StateAppId(), "falls back to the addressed alias")
}
