package proxy

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

// streamOpenAIChunk OpenAI 流式 chunk 解析结构（供 StreamOpenAIToAnthropic / StreamOpenAIPassthrough 共用）
type streamOpenAIChunk struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Created int64  `json:"created"`
	Choices []struct {
		Index int `json:"index"`
		Delta struct {
			Role             string `json:"role"`
			Content          string `json:"content"`
			ReasoningContent string `json:"reasoning_content"`
			ToolCalls []struct {
				Index    int    `json:"index"`
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage *OpenAIUsage `json:"usage"`
}

// StreamOpenAIToAnthropic 读上游 OpenAI SSE 流，边转成 Anthropic SSE 事件写给客户端
// 返回捕获的 usage（供日志）和首字节用时（ms）
func StreamOpenAIToAnthropic(w io.Writer, r io.Reader, model string) (*OpenAIUsage, int64, error) {
	flusher, canFlush := w.(http.Flusher)
	start := time.Now()
	var firstByteMs int64

	writeEvent := func(event string, data interface{}) {
		jsonData, _ := json.Marshal(data)
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, string(jsonData))
		if firstByteMs == 0 {
			firstByteMs = time.Since(start).Milliseconds()
		}
		if canFlush {
			flusher.Flush()
		}
	}

	messageStarted := false
	nextBlockIndex := 0
	textBlockIndex := -1
	reasoningBlockIndex := -1
	toolCallBlocks := map[int]int{} // tc.index -> anthropic block index
	var capturedUsage *OpenAIUsage

	messageID := fmt.Sprintf("msg_%d", time.Now().Unix())

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	var firstDataLines []string // 调试：记录前几行 data（firstByteMs==0 时用于排查）
	var firstParseErr error
	var firstChoicesCount int = -1
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			break
		}
		if len(firstDataLines) < 3 {
			firstDataLines = append(firstDataLines, data)
		}

		var chunk streamOpenAIChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			if firstParseErr == nil {
				firstParseErr = err
			}
			continue
		}
		if firstChoicesCount == -1 {
			firstChoicesCount = len(chunk.Choices)
		}
		if chunk.Model != "" && model == "" {
			model = chunk.Model
		}

		// 首个有效 chunk -> 发 message_start
		if !messageStarted && len(chunk.Choices) > 0 {
			writeEvent("message_start", map[string]interface{}{
				"type": "message_start",
				"message": map[string]interface{}{
					"id":            messageID,
					"type":          "message",
					"role":          "assistant",
					"model":         model,
					"content":       []interface{}{},
					"stop_reason":   nil,
					"stop_sequence": nil,
					"usage": map[string]interface{}{
						"input_tokens":  0,
						"output_tokens": 0,
					},
				},
			})
			messageStarted = true
		}

		if len(chunk.Choices) == 0 {
			if chunk.Usage != nil {
				capturedUsage = chunk.Usage
			}
			continue
		}
		c := chunk.Choices[0]
		delta := c.Delta

		// tool_calls -> tool_use block（按 tc.index 分配 anthropic block index）
		for _, tc := range delta.ToolCalls {
			anthropicIdx, exists := toolCallBlocks[tc.Index]
			if !exists {
				anthropicIdx = nextBlockIndex
				nextBlockIndex++
				toolCallBlocks[tc.Index] = anthropicIdx
				writeEvent("content_block_start", map[string]interface{}{
					"type":  "content_block_start",
					"index": anthropicIdx,
					"content_block": map[string]interface{}{
						"type":  "tool_use",
						"id":    tc.ID,
						"name":  tc.Function.Name,
						"input": map[string]interface{}{},
					},
				})
			}
			if tc.Function.Arguments != "" {
				writeEvent("content_block_delta", map[string]interface{}{
					"type":  "content_block_delta",
					"index": anthropicIdx,
					"delta": map[string]interface{}{
						"type":         "input_json_delta",
						"partial_json": tc.Function.Arguments,
					},
				})
			}
		}

		// reasoning_content -> text block（推理模型思维链）
		if delta.ReasoningContent != "" {
			if reasoningBlockIndex == -1 {
				reasoningBlockIndex = nextBlockIndex
				nextBlockIndex++
				writeEvent("content_block_start", map[string]interface{}{
					"type":          "content_block_start",
					"index":         reasoningBlockIndex,
					"content_block": map[string]interface{}{"type": "text", "text": ""},
				})
			}
			writeEvent("content_block_delta", map[string]interface{}{
				"type":  "content_block_delta",
				"index": reasoningBlockIndex,
				"delta": map[string]interface{}{"type": "text_delta", "text": delta.ReasoningContent},
			})
		}

		// content -> text block
		if delta.Content != "" {
			if textBlockIndex == -1 {
				textBlockIndex = nextBlockIndex
				nextBlockIndex++
				writeEvent("content_block_start", map[string]interface{}{
					"type":          "content_block_start",
					"index":         textBlockIndex,
					"content_block": map[string]interface{}{"type": "text", "text": ""},
				})
			}
			writeEvent("content_block_delta", map[string]interface{}{
				"type":  "content_block_delta",
				"index": textBlockIndex,
				"delta": map[string]interface{}{"type": "text_delta", "text": delta.Content},
			})
		}

		// finish_reason -> stop 所有 block + message_delta + message_stop
		if c.FinishReason != "" {
			toolIndices := make([]int, 0, len(toolCallBlocks))
			for _, idx := range toolCallBlocks {
				toolIndices = append(toolIndices, idx)
			}
			sort.Ints(toolIndices)
			for _, idx := range toolIndices {
				writeEvent("content_block_stop", map[string]interface{}{
					"type": "content_block_stop", "index": idx,
				})
			}
			if reasoningBlockIndex != -1 {
				writeEvent("content_block_stop", map[string]interface{}{
					"type": "content_block_stop", "index": reasoningBlockIndex,
				})
			}
			if textBlockIndex != -1 {
				writeEvent("content_block_stop", map[string]interface{}{
					"type": "content_block_stop", "index": textBlockIndex,
				})
			}

			stopReason := "end_turn"
			if c.FinishReason == "tool_calls" {
				stopReason = "tool_use"
			} else if c.FinishReason == "length" {
				stopReason = "max_tokens"
			}

			if chunk.Usage != nil {
				capturedUsage = chunk.Usage
			}

			writeEvent("message_delta", map[string]interface{}{
				"type": "message_delta",
				"delta": map[string]interface{}{
					"stop_reason":   stopReason,
					"stop_sequence": nil,
				},
				"usage": map[string]interface{}{
					"output_tokens": capturedUsage.GetCompletionTokens(),
				},
			})
			writeEvent("message_stop", map[string]interface{}{
				"type": "message_stop",
			})
		}

		if chunk.Usage != nil {
			capturedUsage = chunk.Usage
		}
	}
	if err := scanner.Err(); err != nil {
		return capturedUsage, firstByteMs, fmt.Errorf("读取 SSE 流失败: %w", err)
	}
	// 没发 message_start 但有 data 行 -> 上游返回了非标准 SSE（如错误 data），记录前几行便于排查
	if firstByteMs == 0 && len(firstDataLines) > 0 {
		snippet := strings.Join(firstDataLines, " | ")
		if len(snippet) > 200 {
			snippet = snippet[:200]
		}
		return capturedUsage, firstByteMs, fmt.Errorf("no message_start, firstChoices=%d, parseErr=%v, first data: %s", firstChoicesCount, firstParseErr, snippet)
	}
	return capturedUsage, firstByteMs, nil
}

