package clipboardbroker_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/cell"
	"github.com/stergiotis/boxer/public/keelson/runtime/clipboardbroker"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
)

func policyBus(t *testing.T, p clipboardbroker.Policy) (bus *inprocbus.Inst, svc *clipboardbroker.Service, cli *inprocbus.Client) {
	t.Helper()
	bus = inprocbus.NewInst(zerolog.Nop())
	svc, err := clipboardbroker.NewServiceWithPolicy(bus, zerolog.Nop(), p)
	require.NoError(t, err)
	t.Cleanup(svc.Close)
	cli = bus.NewClient("test.app", []app.SubjectFilter{
		{Pattern: clipboardbroker.SubjectWrite, Direction: app.CapDirectionPub, Reason: "test: copy"},
		{Pattern: clipboardbroker.SubjectResolve, Direction: app.CapDirectionPub, Reason: "test: paste"},
	})
	return
}

// A raw payload is text and nothing else; a labelled one carries its
// label and source through the wire and back.
func TestThePayloadIsToldFromRawText(t *testing.T) {
	p, labelled, err := clipboardbroker.DecodePayload([]byte("hello"))
	require.NoError(t, err)
	assert.False(t, labelled)
	assert.Equal(t, "hello", p.Text)

	in := clipboardbroker.Payload{Text: "SELECT 1", Label: agentrun.Label{Level: agentrun.LevelOwnModel, Tier: agentrun.TierAmber, Origin: []string{"r1"}}, Source: "play/sql"}
	b, err := in.Encode()
	require.NoError(t, err)
	out, labelled, err := clipboardbroker.DecodePayload(b)
	require.NoError(t, err)
	assert.True(t, labelled)
	assert.Equal(t, in.Text, out.Text)
	assert.Equal(t, in.Source, out.Source)
	assert.True(t, in.Label.Equal(out.Label))
	_, ok := clipboardbroker.IsHandle("boxer:clip/abc")
	assert.True(t, ok)
	_, ok = clipboardbroker.IsHandle("boxer:clip/")
	assert.False(t, ok)
}

// Under the default policy every copy leaves as plaintext: raw text drains
// as before, a labelled copy at CLEAR drains plaintext and is recorded with
// its label.
func TestTheDefaultPolicyRecordsAndForwardsEverything(t *testing.T) {
	_, svc, cli := policyBus(t, clipboardbroker.DefaultPolicy())
	require.NoError(t, clipboardbroker.WriteText(cli, "raw"))
	rec, err := clipboardbroker.Write(cli, clipboardbroker.Payload{Text: "clear text", Label: agentrun.Label{Level: agentrun.LevelOwnModel}, Source: "chat/reply"})
	require.NoError(t, err)
	assert.True(t, rec.Plaintext)
	assert.Empty(t, rec.Handle)
	assert.Equal(t, agentrun.ContentDigest("clear text"), rec.Digest)
	assert.Equal(t, "chat/reply", rec.Source)
	assert.Equal(t, agentrun.LevelOwnModel, rec.Label.Level)

	amber, err := clipboardbroker.Write(cli, clipboardbroker.Payload{Text: "amber text", Label: agentrun.Label{Tier: agentrun.TierAmber}, Source: "play/result"})
	require.NoError(t, err)
	assert.True(t, amber.Plaintext, "record mode forwards plaintext")
	assert.False(t, amber.Recorded, "within the free tier")

	assert.Equal(t, []string{"raw", "clear text", "amber text"}, svc.DrainPending())
	records := svc.Records()
	require.Len(t, records, 3, "raw text leaves a record with the zero label")
	assert.True(t, records[0].Raw)
	assert.Equal(t, agentrun.ContentDigest("raw"), records[0].Digest)
	assert.Equal(t, "chat/reply", records[1].Source)
	assert.Equal(t, uint8(agentrun.TierAmber), records[2].Tier)
}

