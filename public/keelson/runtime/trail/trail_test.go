package trail

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/functional/option"
	"github.com/stergiotis/boxer/public/keelson/runtime/vocab"
)

// The ditch names the text section's columns by hand; they must be the
// table's, and all of them, or it would leave a section half-cleared.
func TestDitchNamesEveryTextSectionColumn(t *testing.T) {
	ddl, err := os.ReadFile("facts_ddl_clickhouse.out.sql")
	require.NoError(t, err)
	var inDdl int
	for _, line := range strings.Split(string(ddl), "\n") {
		if strings.Contains(line, `"tv:textArray:`) {
			inDdl++
		}
	}
	assert.Equal(t, inDdl, len(textSectionCols))
	for _, c := range textSectionCols {
		assert.Contains(t, string(ddl), c)
	}
	sql := DitchBodiesSQL("", "an 'app'")
	assert.True(t, strings.HasPrefix(sql, "ALTER TABLE boxer.facts UPDATE "))
	assert.Contains(t, sql, `= 'an \'app\''`)
}

// The body is the only component of a message row on the text section, the
// property the ditch rests on (ADR-0277 §SD4).
func TestOnlyTheBodyBindsTheTextSection(t *testing.T) {
	for comp, sql := range map[string]string{
		"Origin": factsScanOriginFilter, "Conversation": factsScanConversationFilter, "Delegation": factsScanDelegationFilter,
		"Cause": factsScanCauseFilter, "LlmCall": factsScanLlmCallFilter, "LlmMessage": factsScanLlmMessageFilter,
		"AgentAction": factsScanAgentActionFilter, "AgentGrant": factsScanAgentGrantFilter, "HttpFetch": factsScanHttpFetchFilter,
		"AgentCapture": factsScanAgentCaptureFilter, "AgentDisclosure": factsScanAgentDisclosureFilter,
	} {
		assert.NotContains(t, sql, "tv:textArray", comp+" binds no slot on the text section")
	}
	assert.Contains(t, factsScanLlmMessageBodyFilter, "tv:textArray")
}

// The context components spell run, app and window with the memberships
// every runtime-written row uses (ADR-0191 §SD1), so one predicate finds a
// run's rows of any kind.
func TestOriginUsesTheSharedMemberships(t *testing.T) {
	ids := TrailMembershipIds["Origin"]
	assert.Equal(t, vocab.MembRuntimeRun.GetId().Value(), ids["runtimeRun"])
	assert.Equal(t, vocab.MembRuntimeApp.GetId().Value(), ids["runtimeApp"])
	assert.Equal(t, vocab.MembLifecycleTileKey.GetId().Value(), ids["runtimeLifecycleTileKey"])
}

// The capture record's memberships come from the runtime vocabulary, so a
// scan filters on the ids its rows carry (ADR-0281 §SD6).
func TestTheCaptureRecordUsesTheVocabulary(t *testing.T) {
	ids := TrailMembershipIds["AgentCapture"]
	assert.Equal(t, vocab.MembKindAgentCapture.GetId().Value(), ids["runtimeKindAgentCapture"])
	assert.Equal(t, vocab.MembAgentCaptureDigest.GetId().Value(), ids["agentCaptureDigest"])
	assert.Equal(t, vocab.MembAgentCaptureObligations.GetId().Value(), ids["agentCaptureObligations"])
}

// So do the disclosure record's (ADR-0287 §SD6), and its digests join a
// capture's and a message's images.
func TestTheDisclosureRecordUsesTheVocabulary(t *testing.T) {
	ids := TrailMembershipIds["AgentDisclosure"]
	assert.Equal(t, vocab.MembKindAgentDisclosure.GetId().Value(), ids["runtimeKindAgentDisclosure"])
	assert.Equal(t, vocab.MembAgentDisclosureDigest.GetId().Value(), ids["agentDisclosureDigest"])
	assert.Equal(t, vocab.MembAgentDisclosureRootDigest.GetId().Value(), ids["agentDisclosureRootDigest"])
	assert.Equal(t, vocab.MembAgentDisclosureDecidedBy.GetId().Value(), ids["agentDisclosureDecidedBy"])
}

// A nil recorder and one without a backend record nothing, say so, and
// refuse only under BOXER_TRAIL_REQUIRED.
func TestRecorderWithoutABackend(t *testing.T) {
	var none *Recorder
	assert.False(t, none.Durable())
	assert.NoError(t, none.LlmCall(time.Now(), Context{}, LlmCall{CallId: "c"}))
	assert.NoError(t, none.Flush(context.Background()))
	assert.NoError(t, none.Admit())
	durable, refuse := none.WriteAhead(context.Background())
	assert.False(t, durable)
	assert.NoError(t, refuse)
	assert.Equal(t, Origin{App: "a", Instance: 3}, none.OriginOf("a", 3))

	RequiredEnv.SetForTest(t, "true")
	rec := NewRecorder(nil, "run-1", zerolog.Nop())
	defer rec.Close()
	assert.Equal(t, "run-1", rec.OriginOf("a", 3).Run, "the recorder stamps its run")
	assert.NoError(t, rec.AgentGrant(time.Now(), Context{Delegation: option.Some(Delegation{Task: "t"})}, option.None[Cause](), AgentGrant{Event: GrantEventEnded}))
	assert.Error(t, rec.Admit())
	_, refuse = rec.WriteAhead(context.Background())
	require.Error(t, refuse)
	assert.Contains(t, refuse.Error(), "BOXER_TRAIL_REQUIRED")
}

func TestToolKeyAndDigest(t *testing.T) {
	assert.Equal(t, "llm-1#2", ToolKey("llm-1", 2))
	assert.Len(t, ContentDigest("x"), 32)
	assert.NotEqual(t, ContentDigest("x"), ContentDigest("y"))
}
