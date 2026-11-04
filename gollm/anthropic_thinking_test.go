// Copyright 2026 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package gollm

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// fakeAnthropic replays one canned body per request and records request bodies.
// Requests are sequential (each iterator is drained before the next), so no lock.
type fakeAnthropic struct {
	bodies []map[string]any
}

func newFakeAnthropic(t *testing.T, resps ...string) *fakeAnthropic {
	t.Helper()
	f := &fakeAnthropic{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var m map[string]any
		_ = json.Unmarshal(raw, &m)
		n := len(f.bodies)
		f.bodies = append(f.bodies, m)
		_, _ = io.WriteString(w, resps[n])
	}))
	t.Cleanup(srv.Close)
	populateEnv(t, "anthropic", srv.URL)
	return f
}

func sse(events ...string) string {
	var b strings.Builder
	for _, e := range events {
		b.WriteString("data: " + e + "\n\n")
	}
	return b.String()
}

const (
	evThinkOmitted = `{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":"","signature":""}}`
	evThinkDelta   = `{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":""}}`
	evSigDelta     = `{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"SIG"}}`
	evStop0        = `{"type":"content_block_stop","index":0}`
	evStop1        = `{"type":"content_block_stop","index":1}`
	evMsgStop      = `{"type":"message_stop"}`
	toolBlk        = `{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"tu1","name":"t","input":{}}}`
	textBlk        = `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`
	textDelta      = `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Let me check."}}`
	errEvent       = `{"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`
)

func msgDelta(reason string) string {
	return `{"type":"message_delta","delta":{"stop_reason":"` + reason + `"},"usage":{"input_tokens":1,"output_tokens":2}}`
}

// reply is a non-streaming response body.
func reply(stopReason, content string) string {
	return `{"id":"m","type":"message","role":"assistant","model":"x","stop_reason":"` + stopReason +
		`","usage":{"input_tokens":1,"output_tokens":2},"content":[` + content + `]}`
}

func newTestChat(t *testing.T) Chat {
	t.Helper()
	c, err := NewAnthropicClient(context.Background(), ClientOptions{})
	if err != nil {
		t.Fatal(err)
	}
	c.httpClient.Timeout = time.Nanosecond // Send and SendStreaming must use streamClient
	chat := c.StartChat("sys", "claude-test")
	if err := chat.SetFunctionDefinitions([]*FunctionDefinition{{Name: "t", Description: "d"}}); err != nil {
		t.Fatal(err)
	}
	return chat
}

func drain(t *testing.T, it ChatResponseIterator, err error) error {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range it {
		if e != nil {
			return e
		}
	}
	return nil
}

// norm re-marshals JSON so key order does not matter; key presence still does.
func norm(t *testing.T, s string) string {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(v)
	return string(b)
}

// lastAssistant returns the JSON of the last assistant message's content in a recorded request.
func lastAssistant(t *testing.T, body map[string]any) string {
	t.Helper()
	msgs := body["messages"].([]any)
	for i := len(msgs) - 1; i >= 0; i-- {
		m := msgs[i].(map[string]any)
		if m["role"] == "assistant" {
			b, _ := json.Marshal(m["content"])
			return string(b)
		}
	}
	t.Fatal("no assistant message in request")
	return ""
}

func toolStream(block0 ...string) string {
	return sse(append(block0, toolBlk,
		`{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"a\":1}"}}`,
		evStop1, msgDelta("tool_use"), evMsgStop)...)
}

const wantTool = `{"type":"tool_use","id":"tu1","name":"t","input":{"a":1}}`

