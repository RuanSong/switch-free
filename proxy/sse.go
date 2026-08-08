package proxy

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// WriteAnthropicSSE 把完整 Anthropic 响应拆成 SSE 事件流（伪流式）
// 适用于客户端请求 stream:true，但上游返回非流式的场景
func WriteAnthropicSSE(w io.Writer, openaiResp *OpenAIResponse, model string) {
	flusher, canFlush := w.(http.Flusher)
	write := func(event string, data interface{}) {
		jsonData, _ := json.Marshal(data)
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, string(jsonData))
		if canFlush {
			flusher.Flush()
		}
	}

	messageID := fmt.Sprintf("msg_%d", openaiResp.Created)

	// message_start
	write("message_start", map[string]interface{}{
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
				"input_tokens":                openaiResp.Usage.GetPromptTokens(),
				"output_tokens":               0,
				"cache_creation_input_tokens": 0,
				"cache_read_input_tokens":     0,
			},
		},
	})

	choice := &OpenAIChoice{}
	if len(openaiResp.Choices) > 0 {
		choice = &openaiResp.Choices[0]
	}
	msg := choice.Message
	blockIndex := 0

	// tool_use blocks
	if len(msg.ToolCalls) > 0 {
		for _, tc := range msg.ToolCalls {
			write("content_block_start", map[string]interface{}{
				"type":  "content_block_start",
				"index": blockIndex,
				"content_block": map[string]interface{}{
					"type":  "tool_use",
					"id":    tc.ID,
					"name":  tc.Function.Name,
					"input": map[string]interface{}{},
				},
			})
			write("content_block_delta", map[string]interface{}{
				"type":  "content_block_delta",
				"index": blockIndex,
				"delta": map[string]interface{}{
					"type":         "input_json_delta",
					"partial_json": tc.Function.Arguments,
				},
			})
			write("content_block_stop", map[string]interface{}{
				"type":  "content_block_stop",
				"index": blockIndex,
			})
			blockIndex++
		}
	}

	// reasoning_content -> text block（推理模型思维链，如 JoyAI-Code-1.5 输出主要在此）
	if msg.ReasoningContent != "" {
		write("content_block_start", map[string]interface{}{
			"type":          "content_block_start",
			"index":         blockIndex,
			"content_block": map[string]interface{}{"type": "text", "text": ""},
		})
		write("content_block_delta", map[string]interface{}{
			"type":  "content_block_delta",
			"index": blockIndex,
			"delta": map[string]interface{}{"type": "text_delta", "text": msg.ReasoningContent},
		})
		write("content_block_stop", map[string]interface{}{
			"type":  "content_block_stop",
			"index": blockIndex,
		})
		blockIndex++
	}
	// text block
	if msg.Content != nil && *msg.Content != "" {
		write("content_block_start", map[string]interface{}{
			"type":  "content_block_start",
			"index": blockIndex,
			"content_block": map[string]interface{}{
				"type": "text",
				"text": "",
			},
		})
		write("content_block_delta", map[string]interface{}{
			"type":  "content_block_delta",
			"index": blockIndex,
			"delta": map[string]interface{}{
				"type": "text_delta",
				"text": *msg.Content,
			},
		})
		write("content_block_stop", map[string]interface{}{
			"type":  "content_block_stop",
			"index": blockIndex,
		})
		blockIndex++
	}

	// stop_reason
	stopReason := "end_turn"
	if choice.FinishReason == "tool_calls" {
		stopReason = "tool_use"
	} else if choice.FinishReason == "length" {
		stopReason = "max_tokens"
	}

	write("message_delta", map[string]interface{}{
		"type": "message_delta",
		"delta": map[string]interface{}{
			"stop_reason":   stopReason,
			"stop_sequence": nil,
		},
		"usage": map[string]interface{}{
			"output_tokens": openaiResp.Usage.GetCompletionTokens(),
		},
	})
	write("message_stop", map[string]interface{}{
		"type": "message_stop",
	})
}

// WriteOpenAISSE 把完整 OpenAI 响应拆成 SSE 事件流（伪流式）
func WriteOpenAISSE(w io.Writer, openaiResp *OpenAIResponse) {
	flusher, canFlush := w.(http.Flusher)
	write := func(data interface{}) {
		jsonData, _ := json.Marshal(data)
		fmt.Fprintf(w, "data: %s\n\n", string(jsonData))
		if canFlush {
			flusher.Flush()
		}
	}

	// 伪流式：把完整响应拆成 delta 格式
	if len(openaiResp.Choices) > 0 {
		choice := openaiResp.Choices[0]
		deltaChoice := *openaiResp
		deltaChoice.Choices = []OpenAIChoice{{
			Index:        choice.Index,
			Message:      choice.Message, // 作为 delta 发
			FinishReason: choice.FinishReason,
		}}
		write(deltaChoice)
	}

	fmt.Fprintf(w, "data: [DONE]\n\n")
	if canFlush {
		flusher.Flush()
	}
}

// GetPromptTokens / GetCompletionTokens 辅助方法
func (u *OpenAIUsage) GetPromptTokens() int {
	if u == nil {
		return 0
	}
	return u.PromptTokens
}

func (u *OpenAIUsage) GetCompletionTokens() int {
	if u == nil {
		return 0
	}
	return u.CompletionTokens
}
