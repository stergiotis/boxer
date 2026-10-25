package openaichat

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json/v2"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/stergiotis/boxer/public/functional/option"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// Streaming (ADR-0286 §SD1): the same request with `stream: true`, read as
// server-sent events, assembled into the response Complete gives. The
// deltas are advisory — what a consumer may show while the answer is
// written — and the assembled response is the only thing that counts.

// Delta is one advance of a streamed answer: new content, new reasoning,
// or the name of a tool call as the provider first names it. At most one
// of the three is set per delta.
type Delta struct {
	Content   string
	Reasoning string
	ToolCall  string
}

// StreamingClientI is a provider that can stream. A [ClientI] that does not
// implement it is called with Complete and gives no deltas.
type StreamingClientI interface {
	ClientI
	// CompleteStream is Complete with onDelta called, on the caller's
	// goroutine, as the answer arrives. The response is assembled from
	// the stream; an endpoint that refuses to stream is called once more
	// without it, and onDelta is then never called.
	CompleteStream(ctx context.Context, req CompletionRequest, onDelta func(Delta)) (resp CompletionResponse, err error)
}

var _ StreamingClientI = (*Client)(nil)

// maxSSELine bounds one event line: a chunk is a few hundred bytes, and a
// tool call's arguments arrive in fragments.
const maxSSELine = 1 << 20

// The stream's chunk shapes: a choice carries a delta rather than a
// message, and a tool call arrives by index in fragments.
type wireStreamChunk struct {
	Id      string             `json:"id"`
	Model   string             `json:"model"`
	Choices []wireStreamChoice `json:"choices"`
	Usage   *wireUsage         `json:"usage,omitempty"`
	Error   *wireErrorBlock    `json:"error,omitempty"`
}

type wireStreamChoice struct {
	Index        int32           `json:"index"`
	Delta        wireStreamDelta `json:"delta"`
	FinishReason string          `json:"finish_reason"`
}

type wireStreamDelta struct {
	Content          string               `json:"content"`
	ReasoningContent string               `json:"reasoning_content"`
	Reasoning        string               `json:"reasoning"`
	ToolCalls        []wireStreamToolCall `json:"tool_calls"`
}

type wireStreamToolCall struct {
	Index    int32                `json:"index"`
	Id       string               `json:"id"`
	Type     string               `json:"type"`
	Function wireToolCallFunction `json:"function"`
}

// CompleteStream implements [StreamingClientI].
func (inst *Client) CompleteStream(ctx context.Context, req CompletionRequest, onDelta func(Delta)) (resp CompletionResponse, err error) {
	if err = validateRequest(req); err != nil {
		return
	}
	if onDelta == nil {
		onDelta = func(Delta) {}
	}
	var body []byte
	body, err = inst.route.encode(req, true)
	if err != nil {
		err = eh.Errorf("encode request: %w", err)
		return
	}
	url := inst.baseUrl + "chat/completions"
	t0 := time.Now()
	var status, attempts int
	if inst.observer != nil {
		defer func() {
			inst.observer(RequestStat{Model: req.ModelId, FinishReason: resp.FinishReason, Status: status, Attempts: attempts,
				Elapsed: time.Since(t0), InputTokens: resp.InputTokens, OutputTokens: resp.OutputTokens, Err: err})
		}()
	}
	var httpResp *http.Response
	var rawBody []byte
	status, httpResp, rawBody, attempts, err = inst.openRetrying(ctx, url, body)
	if err != nil {
		return
	}
	if httpResp == nil {
		// A non-2xx status with its body read. An endpoint that does not
		// stream says so with a 4xx naming the field; it is asked once
		// more for the plain call (SD1).
		if refusesStreaming(status, rawBody) {
			return inst.Complete(ctx, req)
		}
		err = inst.classifyHttpError(ctx, url, status, rawBody)
		return
	}
	defer func() { _ = httpResp.Body.Close() }()
	resp, err = inst.readStream(httpResp.Body, onDelta)
	if err != nil {
		return
	}
	if isIncompleteFinishReason(resp.FinishReason) {
		err = eb.Build().Str("finishReason", resp.FinishReason).Int("outputTokens", int(resp.OutputTokens)).
			Errorf("openaichat: completion stopped early: %w", ErrIncompleteCompletion)
	}
	return
}

