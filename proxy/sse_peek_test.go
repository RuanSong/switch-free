package proxy

import (
	"errors"
	"io"
	"strings"
	"testing"
)

func readAll(t *testing.T, r io.Reader) string {
	t.Helper()
	b, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return string(b)
}

// 空流（仅 role + finish_reason + [DONE]）应被判定为空并跳过。
func TestPeekSSEEmptyContent(t *testing.T) {
	stream := "data: {\"choices\":[{\"delta\":{\"role\":\"assistant\"}}]}\n" +
		"data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n" +
		"data: [DONE]\n"
	rc := io.NopCloser(strings.NewReader(stream))
	_, err := peekSSEUntilContent(rc, "")
	if !errors.Is(err, errSSEEmptyContent) {
		t.Fatalf("want errSSEEmptyContent, got %v", err)
	}
}

// 只有 [DONE]，也是空流。
func TestPeekSSEOnlyDone(t *testing.T) {
	rc := io.NopCloser(strings.NewReader("data: [DONE]\n"))
	_, err := peekSSEUntilContent(rc, "")
	if !errors.Is(err, errSSEEmptyContent) {
		t.Fatalf("want errSSEEmptyContent, got %v", err)
	}
}

// 有真实 content：返回回放 reader，且回放内容 + 剩余流完整无损。
func TestPeekSSEWithContent(t *testing.T) {
	// role 块（空）→ content 块（命中）→ 后续块 + [DONE]
	stream := "data: {\"choices\":[{\"delta\":{\"role\":\"assistant\"}}]}\n" +
		"data: {\"choices\":[{\"delta\":{\"content\":\"你好\"}}]}\n" +
		"data: {\"choices\":[{\"delta\":{\"content\":\"世界\"}}]}\n" +
		"data: [DONE]\n"
	rc := io.NopCloser(strings.NewReader(stream))
	peeked, err := peekSSEUntilContent(rc, "")
	if err != nil {
		t.Fatalf("want content, got err: %v", err)
	}
	out := readAll(t, peeked)
	if out != stream {
		t.Fatalf("replayed stream mismatch.\nwant:\n%q\n got:\n%q", stream, out)
	}
	if !strings.Contains(out, "你好") || !strings.Contains(out, "世界") {
		t.Fatalf("replayed stream missing content: %q", out)
	}
}

// reasoning_content 也算有内容。
func TestPeekSSEReasoningCounts(t *testing.T) {
	stream := "data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"思考中\"}}]}\n" +
		"data: [DONE]\n"
	rc := io.NopCloser(strings.NewReader(stream))
	peeked, err := peekSSEUntilContent(rc, "")
	if err != nil {
		t.Fatalf("reasoning should count as content, got %v", err)
	}
	if !strings.Contains(readAll(t, peeked), "思考中") {
		t.Fatal("replayed stream missing reasoning content")
	}
}

// tool_calls 带 name/arguments 也算有内容。
func TestPeekSSEToolCallCounts(t *testing.T) {
	stream := "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"name\":\"foo\",\"arguments\":\"\"}}]}}]}\n" +
		"data: [DONE]\n"
	rc := io.NopCloser(strings.NewReader(stream))
	peeked, err := peekSSEUntilContent(rc, "")
	if err != nil {
		t.Fatalf("tool_call should count as content, got %v", err)
	}
	_ = peeked.Close()
}

// 最后一个 content 块没有结尾换行、直接 EOF：仍应识别为有内容并完整回放。
func TestPeekSSENoTrailingNewline(t *testing.T) {
	stream := "data: {\"choices\":[{\"delta\":{\"content\":\"尾块\"}}]}"
	rc := io.NopCloser(strings.NewReader(stream))
	peeked, err := peekSSEUntilContent(rc, "")
	if err != nil {
		t.Fatalf("want content on final chunk, got %v", err)
	}
	if !strings.Contains(readAll(t, peeked), "尾块") {
		t.Fatal("replayed stream missing final chunk")
	}
}
