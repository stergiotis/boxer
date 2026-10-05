package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func modelList(t *testing.T, body string) (url string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		assert.Equal(t, "Bearer k", r.Header.Get("Authorization"))
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv.URL + "/v1"
}

// The context size is read from the field the server reports it in, for
// the configured model; the loaded size wins over the supported one.
func TestTheContextSizeIsReadFromTheModelList(t *testing.T) {
	url := modelList(t, `{"data":[{"id":"other","max_model_len":1},{"id":"m","context_length":131072,"loaded_context_length":32768}]}`)
	n, field, err := ProbeContextTokens(context.Background(), http.DefaultClient, url+"/", "k", "m")
	require.NoError(t, err)
	assert.EqualValues(t, 32768, n)
	assert.Equal(t, "loaded_context_length", field)

	url = modelList(t, `{"data":[{"id":"file.gguf","meta":{"n_ctx_train":8192}}]}`)
	n, field, err = ProbeContextTokens(context.Background(), http.DefaultClient, url, "k", "m")
	require.NoError(t, err)
	assert.EqualValues(t, 8192, n, "the only entry stands for the model")
	assert.Equal(t, "meta.n_ctx_train", field)

	url = modelList(t, `{"data":[{"id":"m","owned_by":"x"},{"id":"n"}]}`)
	n, _, err = ProbeContextTokens(context.Background(), http.DefaultClient, url, "k", "m")
	require.NoError(t, err)
	assert.Zero(t, n, "a list that names none leaves it unknown")
}

// describe reports the stated size, and the probed one once it answers.
func TestDescribeReportsTheContextSize(t *testing.T) {
	cli, _, _ := serve(t, Config{Endpoint: "http://127.0.0.1:1234/v1", Model: "m", ContextTokens: 4096, Client: &fakeProvider{}})
	d, err := cli.Describe(context.Background())
	require.NoError(t, err)
	assert.EqualValues(t, 4096, d.ContextTokens)
	assert.Equal(t, "BOXER_LLM_CONTEXT_TOKENS", d.ContextSource)

	url := modelList(t, `{"data":[{"id":"m","max_model_len":16384}]}`)
	cli, _, _ = serve(t, Config{Endpoint: url, Model: "m", ApiKey: "k"})
	require.Eventually(t, func() bool {
		d, err = cli.Describe(context.Background())
		return err == nil && d.ContextTokens == 16384
	}, probeWait, 10e6)
	assert.Equal(t, "endpoint: max_model_len", d.ContextSource)
}