// validateRequest is the check Complete and CompleteStream share.
func validateRequest(req CompletionRequest) (err error) {
	if req.ModelId == "" {
		return eh.Errorf("openaichat: ModelId is empty")
	}
	if len(req.Messages) == 0 {
		return eh.Errorf("openaichat: Messages is empty")
	}
	for i, m := range req.Messages {
		if !m.Role.IsValid() {
			return eb.Build().Int("index", i).Uint8("role", uint8(m.Role)).Errorf("openaichat: message has an invalid role")
		}
		if m.Role == ChatRoleTool && m.ToolCallId == "" {
			return eb.Build().Int("index", i).Errorf("openaichat: tool message is missing ToolCallId")
		}
	}
	return
}

// refusesStreaming reads a 4xx as "this endpoint does not stream" when its
// body names the field: the shape OpenAI-compatible servers use for an
// unsupported parameter.
func refusesStreaming(status int, raw []byte) bool {
	if status != http.StatusBadRequest && status != http.StatusUnprocessableEntity && status != http.StatusNotImplemented {
		return false
	}
	return bytes.Contains(bytes.ToLower(raw), []byte("stream"))
}

// openRetrying opens the streaming exchange with the retry policy of the
// plain call, up to the first byte of a 2xx body: a transient status or a
// transport failure before the stream begins is retried, and once the
// stream has begun nothing is. A 2xx returns the open response; a non-2xx
// returns its status and body, read, with resp nil.
func (inst *Client) openRetrying(ctx context.Context, url string, body []byte) (status int, resp *http.Response, raw []byte, attempts int, err error) {
	maxAttempts := max(inst.retry.MaxAttempts, 1)
	for attempt := 1; ; attempt++ {
		attempts = attempt
		var retryAfter time.Duration
		status, resp, raw, retryAfter, err = inst.open(ctx, url, body)
		if resp != nil {
			return
		}
		if attempt >= maxAttempts || !retryable(status, err) {
			return
		}
		timer := time.NewTimer(inst.backoff(attempt, retryAfter))
		select {
		case <-ctx.Done():
			timer.Stop()
			if err == nil {
				err = eb.Build().Str("url", url).Errorf("openaichat: context done during retry backoff: %w", ctx.Err())
			}
			return
		case <-timer.C:
		}
	}
}

// open is one attempt: on a 2xx the response is returned open for the
// stream reader; otherwise its body is read and returned as send does.
func (inst *Client) open(ctx context.Context, url string, body []byte) (status int, resp *http.Response, raw []byte, retryAfter time.Duration, err error) {
	httpReq, rerr := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if rerr != nil {
		err = eh.Errorf("new request: %w: %w", errRequestBuild, rerr)
		return
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	if inst.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+inst.apiKey)
	}
	httpResp, derr := inst.httpClient.Do(httpReq)
	if derr != nil {
		err = eb.Build().Str("url", url).Errorf("http do: %w", derr)
		return
	}
	status = httpResp.StatusCode
	retryAfter = parseRetryAfter(httpResp.Header.Get("Retry-After"))
	if status >= 200 && status < 300 {
		resp = httpResp
		return
	}
	defer func() { _ = httpResp.Body.Close() }()
	reader := io.Reader(httpResp.Body)
	if inst.maxResponseBytes > 0 {
		reader = io.LimitReader(httpResp.Body, inst.maxResponseBytes+1)
	}
	if raw, err = io.ReadAll(reader); err != nil {
		err = eh.Errorf("read body: %w", err)
	}
	return
}

// streamToolCall is one tool call as its fragments accumulate by index.
type streamToolCall struct {
	id, name string
	args     strings.Builder
}

