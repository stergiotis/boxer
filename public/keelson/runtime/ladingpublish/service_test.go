package ladingpublish

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/storage/recordstore"
)

const (
	tagA identifier.TagValue = 2178400
	tagB identifier.TagValue = 2178401
	appA app.AppIdT          = "example.org/a"
	appB app.AppIdT          = "example.org/b"
)

func testLimits() Limits {
	return Limits{Ceiling: agentrun.TierAmber, MaxTtlDays: 90, MaxBytes: 1 << 10}
}

func manifests() []app.Manifest {
	return []app.Manifest{
		{Id: appA, Caps: []app.SubjectFilter{{Pattern: Subject(tagA), Direction: app.CapDirectionPub}}},
		{Id: appB, Caps: []app.SubjectFilter{{Pattern: Subject(tagB), Direction: app.CapDirectionPub}, {Pattern: "lading.publish.>", Direction: app.CapDirectionPub}}},
	}
}

func testService(t *testing.T, ms []app.Manifest, connect func(ctx context.Context) (recordstore.ExecutorI, error)) *Service {
	t.Helper()
	s := newService(Config{Limits: testLimits(), Connect: connect}, zerolog.New(zerolog.NewTestWriter(t)), nil)
	s.owners = ownersOf(ms, s.log)
	t.Cleanup(s.Close)
	return s
}

func oneFile() []File { return []File{{Path: "a.md", Data: []byte("# a\n")}} }

func TestATagTwoAppsDeclareIsOwnedByNeither(t *testing.T) {
	ms := append(manifests(), app.Manifest{Id: "example.org/c", Caps: []app.SubjectFilter{{Pattern: Subject(tagA), Direction: app.CapDirectionBoth}}})
	owners := ownersOf(ms, zerolog.Nop())
	assert.Equal(t, app.AppIdT(""), owners[tagA])
	assert.Equal(t, appB, owners[tagB], "a wildcard declares no tag")

	ms = append(manifests(), app.Manifest{Id: "example.org/c", Caps: []app.SubjectFilter{{Pattern: Subject(tagA), Direction: app.CapDirectionSub}}})
	assert.Equal(t, appA, ownersOf(ms, zerolog.Nop())[tagA], "a declaration to subscribe owns nothing")
}

func TestTheCheckRefusesBeforeAnythingIsWritten(t *testing.T) {
	s := testService(t, manifests(), nil)
	ok := Request{Tier: uint8(agentrun.TierGreen), Files: oneFile()}
	underB, err := mintMount(tagB)
	require.NoError(t, err)
	for _, c := range []struct {
		name string
		by   app.AppIdT
		tag  identifier.TagValue
		req  Request
		want string
	}{
		{"another app's tag", appB, tagA, ok, "is example.org/a's"},
		{"an undeclared tag", appA, 2178402, ok, "no app declares"},
		{"a mount under another tag", appA, tagA, Request{Mount: uint64(underB), Files: oneFile()}, "not under the tag"},
		{"confined", appA, tagA, Request{Confined: true, Files: oneFile()}, "sealed data"},
		{"above the ceiling", appA, tagA, Request{Tier: uint8(agentrun.TierRed), Files: oneFile()}, "above the AMBER"},
		{"too long a class", appA, tagA, Request{TtlDays: 91, Files: oneFile()}, "longer than the 90"},
		{"no files", appA, tagA, Request{}, "no files"},
		{"a path out of the root", appA, tagA, Request{Files: []File{{Path: "../x"}}}, "not a slash-separated path"},
		{"a path twice", appA, tagA, Request{Files: []File{{Path: "x"}, {Path: "x"}}}, "twice"},
		{"too many bytes", appA, tagA, Request{Files: []File{{Path: "x", Data: make([]byte, 2<<10)}}}, "more than the 1024"},
		{"a file that is a directory", appA, tagA, Request{Files: []File{{Path: "a"}, {Path: "a/b/c"}}}, "a file and a directory"},
		{"the bare tag as a mount", appA, tagA, Request{Mount: uint64(tagA.GetTag().ComposeId(0)), Files: oneFile()}, "not under the tag"},
	} {
		t.Run(c.name, func(t *testing.T) {
			res := s.Publish(context.Background(), c.by, 0, c.tag, c.req)
			assert.True(t, res.Refused)
			assert.Contains(t, res.Reason, c.want)
		})
	}
}