// StreamOpenAIPassthrough 透传上游 OpenAI SSE 给客户端（/v1/chat/completions 流式入口）
// 边透传边捕获 usage，返回 usage 和首字节用时（ms）
func StreamOpenAIPassthrough(w io.Writer, r io.Reader) (*OpenAIUsage, int64, error) {
	flusher, canFlush := w.(http.Flusher)
	start := time.Now()
	var firstByteMs int64
	var capturedUsage *OpenAIUsage

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		// 原样透传每一行（保留 SSE 格式，空行也会写出 \n）
		fmt.Fprintln(w, line)
		trimmed := strings.TrimSpace(line)
		if firstByteMs == 0 && strings.HasPrefix(trimmed, "data:") {
			firstByteMs = time.Since(start).Milliseconds()
		}
		if canFlush {
			flusher.Flush()
		}
		// 捕获 usage（在最后一个 chunk）
		if strings.HasPrefix(trimmed, "data:") {
			data := strings.TrimSpace(strings.TrimPrefix(trimmed, "data:"))
			if data != "[DONE]" {
				var chunk streamOpenAIChunk
				if json.Unmarshal([]byte(data), &chunk) == nil && chunk.Usage != nil {
					capturedUsage = chunk.Usage
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return capturedUsage, firstByteMs, fmt.Errorf("读取 SSE 流失败: %w", err)
	}
	return capturedUsage, firstByteMs, nil
}
