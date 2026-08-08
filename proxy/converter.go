package proxy

import (
	"encoding/json"
	"fmt"
	"strings"
)

// AnthropicToOpenAI 把 Anthropic /v1/messages 请求体转成 OpenAI Chat Completions 格式（JoyCode 用）
// 业务字段（tenant/userId/client 等）由 JoyCode 上游适配器在发送前注入
func AnthropicToOpenAI(body *AnthropicRequest) (*OpenAIRequest, bool) {
	requestedModel := body.Model
	resolvedModel := ResolveModel(requestedModel)
	isAuto := requestedModel == "" || strings.ToLower(requestedModel) == "auto"

	messages, tools := anthropicMessagesToOpenAI(body)
	jcMeta := JoyCodeModelByID[resolvedModel]

	maxTokens := ClampMaxTokens(body.MaxTokens, 0)
	if jcMeta != nil {
		maxTokens = ClampMaxTokens(body.MaxTokens, jcMeta.OutputMaxTokens)
	}

	openai := &OpenAIRequest{
		Model:     resolvedModel,
		Messages:  messages,
		Stream:    false, // 上游统一非流式
		MaxTokens: maxTokens,
	}

	if body.Temperature != nil {
		openai.Temperature = body.Temperature
	}
	if body.TopP != nil {
		openai.TopP = body.TopP
	}
	if len(body.StopSequences) > 0 {
		openai.Stop = body.StopSequences
	}
	if tools != nil {
		openai.Tools = tools
	}

	return openai, isAuto
}

// AnthropicToOpenAIDeveco 把 Anthropic 请求转成 DevEco 的 OpenAI body（不注入业务字段）
func AnthropicToOpenAIDeveco(body *AnthropicRequest) *OpenAIRequest {
	requestedModel := body.Model
	resolvedModel := ResolveModel(requestedModel)
	devecoModel := DevEcoModelByID[resolvedModel]
	if devecoModel == nil {
		devecoModel = &DevEcoModels[0]
	}

	messages, tools := anthropicMessagesToOpenAI(body)

	maxTokens := 4096
	if body.MaxTokens > 0 {
		maxTokens = body.MaxTokens
	}
	if devecoModel.Output > 0 && maxTokens > devecoModel.Output {
		maxTokens = devecoModel.Output
	}

	openai := &OpenAIRequest{
		Model:     devecoModel.Upstream,
		Messages:  messages,
		Stream:    false,
		MaxTokens: maxTokens,
	}

	if body.Temperature != nil {
		openai.Temperature = body.Temperature
	}
	if body.TopP != nil {
		openai.TopP = body.TopP
	}
	if len(body.StopSequences) > 0 {
		openai.Stop = body.StopSequences
	}
	if tools != nil {
		openai.Tools = tools
	}
	return openai
}

// AnthropicToOpenAIOpencode 把 Anthropic 请求转成 OpenCode 的 OpenAI body
func AnthropicToOpenAIOpencode(body *AnthropicRequest) *OpenAIRequest {
	requestedModel := body.Model
	resolvedModel := ResolveModel(requestedModel)

	messages, tools := anthropicMessagesToOpenAI(body)
	ocMeta := OpenCodeModelByID[resolvedModel]

	maxTokens := 4096
	if body.MaxTokens > 0 {
		maxTokens = body.MaxTokens
	}
	if ocMeta != nil && ocMeta.Output > 0 && maxTokens > ocMeta.Output {
		maxTokens = ocMeta.Output
	}

	openai := &OpenAIRequest{
		Model:     resolvedModel,
		Messages:  messages,
		Stream:    false,
		MaxTokens: maxTokens,
	}

	if body.Temperature != nil {
		openai.Temperature = body.Temperature
	}
	if body.TopP != nil {
		openai.TopP = body.TopP
	}
	if len(body.StopSequences) > 0 {
		openai.Stop = body.StopSequences
	}
	if tools != nil {
		openai.Tools = tools
	}
	return openai
}

// AnthropicToOpenAIWorkbuddy 把 Anthropic 请求转成 WorkBuddy 的 OpenAI body
// model 去掉 wb/ 前缀；stream 设 false（WorkBuddy 上游强制 stream:true，由 Call 内部覆盖）
func AnthropicToOpenAIWorkbuddy(body *AnthropicRequest) *OpenAIRequest {
	requestedModel := body.Model
	resolvedModel := ResolveModel(requestedModel)

	messages, tools := anthropicMessagesToOpenAI(body)
	wbMeta := WorkBuddyModelByID[resolvedModel]

	maxTokens := 4096
	if body.MaxTokens > 0 {
		maxTokens = body.MaxTokens
	}
	if wbMeta != nil && wbMeta.Output > 0 && maxTokens > wbMeta.Output {
		maxTokens = wbMeta.Output
	}

	openai := &OpenAIRequest{
		Model:     stripWbPrefix(resolvedModel),
		Messages:  messages,
		Stream:    false,
		MaxTokens: maxTokens,
	}

	if body.Temperature != nil {
		openai.Temperature = body.Temperature
	}
	if body.TopP != nil {
		openai.TopP = body.TopP
	}
	if len(body.StopSequences) > 0 {
		openai.Stop = body.StopSequences
	}
	if tools != nil {
		openai.Tools = tools
	}
	return openai
}