func TestWithoutAStoreAPublishFailsWithTheReason(t *testing.T) {
	s := testService(t, manifests(), nil)
	res := s.Publish(context.Background(), appA, 0, tagA, Request{Files: oneFile()})
	assert.True(t, res.Failed)
	assert.Contains(t, res.Reason, "no lading store")
	assert.NotZero(t, res.Mount, "the mount was minted under the tag")
	assert.True(t, tagA.GetTag().SameTag(identifier.TaggedId(res.Mount)))

	s = testService(t, manifests(), func(context.Context) (recordstore.ExecutorI, error) { return nil, errors.New("down") })
	res = s.Publish(context.Background(), appA, 0, tagA, Request{Files: oneFile()})
	assert.True(t, res.Failed)
	assert.Contains(t, res.Reason, "down")
}

// Over the bus the subject carries the tag, the bus keeps an app to the
// subjects it declared, and a refusal comes back as a RefusedError.
func TestOverTheBus(t *testing.T) {
	bus := inprocbus.NewInst(zerolog.Nop())
	s, err := NewService(Config{Bus: bus, Registry: app.NewRegistry(), Limits: testLimits(), Log: zerolog.Nop()})
	require.NoError(t, err)
	// The owners as manifests that declare the subjects would give them.
	s.owners = ownersOf(manifests(), zerolog.Nop())
	t.Cleanup(s.Close)

	clientA := bus.NewClient(appA, []app.SubjectFilter{{Pattern: Subject(tagA), Direction: app.CapDirectionPub}})
	_, err = Publish(clientA, tagA, Request{Confined: true, Files: oneFile()})
	var re *RefusedError
	require.ErrorAs(t, err, &re)
	assert.True(t, re.Result.Refused)

	_, err = Publish(clientA, tagB, Request{Files: oneFile()})
	assert.ErrorIs(t, err, inprocbus.ErrPermissionViolation, "the bus keeps an app to the tags it declared")

	// A dry run answers with the check, and writes nothing: without a store
	// a publish would fail, the check passes.
	require.NoError(t, Check(clientA, tagA, Request{Files: oneFile()}))
	err = Check(clientA, tagA, Request{Tier: uint8(agentrun.TierRed), Files: oneFile()})
	require.ErrorAs(t, err, &re)
	assert.Contains(t, re.Result.Reason, "above the AMBER")

	// A request that does not decode is refused, not left unanswered.
	reply, err := clientA.RequestWithTimeout(Subject(tagA), []byte{0xff}, time.Second)
	require.NoError(t, err)
	res, err := buscodec.Decode[Result](reply)
	require.NoError(t, err)
	assert.True(t, res.Refused)
}

// With no service on the bus a check fails in its own short time, naming
// the service, rather than in a publish's two minutes.
func TestACheckWithoutTheServiceSaysSo(t *testing.T) {
	bus := inprocbus.NewInst(zerolog.Nop())
	client := bus.NewClient(appA, []app.SubjectFilter{{Pattern: Subject(tagA), Direction: app.CapDirectionPub}})
	prior := checkTimeout
	checkTimeout = 50 * time.Millisecond
	t.Cleanup(func() { checkTimeout = prior })
	err := Check(client, tagA, Request{Files: oneFile()})
	assert.ErrorIs(t, err, inprocbus.ErrTimeout)
	assert.ErrorContains(t, err, "does this host run it")
}

// A publish queued behind another past its deadline gives up before it
// writes, and Close cancels one in flight.
func TestAPublishGivesUpAtItsDeadline(t *testing.T) {
	s := testService(t, manifests(), nil)
	s.turn <- struct{}{}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	res := s.Publish(ctx, appA, 0, tagA, Request{Files: oneFile()})
	assert.True(t, res.Failed)
	assert.Contains(t, res.Reason, "behind another publish")
	<-s.turn
}