// (1) truncated, refused or failed responses must error, not succeed empty or
// dispatch a cut-off tool_use, and an errored turn is not committed to history.
func TestAnthropicStream_StopErrors(t *testing.T) {
	toolDelta := `{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"a\":"}}`
	cases := map[string]struct {
		stream  string
		wantErr string // "" means no error
	}{
		"thinking only, max_tokens":      {sse(evThinkOmitted, evThinkDelta, evSigDelta, evStop0, msgDelta("max_tokens"), evMsgStop), "max_tokens"},
		"text then cut tool_use":         {sse(textBlk, textDelta, evStop0, toolBlk, toolDelta, evStop1, msgDelta("max_tokens"), evMsgStop), "max_tokens"},
		"text only, max_tokens":          {sse(textBlk, textDelta, evStop0, msgDelta("max_tokens"), evMsgStop), ""},
		"empty text, max_tokens":         {sse(textBlk, evStop0, msgDelta("max_tokens"), evMsgStop), "max_tokens"},
		"text delta before block start":  {sse(textDelta, evStop0, msgDelta("end_turn"), evMsgStop), "before its content_block_start"},
		"refusal, no content":            {sse(msgDelta("refusal"), evMsgStop), "refusal"},
		"refusal after text":             {sse(textBlk, textDelta, evStop0, msgDelta("refusal"), evMsgStop), "refusal"},
		"context exceeded, empty":        {sse(msgDelta("model_context_window_exceeded"), evMsgStop), "model_context_window_exceeded"},
		"context exceeded, cut tool_use": {sse(textBlk, textDelta, evStop0, toolBlk, toolDelta, evStop1, msgDelta("model_context_window_exceeded"), evMsgStop), "filled the model's context window"},
		"error event mid-thinking":       {sse(evThinkOmitted, evThinkDelta, errEvent), "overloaded_error"},
		"EOF mid-thinking":               {sse(evThinkOmitted, evThinkDelta), "message_stop"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			newFakeAnthropic(t, tc.stream)
			chat := newTestChat(t)
			it, err := chat.SendStreaming(context.Background(), "hi")
			e := drain(t, it, err)
			if (tc.wantErr == "") != (e == nil) || (e != nil && !strings.Contains(e.Error(), tc.wantErr)) {
				t.Fatalf("want error containing %q, got %v", tc.wantErr, e)
			}
			if n := len(chat.(*anthropicChat).messages); e != nil && n != 0 {
				t.Fatalf("errored turn committed %d messages", n)
			}
		})
	}
}

// (1b) non-streaming Send: same truncation error, and the failed turn is not
// committed; a truncated reply that ends in text is kept.
func TestAnthropicSend_MaxTokensErrors(t *testing.T) {
	newFakeAnthropic(t, reply("max_tokens", `{"type":"thinking","thinking":"","signature":"SIG"}`),
		reply("max_tokens", `{"type":"text","text":"partial"}`))
	chat := newTestChat(t)
	if _, err := chat.Send(context.Background(), "hi"); err == nil || !strings.Contains(err.Error(), "max_tokens") {
		t.Fatalf("want max_tokens error, got %v", err)
	}
	if n := len(chat.(*anthropicChat).messages); n != 0 {
		t.Fatalf("failed turn committed %d messages", n)
	}
	if _, err := chat.Send(context.Background(), "hi"); err != nil {
		t.Fatal(err)
	}
	if n := len(chat.(*anthropicChat).messages); n != 2 {
		t.Fatalf("kept turn committed %d messages, want 2", n)
	}
}

// (1c) a reply that fails after a tool_result turn is not added to history,
// but the tool_result turn is, so the next request still answers the tool_use.
func TestAnthropic_FailedReplyKeepsToolResult(t *testing.T) {
	done := sse(msgDelta("end_turn"), evMsgStop)
	cases := map[string]struct {
		send  bool
		resps []string // the tool_use reply, the failing reply, the reply to the next turn
	}{
		"stream refusal":     {false, []string{toolStream(), sse(textBlk, textDelta, evStop0, msgDelta("refusal"), evMsgStop), done}},
		"stream error event": {false, []string{toolStream(), sse(textBlk, textDelta, errEvent), done}},
		"stream EOF":         {false, []string{toolStream(), sse(textBlk, textDelta), done}},
		"send max_tokens": {true, []string{reply("tool_use", wantTool),
			reply("max_tokens", `{"type":"thinking","thinking":"","signature":"SIG"}`), reply("end_turn", `{"type":"text","text":"ok"}`)}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFakeAnthropic(t, tc.resps...)
			chat := newTestChat(t)
			send := func(content any) error {
				if tc.send {
					_, err := chat.Send(context.Background(), content)
					return err
				}
				it, err := chat.SendStreaming(context.Background(), content)
				return drain(t, it, err)
			}
			if err := send("hi"); err != nil {
				t.Fatal(err)
			}
			if err := send(FunctionCallResult{ID: "tu1", Name: "t", Result: map[string]any{"ok": true}}); err == nil {
				t.Fatal("want the reply to the tool_result to fail")
			}
			if err := send("next"); err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, m := range f.bodies[2]["messages"].([]any) {
				msg := m.(map[string]any)
				s := msg["role"].(string) + ":"
				for _, b := range msg["content"].([]any) {
					s += b.(map[string]any)["type"].(string) + ","
				}
				got = append(got, s)
			}
			if want := "user:text, assistant:tool_use, user:tool_result, user:text,"; strings.Join(got, " ") != want {
				t.Fatalf("next request messages\n got %s\nwant %s", strings.Join(got, " "), want)
			}
		})
	}
}