// Under enforcement, labelled text above the free tier on the person's
// path leaves as a handle that resolves inside boxer to the payload with
// its labels, and the handle carries nothing of the content; a tier on
// any other path is refused and nothing is enqueued; at or below the
// tier it leaves plaintext, unless another model touched it; in record
// mode it leaves plaintext with a recorded exit.
func TestEnforcementCopiesByReference(t *testing.T) {
	_, svc, cli := policyBus(t, clipboardbroker.Policy{FreeTier: agentrun.TierGreen, Mode: clipboardbroker.PolicyModeEnforce})
	label := agentrun.Label{Level: agentrun.LevelExternal, Tier: agentrun.TierAmber, Exposure: []string{"openai"}}
	rec, err := clipboardbroker.Write(cli, clipboardbroker.Payload{Text: "secret rows", Label: label, Source: "play/result"})
	require.NoError(t, err)
	assert.False(t, rec.Plaintext)
	assert.True(t, strings.HasPrefix(rec.Handle, clipboardbroker.HandlePrefix))
	assert.Empty(t, rec.Approval, "a handle is not an approval")
	id, _ := clipboardbroker.IsHandle(rec.Handle)
	assert.Len(t, id, 32, "a random id")
	assert.NotContains(t, id, rec.Digest[:8], "nothing of the content")
	again, err := clipboardbroker.Write(cli, clipboardbroker.Payload{Text: "secret rows", Label: label, Source: "play/result"})
	require.NoError(t, err)
	assert.NotEqual(t, rec.Handle, again.Handle, "the same text gets a fresh id")
	drained := svc.DrainPending()
	require.Equal(t, []string{rec.Handle, again.Handle}, drained, "the clipboard gets the inert handle")

	_, err = clipboardbroker.Write(cli, clipboardbroker.Payload{Text: "board minutes", Label: agentrun.Label{Tier: agentrun.TierAmberStrict}, Source: "play/result"})
	var refused *clipboardbroker.RefusedError
	require.True(t, errors.As(err, &refused), "%v", err)
	assert.Contains(t, refused.Record.Reason, "organisation")
	_, err = clipboardbroker.Write(cli, clipboardbroker.Payload{Text: "keys", Label: agentrun.Label{Tier: agentrun.TierRed}, Source: "play/result"})
	require.True(t, errors.As(err, &refused), "%v", err)
	assert.Contains(t, refused.Record.Reason, "never")
	assert.Nil(t, svc.DrainPending(), "a refused copy reaches no clipboard")
	otherModel, err := clipboardbroker.Write(cli, clipboardbroker.Payload{Text: "their sql", Label: agentrun.Label{Level: agentrun.LevelOtherModel}, Source: "chat/code"})
	require.NoError(t, err)
	assert.NotEmpty(t, otherModel.Handle, "content another model touched copies by reference")
	assert.Empty(t, otherModel.Approval, "within the free tier")
	svc.DrainPending()

	p, ok, err := clipboardbroker.Resolve(cli, rec.Handle)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "secret rows", p.Text)
	assert.True(t, label.Equal(p.Label), "a paste inside boxer restores the labels")
	assert.Equal(t, "play/result", p.Source)
	_, ok, err = clipboardbroker.Resolve(cli, "boxer:clip/nope")
	require.NoError(t, err)
	assert.False(t, ok)
	_, ok, err = clipboardbroker.Resolve(cli, "not a handle")
	require.NoError(t, err)
	assert.False(t, ok)

	integrityOnly, err := clipboardbroker.Write(cli, clipboardbroker.Payload{Text: "agent sql", Label: agentrun.Label{Level: agentrun.LevelOwnModel}, Source: "chat/code"})
	require.NoError(t, err)
	assert.True(t, integrityOnly.Plaintext, "this run's own model output at CLEAR copies plaintext with its record")
	assert.Equal(t, []string{"agent sql"}, svc.DrainPending())

	_, recSvc, recCli := policyBus(t, clipboardbroker.Policy{FreeTier: agentrun.TierGreen, Mode: clipboardbroker.PolicyModeRecord})
	recorded, err := clipboardbroker.Write(recCli, clipboardbroker.Payload{Text: "secret rows", Label: label, Source: "play/result"})
	require.NoError(t, err)
	assert.True(t, recorded.Plaintext)
	assert.True(t, recorded.Recorded, "a host the person administers records the exit")
	assert.Equal(t, []string{"secret rows"}, recSvc.DrainPending())
	touched, err := clipboardbroker.Write(recCli, clipboardbroker.Payload{Text: "their sql", Label: agentrun.Label{Level: agentrun.LevelOtherModel}, Source: "chat/code"})
	require.NoError(t, err)
	assert.NotEmpty(t, touched.Handle, "content another model touched copies by reference whatever the mode (ADR-0317 §SD3)")
	assert.False(t, touched.Plaintext)
	assert.Equal(t, []string{touched.Handle}, recSvc.DrainPending())
}

