package openaichat

import (
	"encoding/json/v2"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The default route folds a late system message into the next user turn;
// a route declaring LateSystemInHistory sends it where it stands. Nothing
// selects the second value yet.
func TestARouteDecidesHowALateSystemMessageTravels(t *testing.T) {
	req := CompletionRequest{ModelId: "m", Messages: []Message{
		{Role: ChatRoleSystem, Content: "head"},
		{Role: ChatRoleUser, Content: "hello"},
		{Role: ChatRoleAssistant, Content: "hi"},
		{Role: ChatRoleSystem, Content: "late"},
		{Role: ChatRoleUser, Content: "again"},
	}}
	roles := func(body []byte) (out []string) {
		var w struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		require.NoError(t, json.Unmarshal(body, &w))
		for _, m := range w.Messages {
			out = append(out, m.Role+":"+m.Content)
		}
		return
	}
	folded, err := EncodeRequest(req)
	require.NoError(t, err)
	assert.Equal(t, []string{"system:head", "user:hello", "assistant:hi", "user:System note:\nlate\n\nagain"}, roles(folded))
	same, err := DefaultRoute().Encode(req)
	require.NoError(t, err)
	assert.Equal(t, string(folded), string(same), "the default route is the package's unconditional behaviour")

	inHistory, err := Route{LateSystem: LateSystemInHistory}.Encode(req)
	require.NoError(t, err)
	assert.Equal(t, []string{"system:head", "user:hello", "assistant:hi", "system:late", "user:again"}, roles(inHistory))
}

// The default route takes inline think tags out of the content; a route
// declaring fields only leaves them.
func TestARouteDecidesWhereReasoningComesFrom(t *testing.T) {
	content := "<think>why</think>answer"
	clean, thought := DefaultRoute().reasoningOf(content, "field", "")
	assert.Equal(t, "answer", clean)
	assert.Equal(t, "field\n\nwhy", thought)
	clean, thought = Route{ReasoningEcho: ReasoningEchoFields}.reasoningOf(content, "field", "")
	assert.Equal(t, content, clean)
	assert.Equal(t, "field", thought)
}

func TestAClientCarriesItsRoute(t *testing.T) {
	cli, err := NewClient("http://127.0.0.1:1/v1", "", WithRoute(Route{Id: "local", ContextTokens: 8192}))
	require.NoError(t, err)
	assert.Equal(t, "local", cli.Route().Id)
	assert.Equal(t, int32(8192), cli.Route().ContextTokens)
	plain, err := NewClient("http://127.0.0.1:1/v1", "")
	require.NoError(t, err)
	assert.Equal(t, DefaultRoute(), plain.Route())
}
