package proxy

import (
	"io"
	"strings"
	"testing"

	"switchdev/upstream"
)

func TestSSEChunkHasContentAnthropic(t *testing.T) {
	cases := []struct {
		name string
		data string
		want bool
	}{
		{"text delta", `{"type":"content_block_delta","delta":{"type":"text_delta","text":"hello"}}`, true},
		{"thinking delta", `{"type":"content_block_delta","delta":{"type":"thinking_delta","thinking":"hmm"}}`, true},
		{"tool input", `{"type":"content_block_delta","delta":{"type":"input_json_delta","partial_json":"{\"a\":"}}`, true},
		{"message start only", `{"type":"message_start","message":{}}`, false},
		{"message delta stop", `{"type":"message_delta","delta":{"stop_reason":"end_turn"}}`, false},
		{"ping", `{"type":"ping"}`, false},
	}
	for _, c := range cases {
		if got := sseChunkHasContent(c.data, "anthropic"); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}

func TestPeekSSEAnthropicContent(t *testing.T) {
	// 完整 anthropic 流：message_start -> content_block_start -> content_block_delta(有文本) -> ...
	stream := "event: message_start\ndata: " + `{"type":"message_start","message":{"usage":{"input_tokens":10}}}` + "\n\n" +
		"event: content_block_start\ndata: " + `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}` + "\n\n" +
		"event: content_block_delta\ndata: " + `{"type":"content_block_delta","delta":{"type":"text_delta","text":"hi"}}` + "\n\n"
	rc := io.NopCloser(strings.NewReader(stream))
	peeked, err := peekSSEUntilContent(rc, "anthropic")
	if err != nil {
		t.Fatalf("expected content, got err %v", err)
	}
	rest, _ := io.ReadAll(peeked)
	if !strings.Contains(string(rest), "hi") {
		t.Fatalf("replayed stream missing content: %s", rest)
	}
}

func TestPeekSSEAnthropicEmpty(t *testing.T) {
	// 只有 message_start / message_stop，没有任何输出内容 -> 视为空
	stream := "event: message_start\ndata: " + `{"type":"message_start"}` + "\n\n" +
		"event: message_stop\ndata: " + `{"type":"message_stop"}` + "\n\n"
	rc := io.NopCloser(strings.NewReader(stream))
	_, err := peekSSEUntilContent(rc, "anthropic")
	if err != errSSEEmptyContent {
		t.Fatalf("expected errSSEEmptyContent, got %v", err)
	}
}

func TestIsEmptyUpstreamResponseAnthropicSuccess(t *testing.T) {
	body := []byte(`{"type":"message","role":"assistant","content":[{"type":"text","text":"hello"}],"stop_reason":"end_turn","usage":{"input_tokens":5,"output_tokens":3}}`)
	resp := &upstream.Response{StatusCode: 200, Body: body}
	if isEmptyUpstreamResponse(resp) {
		t.Fatal("anthropic success response should NOT be treated as empty")
	}
}

func TestIsEmptyUpstreamResponseAnthropicEmptyContent(t *testing.T) {
	body := []byte(`{"type":"message","role":"assistant","content":[],"stop_reason":"end_turn"}`)
	resp := &upstream.Response{StatusCode: 200, Body: body}
	if !isEmptyUpstreamResponse(resp) {
		t.Fatal("anthropic empty content array should be treated as empty")
	}
}
