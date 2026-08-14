package proxy

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// errSSEEmptyContent 表示上游 SSE 流读完都没有任何实际输出（text/reasoning/tool_calls），
// 只有 role 块 + finish_reason，或干脆只有 [DONE]。在降级链里当作可跳过的失败。
var errSSEEmptyContent = errors.New("上游返回空内容流")

// sseChunkHasContent 判断一个 OpenAI SSE data 块是否携带真实输出。
// 只要出现非空 content / reasoning_content，或 tool_calls 带参数/名称，就算有内容。
// 纯 role 块（{"delta":{"role":"assistant"}}）不算。
func sseChunkHasContent(data string) bool {
	var chunk streamOpenAIChunk
	if err := json.Unmarshal([]byte(data), &chunk); err != nil {
		// 不是 JSON（可能是错误/注释/[DONE]），不算有内容
		return false
	}
	if len(chunk.Choices) == 0 {
		return false
	}
	d := chunk.Choices[0].Delta
	if d.Content != "" || d.ReasoningContent != "" {
		return true
	}
	for _, tc := range d.ToolCalls {
		if tc.Function.Name != "" || tc.Function.Arguments != "" {
			return true
		}
	}
	return false
}

// peekSSEUntilContent 预读 SSE 流，直到出现第一块真实输出或流结束。
//   - 有内容：返回一个新的 io.ReadCloser，它先回放已读字节，再接续原始流剩余部分；
//     调用方应改用返回的 reader，行为与原流一致。
//   - 读到 [DONE]/EOF 仍无内容：返回 errSSEEmptyContent（并已 Close 原 body）。
//
// 只缓冲到"第一个内容块"为止，之后纯透传，不影响实时性。
// 注意：必须复用同一个 bufio.Reader 接续读取（它内部可能已预读），
// 不能在预读后直接回到原始 body，否则会丢字节。
func peekSSEUntilContent(body io.ReadCloser) (io.ReadCloser, error) {
	br := bufio.NewReaderSize(body, 64*1024)
	var prefix bytes.Buffer

	for {
		line, err := br.ReadString('\n')
		if line != "" {
			prefix.WriteString(line)
		}
		// 在处理 err 之前先检查这一行是否带内容（最后一个 chunk 可能无换行直接 EOF）
		trimmed := strings.TrimSpace(line)
		hasContentData := false
		if strings.HasPrefix(trimmed, "data:") {
			data := strings.TrimSpace(strings.TrimPrefix(trimmed, "data:"))
			if data == "[DONE]" {
				body.Close()
				return nil, errSSEEmptyContent
			}
			if data != "" && sseChunkHasContent(data) {
				hasContentData = true
			}
		}
		// 单行超过预读缓冲：几乎必然是大 content 块，按有内容放行，交给下游解析
		if err == bufio.ErrTooLong {
			return &peekedSSEReader{prefix: prefix.Bytes(), rest: br, body: body}, nil
		}
		if hasContentData {
			// 命中真实输出：回放已缓冲字节，剩余部分从同一个 br 继续
			return &peekedSSEReader{prefix: prefix.Bytes(), rest: br, body: body}, nil
		}
		if err != nil {
			// EOF 或读错误：流已结束且整段无内容
			body.Close()
			if err != io.EOF {
				return nil, fmt.Errorf("预读 SSE 失败: %w", err)
			}
			return nil, errSSEEmptyContent
		}
	}
}

// peekedSSEReader 先吐出 prefix（预读缓冲），读完后接续 rest（预读用的 bufio.Reader）。
type peekedSSEReader struct {
	prefix []byte
	rest   *bufio.Reader
	body   io.Closer
}

func (r *peekedSSEReader) Read(p []byte) (int, error) {
	if len(r.prefix) > 0 {
		n := copy(p, r.prefix)
		r.prefix = r.prefix[n:]
		return n, nil
	}
	return r.rest.Read(p)
}

func (r *peekedSSEReader) Close() error {
	return r.body.Close()
}