// (2) tool turn keeps the thinking block (empty text, signature) before tool_use;
// (2b) redacted_thinking is kept with data; (2c) a no-argument tool_use goes
// back with "input":{}; (2d) thinking + text, no tools, stays [thinking, text];
// (2e) a max_tokens reply with a whole tool_use then an empty text block is
// kept, without the empty text (the fake does not check that the next turn's
// tool_result matches a tool_use).
func TestAnthropicStream_History(t *testing.T) {
	cases := map[string]struct{ stream, want string }{
		"thinking": {
			toolStream(evThinkOmitted, evThinkDelta, evSigDelta, evStop0),
			`[{"type":"thinking","thinking":"","signature":"SIG"},` + wantTool + `]`,
		},
		"thinking text": {
			toolStream(evThinkOmitted,
				`{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"ab"}}`,
				`{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"cd"}}`,
				evSigDelta, evStop0),
			`[{"type":"thinking","thinking":"abcd","signature":"SIG"},` + wantTool + `]`,
		},
		"redacted": {
			toolStream(`{"type":"content_block_start","index":0,"content_block":{"type":"redacted_thinking","data":"D"}}`, evStop0),
			`[{"type":"redacted_thinking","data":"D"},` + wantTool + `]`,
		},
		"no-arg tool": {
			sse(toolBlk, evStop1, msgDelta("tool_use"), evMsgStop),
			`[{"type":"tool_use","id":"tu1","name":"t","input":{}}]`,
		},
		"thinking then text": {
			sse(evThinkOmitted, evThinkDelta, evSigDelta, evStop0,
				`{"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}`,
				`{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"hello"}}`,
				evStop1, msgDelta("end_turn"), evMsgStop),
			`[{"type":"thinking","thinking":"","signature":"SIG"},{"type":"text","text":"hello"}]`,
		},
		"tool then empty text, max_tokens": {
			sse(toolBlk, evStop1, `{"type":"content_block_start","index":2,"content_block":{"type":"text","text":""}}`, msgDelta("max_tokens"), evMsgStop),
			`[{"type":"tool_use","id":"tu1","name":"t","input":{}}]`,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFakeAnthropic(t, tc.stream, sse(textBlk, evStop0, msgDelta("end_turn"), evMsgStop))
			chat := newTestChat(t)
			it, err := chat.SendStreaming(context.Background(), "hi")
			if e := drain(t, it, err); e != nil {
				t.Fatal(e)
			}
			it, err = chat.SendStreaming(context.Background(), FunctionCallResult{ID: "tu1", Name: "t", Result: map[string]any{"ok": true}})
			if e := drain(t, it, err); e != nil {
				t.Fatal(e)
			}
			if got := lastAssistant(t, f.bodies[1]); got != norm(t, tc.want) {
				t.Fatalf("assistant message\n got %s\nwant %s", got, tc.want)
			}
			if n := len(chat.(*anthropicChat).messages); n != 3 { // hi, reply, tool_result (the empty reply adds nothing)
				t.Fatalf("history has %d messages, want 3", n)
			}
		})
	}
}

// (3) default and env override of max_tokens.
func TestAnthropicMaxTokens(t *testing.T) {
	for _, tc := range []struct {
		env  string
		want float64
	}{{"", 16384}, {"2048", 2048}, {"junk", 16384}} {
		f := newFakeAnthropic(t, sse(msgDelta("end_turn"), evMsgStop))
		t.Setenv("ANTHROPIC_MAX_TOKENS", tc.env)
		it, err := newTestChat(t).SendStreaming(context.Background(), "hi")
		if e := drain(t, it, err); e != nil {
			t.Fatal(e)
		}
		if got := f.bodies[0]["max_tokens"]; got != tc.want {
			t.Fatalf("env %q: max_tokens=%v want %v", tc.env, got, tc.want)
		}
	}
}