// readStream reads the events and assembles the response. Content and the
// two reasoning fields accumulate as the plain response carries them, so
// the route's reasoning split runs once, over the whole; the deltas hand
// the raw pieces on as they come.
func (inst *Client) readStream(r io.Reader, onDelta func(Delta)) (resp CompletionResponse, err error) {
	if inst.maxResponseBytes > 0 {
		r = io.LimitReader(r, inst.maxResponseBytes+1)
	}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), maxSSELine)
	var content, reasoningContent, reasoning strings.Builder
	var calls []*streamToolCall
	var usage *wireUsage
	var finish, id, model string
	var data bytes.Buffer
	var read int64
	done := false
	flush := func() (stop bool, ferr error) {
		if data.Len() == 0 {
			return
		}
		payload := bytes.TrimSpace(data.Bytes())
		data.Reset()
		if bytes.Equal(payload, []byte("[DONE]")) {
			return true, nil
		}
		var chunk wireStreamChunk
		if ferr = json.Unmarshal(payload, &chunk); ferr != nil {
			ferr = eb.Build().Str("rawSnippet", snippet(string(payload), 256)).Errorf("decode stream chunk: %w", ferr)
			return
		}
		if chunk.Error != nil && chunk.Error.Message != "" {
			ferr = eb.Build().Str("type", chunk.Error.Type).Errorf("openaichat: the stream reported an error: %s", chunk.Error.Message)
			return
		}
		if chunk.Id != "" {
			id = chunk.Id
		}
		if chunk.Model != "" {
			model = chunk.Model
		}
		if chunk.Usage != nil {
			usage = chunk.Usage
		}
		for _, ch := range chunk.Choices {
			if ch.Index != 0 {
				continue
			}
			d := ch.Delta
			if d.Content != "" {
				content.WriteString(d.Content)
				onDelta(Delta{Content: d.Content})
			}
			if d.ReasoningContent != "" {
				reasoningContent.WriteString(d.ReasoningContent)
				onDelta(Delta{Reasoning: d.ReasoningContent})
			}
			if d.Reasoning != "" {
				reasoning.WriteString(d.Reasoning)
				onDelta(Delta{Reasoning: d.Reasoning})
			}
			for _, tc := range d.ToolCalls {
				for int(tc.Index) >= len(calls) {
					calls = append(calls, &streamToolCall{})
				}
				c := calls[tc.Index]
				if tc.Id != "" {
					c.id = tc.Id
				}
				if tc.Function.Name != "" && c.name == "" {
					c.name = tc.Function.Name
					onDelta(Delta{ToolCall: c.name})
				}
				c.args.WriteString(tc.Function.Arguments)
			}
			if ch.FinishReason != "" {
				finish = ch.FinishReason
			}
		}
		return
	}
	for sc.Scan() {
		line := sc.Bytes()
		read += int64(len(line)) + 1
		if inst.maxResponseBytes > 0 && read > inst.maxResponseBytes {
			err = eb.Build().Int64("maxResponseBytes", inst.maxResponseBytes).Errorf("read stream: %w", ErrResponseTooLarge)
			return
		}
		switch {
		case len(line) == 0:
			// A blank line ends the event.
			if done, err = flush(); err != nil || done {
				break
			}
			continue
		case bytes.HasPrefix(line, []byte(":")):
			// A comment, which some servers send as a keep-alive.
			continue
		case bytes.HasPrefix(line, []byte("data:")):
			if data.Len() > 0 {
				data.WriteByte('\n')
			}
			data.Write(bytes.TrimPrefix(bytes.TrimPrefix(line, []byte("data:")), []byte(" ")))
			continue
		default:
			// event:, id:, retry: — the completions stream uses none.
			continue
		}
		break
	}
	if err != nil {
		return
	}
	if !done {
		if serr := sc.Err(); serr != nil {
			err = eh.Errorf("read stream: %w", serr)
			return
		}
		// A stream that ended without [DONE]: the last event may still be
		// pending.
		if _, err = flush(); err != nil {
			return
		}
	}
	if finish == "" && content.Len() == 0 && reasoningContent.Len() == 0 && reasoning.Len() == 0 && len(calls) == 0 {
		err = eh.Errorf("openaichat: the stream carried no choices")
		return
	}
	cleanContent, thought := inst.route.reasoningOf(content.String(), reasoningContent.String(), reasoning.String())
	resp = CompletionResponse{Content: cleanContent, Reasoning: thought, FinishReason: finish, Id: id, Model: model}
	for _, c := range calls {
		resp.ToolCalls = append(resp.ToolCalls, ToolCall{Id: c.id, Name: c.name, Arguments: c.args.String()})
	}
	if usage != nil {
		resp.InputTokens, resp.OutputTokens = usage.PromptTokens, usage.CompletionTokens
		if d := usage.PromptTokensDetails; d != nil && d.CachedTokens != nil {
			resp.CachedInputTokens = option.Some(*d.CachedTokens)
		}
		if d := usage.CompletionTokensDetails; d != nil && d.ReasoningTokens != nil {
			resp.ReasoningTokens = option.Some(*d.ReasoningTokens)
		}
	}
	if finish == "" {
		// A stream that closed mid-answer: the answer is what arrived,
		// and it did not finish normally.
		resp.FinishReason = "length"
	}
	return
}