// Under enforcement raw text, whose provenance the broker cannot know,
// copies by reference like content another model touched; on a host
// that records it goes as it is. A raw copy is recorded either way.
func TestEnforcementDoesNotLetRawTextBypassThePolicy(t *testing.T) {
	_, svc, cli := policyBus(t, clipboardbroker.Policy{FreeTier: agentrun.TierGreen, Mode: clipboardbroker.PolicyModeEnforce})
	require.NoError(t, clipboardbroker.WriteText(cli, "unlabelled"))
	drained := svc.DrainPending()
	require.Len(t, drained, 1)
	_, isHandle := clipboardbroker.IsHandle(drained[0])
	assert.True(t, isHandle, "raw text leaves as a handle under enforcement")
	records := svc.Records()
	require.Len(t, records, 1)
	assert.True(t, records[0].Raw)
	assert.False(t, records[0].Plaintext)
	assert.Equal(t, uint8(agentrun.LevelOtherModel), records[0].Level, "unknown provenance reads as another model's")
	p, ok := svc.Resolve(strings.TrimPrefix(drained[0], clipboardbroker.HandlePrefix))
	require.True(t, ok)
	assert.Equal(t, "unlabelled", p.Text)
	assert.Empty(t, clipboardbroker.CheckL20(agentrun.TierGreen, records, []clipboardbroker.ForwardedCopy{{Text: drained[0]}}))
}

// Law L20 over a run of copies: the broker's own sequence passes; a copy
// forwarded without a record, or labelled text above the tier that left
// plaintext with no recorded exit, breaks it.
func TestLawL20(t *testing.T) {
	_, svc, cli := policyBus(t, clipboardbroker.Policy{FreeTier: agentrun.TierGreen, Mode: clipboardbroker.PolicyModeEnforce})
	_, err := clipboardbroker.Write(cli, clipboardbroker.Payload{Text: "a", Label: agentrun.Label{Tier: agentrun.TierAmber}})
	require.NoError(t, err)
	_, err = clipboardbroker.Write(cli, clipboardbroker.Payload{Text: "b", Label: agentrun.Label{Tier: agentrun.TierClear, Level: agentrun.LevelOwnModel}})
	require.NoError(t, err)
	var forwarded []clipboardbroker.ForwardedCopy
	for _, text := range svc.DrainPending() {
		forwarded = append(forwarded, clipboardbroker.ForwardedCopy{Text: text})
	}
	assert.Empty(t, clipboardbroker.CheckL20(agentrun.TierGreen, svc.Records(), forwarded))

	bypass := append(forwarded, clipboardbroker.ForwardedCopy{Text: "copied around the broker"})
	assert.Len(t, clipboardbroker.CheckL20(agentrun.TierGreen, svc.Records(), bypass), 1)
	planted := append(svc.Records(), clipboardbroker.CopyRecord{Digest: agentrun.ContentDigest("c"), Tier: uint8(agentrun.TierRed), Plaintext: true})
	assert.Len(t, clipboardbroker.CheckL20(agentrun.TierGreen, planted, forwarded), 1)

	require.NoError(t, clipboardbroker.WriteText(cli, "raw copy"))
	raw := svc.DrainPending()
	require.Len(t, raw, 1)
	_, isHandle := clipboardbroker.IsHandle(raw[0])
	assert.True(t, isHandle, "under enforcement a raw copy leaves by reference")
	assert.Empty(t, clipboardbroker.CheckL20(agentrun.TierGreen, svc.Records(), append(forwarded, clipboardbroker.ForwardedCopy{Text: raw[0]})), "a raw copy the broker recorded is not a bypass")
}

// A labelled payload that does not decode is refused with a record that
// says so, and Write reports it rather than a copy that never happened;
// the cell declaration maps onto the policy.
func TestAMalformedPayloadIsRefusedNotForwarded(t *testing.T) {
	_, svc, cli := policyBus(t, clipboardbroker.DefaultPolicy())
	good, err := clipboardbroker.Payload{Text: "x"}.Encode()
	require.NoError(t, err)
	reply, err := cli.Request(clipboardbroker.SubjectWrite, good[:len(good)-2])
	require.NoError(t, err)
	rec, err := buscodec.Decode[clipboardbroker.CopyRecord](reply)
	require.NoError(t, err)
	assert.True(t, rec.Refused)
	assert.Contains(t, rec.Reason, "does not decode")
	assert.Nil(t, svc.DrainPending())
	assert.Empty(t, svc.Records(), "a refused decode leaves no copy record")

	decl := cell.Defaults()
	decl.Clearances.Clipboard = agentrun.TierAmber
	decl.Managed = true
	p := clipboardbroker.PolicyOf(decl)
	assert.Equal(t, agentrun.TierAmber, p.FreeTier)
	assert.Equal(t, clipboardbroker.PolicyModeEnforce, p.Mode)
	assert.Equal(t, cell.ApprovalTierPerson, p.Approvals.Path(agentrun.TierAmber))
	decl.Managed = false
	assert.Equal(t, clipboardbroker.PolicyModeRecord, clipboardbroker.PolicyOf(decl).Mode)
}