// anthropicMessagesToOpenAI 共用：Anthropic body 的 messages/system/tools 转成 OpenAI 的 messages + tools
func anthropicMessagesToOpenAI(body *AnthropicRequest) ([]OpenAIMessage, []OpenAITool) {
	var messages []OpenAIMessage

	// system 字段 → 第一条 system message
	if len(body.System) > 0 {
		var sysText string
		// 尝试作为 string 解析
		var sysStr string
		if err := json.Unmarshal(body.System, &sysStr); err == nil {
			sysText = sysStr
		} else {
			// 作为 []block 解析
			var blocks []AnthropicSystemBlock
			if err := json.Unmarshal(body.System, &blocks); err == nil {
				var parts []string
				for _, b := range blocks {
					if b.Text != "" {
						parts = append(parts, b.Text)
					}
				}
				sysText = strings.Join(parts, "\n")
			}
		}
		if sysText != "" {
			messages = append(messages, OpenAIMessage{Role: "system", Content: &sysText})
		}
	}

	// Anthropic messages → OpenAI messages
	for _, msg := range body.Messages {
		role := msg.Role
		if role == "assistant" {
			// keep
		} else {
			role = "user"
		}

		// 尝试作为 string 解析
		var contentStr string
		if err := json.Unmarshal(msg.Content, &contentStr); err == nil {
			messages = append(messages, OpenAIMessage{Role: role, Content: &contentStr})
			continue
		}

		// 作为 []block 解析
		var blocks []AnthropicContentBlock
		if err := json.Unmarshal(msg.Content, &blocks); err != nil {
			// fallback: 直接用 raw string
			raw := string(msg.Content)
			messages = append(messages, OpenAIMessage{Role: role, Content: &raw})
			continue
		}

		var textParts []string
		var toolCalls []OpenAIToolCall
		var toolResults []AnthropicContentBlock

		for _, block := range blocks {
			switch block.Type {
			case "text":
				if block.Text != "" {
					textParts = append(textParts, block.Text)
				}
			case "tool_use":
				toolCalls = append(toolCalls, OpenAIToolCall{
					ID:   block.ID,
					Type: "function",
					Function: OpenAIFunctionCall{
						Name:      block.Name,
						Arguments: string(block.Input),
					},
				})
			case "tool_result":
				toolResults = append(toolResults, block)
			}
		}

		if len(toolResults) > 0 {
			// tool_result → role:tool message
			var resultTexts []string
			var toolUseID string
			for _, tr := range toolResults {
				if tr.ToolUseID != "" {
					toolUseID = tr.ToolUseID
				}
				if tr.Content != nil {
					var c string
					if err := json.Unmarshal(tr.Content, &c); err == nil {
						resultTexts = append(resultTexts, c)
					} else {
						resultTexts = append(resultTexts, string(tr.Content))
					}
				}
			}
			content := strings.Join(resultTexts, "\n")
			messages = append(messages, OpenAIMessage{
				Role:       "tool",
				Content:    &content,
				ToolCallID: &toolUseID,
			})
		} else if len(toolCalls) > 0 {
			var content *string
			if len(textParts) > 0 {
				t := strings.Join(textParts, "")
				content = &t
			}
			messages = append(messages, OpenAIMessage{
				Role:      role,
				Content:   content,
				ToolCalls: toolCalls,
			})
		} else {
			t := strings.Join(textParts, "")
			messages = append(messages, OpenAIMessage{Role: role, Content: &t})
		}
	}

	// tools 转换
	var openaiTools []OpenAITool
	if len(body.Tools) > 0 {
		for _, t := range body.Tools {
			params := t.InputSchema
			if len(params) == 0 {
				params = json.RawMessage(`{"type":"object","properties":{}}`)
			}
			openaiTools = append(openaiTools, OpenAITool{
				Type: "function",
				Function: OpenAIFunction{
					Name:        t.Name,
					Description: t.Description,
					Parameters:  params,
				},
			})
		}
	}

	return messages, openaiTools
}

// OpenAIToAnthropic OpenAI Chat Completions 响应 → Anthropic Messages 响应
func OpenAIToAnthropic(oai *OpenAIResponse, reqID string) *AnthropicResponse {
	choice := &OpenAIChoice{}
	if len(oai.Choices) > 0 {
		choice = &oai.Choices[0]
	}
	msg := choice.Message

	var content []AnthropicContentBlock

	// tool_calls → tool_use blocks
	if len(msg.ToolCalls) > 0 {
		for _, tc := range msg.ToolCalls {
			content = append(content, AnthropicContentBlock{
				Type:  "tool_use",
				ID:    tc.ID,
				Name:  tc.Function.Name,
				Input: json.RawMessage(tc.Function.Arguments),
			})
		}
		if msg.Content != nil && *msg.Content != "" {
			content = append(content, AnthropicContentBlock{
				Type: "text",
				Text: *msg.Content,
			})
		}
	} else if msg.Content != nil && *msg.Content != "" {
		content = append(content, AnthropicContentBlock{
			Type: "text",
			Text: *msg.Content,
		})
	}

	stopReason := "end_turn"
	if choice.FinishReason == "tool_calls" {
		stopReason = "tool_use"
	} else if choice.FinishReason == "length" {
		stopReason = "max_tokens"
	}

	usage := AnthropicUsage{}
	if oai.Usage != nil {
		usage.InputTokens = oai.Usage.PromptTokens
		usage.OutputTokens = oai.Usage.CompletionTokens
	}

	model := oai.Model
	if model == "" {
		model = AutoModel
	}

	return &AnthropicResponse{
		ID:         fmt.Sprintf("msg_%s", reqID),
		Type:       "message",
		Role:       "assistant",
		Model:      model,
		Content:    content,
		StopReason: stopReason,
		Usage:      usage,
	}
}

// SafeJSONParse 安全解析 JSON
func SafeJSONParse(data []byte, v interface{}) {
	_ = json.Unmarshal(data, v)
}