// (4) non-streaming Send echoes thinking (with signature) and tool_use; (4b) as
// in (2e), a max_tokens reply with a whole tool_use then an empty text block
// goes back without the empty text; (4c) the empty end_turn reply to the
// tool_result commits only the user turn, as in streaming.
func TestAnthropicSend_History(t *testing.T) {
	thinkTool := `{"type":"thinking","thinking":"","signature":"SIG"},` + wantTool
	cases := map[string]struct{ resp, want string }{
		"thinking":                         {reply("tool_use", thinkTool), `[` + thinkTool + `]`},
		"tool then empty text, max_tokens": {reply("max_tokens", wantTool+`,{"type":"text","text":""}`), `[` + wantTool + `]`},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFakeAnthropic(t, tc.resp, reply("end_turn", `{"type":"text","text":""}`))
			chat := newTestChat(t)
			if _, err := chat.Send(context.Background(), "hi"); err != nil {
				t.Fatal(err)
			}
			if _, err := chat.Send(context.Background(), FunctionCallResult{ID: "tu1", Name: "t", Result: map[string]any{}}); err != nil {
				t.Fatal(err)
			}
			if got := lastAssistant(t, f.bodies[1]); got != norm(t, tc.want) {
				t.Fatalf("assistant message\n got %s\nwant %s", got, tc.want)
			}
			if n := len(chat.(*anthropicChat).messages); n != 3 { // hi, reply, tool_result
				t.Fatalf("history has %d messages, want 3", n)
			}
		})
	}
}

// (5) a stream that goes silent after its headers is closed after streamIdle,
// and so is one whose iterator the caller never runs, and an error response
// whose body stalls; one that keeps sending for longer than streamIdle
// completes, and so does a prompt one whose consumer spends longer than
// streamIdle in a yield.
func TestAnthropicStream_IdleTimeout(t *testing.T) {
	cases := map[string]struct {
		silent, slowConsumer, unread bool
		status                       int // sent before the body, if not 0
	}{
		"silent":           {silent: true},
		"slow but sending": {},
		"slow consumer":    {slowConsumer: true},
		"unread":           {silent: true, unread: true},
		"stalled 500 body": {silent: true, status: http.StatusInternalServerError},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			hungUp := make(chan struct{}, 1)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				if tc.status != 0 {
					w.WriteHeader(tc.status)
				}
				_, _ = io.WriteString(w, sse(`{"type":"ping"}`)) // so "silent" stalls on a later read, not the first
				w.(http.Flusher).Flush()
				switch {
				case tc.silent:
					select { // the client hangs up; the timer only ends a run without the watchdog
					case <-r.Context().Done():
						hungUp <- struct{}{}
					case <-time.After(3 * time.Second):
					}
					return
				case tc.slowConsumer: // one text chunk now, the rest 50 ms later
					_, _ = io.WriteString(w, sse(textBlk, textDelta))
					w.(http.Flusher).Flush()
					time.Sleep(50 * time.Millisecond)
				default:
					for range 12 { // 300 ms in all, 25 ms apart
						time.Sleep(25 * time.Millisecond)
						_, _ = io.WriteString(w, sse(`{"type":"ping"}`))
						w.(http.Flusher).Flush()
					}
				}
				_, _ = io.WriteString(w, sse(msgDelta("end_turn"), evMsgStop))
			}))
			t.Cleanup(srv.Close)
			populateEnv(t, "anthropic", srv.URL)
			chat := newTestChat(t)
			chat.(*anthropicChat).client.streamIdle = 200 * time.Millisecond
			start := time.Now()
			it, err := chat.SendStreaming(context.Background(), "hi")
			if tc.status != 0 { // SendStreaming reads an error body before it returns
				var apiErr *APIError
				if !errors.As(err, &apiErr) || apiErr.StatusCode != tc.status {
					t.Fatalf("got error %v, want an APIError with status %d", err, tc.status)
				}
				if took := time.Since(start); took > 2*time.Second {
					t.Fatalf("stalled %d body returned after %v, want about 200ms", tc.status, took)
				}
				select {
				case <-hungUp:
				case <-time.After(2 * time.Second):
					t.Fatal("stalled error body still open; want closed after streamIdle (200ms)")
				}
				return
			}
			if tc.unread { // the iterator is dropped: no client Timeout bounds the body
				if err != nil {
					t.Fatal(err)
				}
				select {
				case <-hungUp:
				case <-time.After(2 * time.Second):
					t.Fatal("unread stream still open 2s after its headers; want closed after streamIdle (200ms)")
				}
				return
			}
			if tc.slowConsumer { // the consumer spends 2x streamIdle in its first yield
				next := it
				it = func(yield func(ChatResponse, error) bool) {
					first := true
					next(func(r ChatResponse, e error) bool {
						if first {
							first = false
							time.Sleep(400 * time.Millisecond)
						}
						return yield(r, e)
					})
				}
			}
			if e := drain(t, it, err); tc.silent != (e != nil) {
				t.Fatalf("silent=%v: got error %v", tc.silent, e)
			}
			if took := time.Since(start); tc.silent && took > 2*time.Second {
				t.Fatalf("silent stream ended after %v, want about 200ms", took)
			}
		})
	}
}
