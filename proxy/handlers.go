package proxy

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"switchfree/creds"
)

// 请求/响应体记录上限（防日志膨胀）
const maxLogBodySize = 4096

// truncateBody 截断请求/响应体到上限，超出加省略号
func truncateBody(s string) string {
	if len(s) > maxLogBodySize {
		return s[:maxLogBodySize] + "...(truncated)"
	}
	return s
}

// makeLogEntry 构造日志条目（公共字段）
func (s *Server) makeLogEntry(model, up, status string, code int, duration int64, errMsg, method, path string, stream bool, reqBody, respBody string) *LogEntry {
	return &LogEntry{
		Model:        model,
		Upstream:     up,
		Status:       status,
		Code:         code,
		Duration:     duration,
		ErrorMsg:     errMsg,
		Method:       method,
		Path:         path,
		Stream:       stream,
		RequestBody:  truncateBody(reqBody),
		ResponseBody: truncateBody(respBody),
	}
}

// enrichUsage 从响应体提取 token 用量 + 真实模型，计算费用
func (s *Server) enrichUsage(entry *LogEntry, respBody []byte, requestedModel string) {
	if len(respBody) == 0 {
		return
	}
	var oai OpenAIResponse
	if err := json.Unmarshal(respBody, &oai); err != nil {
		return
	}
	if oai.Usage != nil {
		entry.InputTokens = oai.Usage.PromptTokens
		entry.OutputTokens = oai.Usage.CompletionTokens

		// usage 合理性钳制：某些上游（如 OpenCode Zen）偶发返回远超 context 上限的 inputTokens
		// 明显异常时清零，避免污染今日统计 / 趋势图 / 费率
		limit := modelContextLimit(ResolveModel(requestedModel))
		if limit > 0 && entry.InputTokens > limit {
			entry.InputTokens = 0
		}

		// 缓存命中 token（两种格式兼容）
		if oai.Usage.PromptTokensDetails.CachedTokens > 0 {
			entry.CacheHitTokens = oai.Usage.PromptTokensDetails.CachedTokens
		} else if oai.Usage.CacheReadInputTokens > 0 {
			entry.CacheHitTokens = oai.Usage.CacheReadInputTokens
		}
	}
	if oai.Model != "" {
		entry.RealModel = oai.Model
	}

	// 计算费用（用自有费率库）
	// 优先用 resolve 后的请求模型（auto -> glm-5.1，Kimi-K2.6-agent -> 归一化匹配 kimi-k2.6）
	// 查不到再用真实模型（费率表可能是标准名）
	if s.Pricing != nil {
		lookupModel := ResolveModel(requestedModel)
		cost, price := s.Pricing.CalculateCost(lookupModel, entry.InputTokens, entry.OutputTokens)
		if price == nil && entry.RealModel != "" && entry.RealModel != lookupModel {
			cost2, price2 := s.Pricing.CalculateCost(entry.RealModel, entry.InputTokens, entry.OutputTokens)
			if price2 != nil {
				cost, price = cost2, price2
			}
		}
		if price != nil {
			entry.Cost = cost
			entry.CostText = fmt.Sprintf("%s", price.DisplayName)
		}
	}
}

// logRequest 记录日志（含用量/费用富化）
func (s *Server) logRequest(entry *LogEntry, respBody []byte, requestedModel string) {
	s.enrichUsage(entry, respBody, requestedModel)
	s.recordLog(entry)
}

// handleAnthropicMessages Anthropic /v1/messages 入口
func (s *Server) handleAnthropicMessages(w http.ResponseWriter, r *http.Request) {
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	var body AnthropicRequest
	if err := json.Unmarshal(raw, &body); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("bad json"))
		return
	}

	stream := body.Stream
	requestedModel := body.Model
	if requestedModel == "" {
		requestedModel = "auto"
	}

	start := time.Now()
	resp, upName, usedModel, err := s.callUpstreamAnthropic(r.Context(), &body)
	duration := time.Since(start).Milliseconds()
	reqBodyStr := string(raw)

	if err != nil {
		var credErr *creds.CredentialsError
		if errors.As(err, &credErr) {
			s.recordLog(s.makeLogEntry(requestedModel, upName, "auth_error", 0, duration, credErr.Error(), "POST", "/v1/messages", stream, reqBodyStr, ""))
			writeAnthropicError(w, http.StatusBadGateway, "authentication_error", credErr.Error())
		} else {
			s.recordLog(s.makeLogEntry(requestedModel, upName, "error", 0, duration, err.Error(), "POST", "/v1/messages", stream, reqBodyStr, ""))
			writeAnthropicError(w, http.StatusBadGateway, "connection_error", err.Error())
		}
		return
	}

	trimmed := strings.TrimSpace(string(resp.Body))

	// 检测响应类型（成功 / 上游错误 / 不可解析）
	var generic map[string]interface{}
	parseErr := json.Unmarshal([]byte(trimmed), &generic)
	hasChoices := parseErr == nil && generic["choices"] != nil

	if !hasChoices {
		// 检查是否为错误响应
		hasErrorField := parseErr == nil && generic["error"] != nil
		hasCode := parseErr == nil && generic["code"] != nil
		hasErrorCode := parseErr == nil && generic["errorCode"] != nil

		if !hasErrorField && !hasCode && !hasErrorCode {
			// 不可解析
			snippet := trimmed
			if len(snippet) > 200 {
				snippet = snippet[:200]
			}
			s.recordLog(s.makeLogEntry(requestedModel, upName, "error", resp.StatusCode, duration, "unparsable upstream", "POST", "/v1/messages", stream, reqBodyStr, trimmed))
			writeAnthropicError(w, http.StatusBadGateway, "upstream_error", "unparsable upstream: "+snippet)
			return
		}

		// 上游错误
		errMsg, errCode := extractUpstreamError([]byte(trimmed))
		isDenied := strings.Contains(errMsg, "访问受限") || strings.Contains(strings.ToLower(errCode), "denied")
		fmt.Printf("[switch-free] 上游错误: [%s] %s\n", errCode, errMsg)
		s.recordLog(s.makeLogEntry(requestedModel, upName, "error", resp.StatusCode, duration, fmt.Sprintf("[%s] %s", errCode, errMsg), "POST", "/v1/messages", stream, reqBodyStr, trimmed))
		errType := "upstream_error"
		if isDenied {
			errType = "permission_error"
		}
		writeAnthropicError(w, http.StatusBadGateway, errType, fmt.Sprintf("[%s] %s", errCode, errMsg))
		return
	}

	// 成功响应（富化用量/费用/真实模型后记录；model 存请求模型，usedModel 存实际模型）
	entry := s.makeLogEntry(requestedModel, upName, "success", resp.StatusCode, duration, "", "POST", "/v1/messages", stream, reqBodyStr, trimmed)
	entry.UsedModel = usedModel
	s.logRequest(entry, resp.Body, requestedModel)

	var parsed OpenAIResponse
	if err := json.Unmarshal([]byte(trimmed), &parsed); err != nil {
		writeAnthropicError(w, http.StatusBadGateway, "upstream_error", "parse success response failed")
		return
	}

	if stream {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		model := parsed.Model
		if model == "" {
			model = body.Model
		}
		WriteAnthropicSSE(w, &parsed, model)
	} else {
		ant := OpenAIToAnthropic(&parsed, resp.ReqID)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ant)
	}
}

// handleOpenAIChatCompletions OpenAI /v1/chat/completions 直通入口
func (s *Server) handleOpenAIChatCompletions(w http.ResponseWriter, r *http.Request) {
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	var body map[string]interface{}
	if err := json.Unmarshal(raw, &body); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("bad json"))
		return
	}

	stream, _ := body["stream"].(bool)
	requestedModel, _ := body["model"].(string)
	if requestedModel == "" {
		requestedModel = "auto"
	}

	start := time.Now()
	resp, upName, usedModel, err := s.callUpstreamOpenAI(r.Context(), body)
	duration := time.Since(start).Milliseconds()
	reqBodyStr := string(raw)

	if err != nil {
		var credErr *creds.CredentialsError
		if errors.As(err, &credErr) {
			s.recordLog(s.makeLogEntry(requestedModel, upName, "auth_error", 0, duration, credErr.Error(), "POST", "/v1/chat/completions", stream, reqBodyStr, ""))
			writeOpenAIError(w, http.StatusBadGateway, credErr.Error())
		} else {
			s.recordLog(s.makeLogEntry(requestedModel, upName, "error", 0, duration, err.Error(), "POST", "/v1/chat/completions", stream, reqBodyStr, ""))
			writeOpenAIError(w, http.StatusBadGateway, err.Error())
		}
		return
	}

	// 成功：model 存请求模型，usedModel 存实际模型
	entry := s.makeLogEntry(requestedModel, upName, "success", resp.StatusCode, duration, "", "POST", "/v1/chat/completions", stream, reqBodyStr, string(resp.Body))
	entry.UsedModel = usedModel
	s.logRequest(entry, resp.Body, requestedModel)

	if stream {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		var parsed OpenAIResponse
		if err := json.Unmarshal(resp.Body, &parsed); err == nil {
			WriteOpenAISSE(w, &parsed)
		} else {
			// 解析失败，原样回
			fmt.Fprintf(w, "data: %s\n\n", string(resp.Body))
			fmt.Fprintf(w, "data: [DONE]\n\n")
		}
	} else {
		w.Header().Set("Content-Type", "application/json")
		w.Write(resp.Body)
	}
}

// writeAnthropicError 写 Anthropic 格式错误响应
func writeAnthropicError(w http.ResponseWriter, status int, errType, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"type": "error",
		"error": map[string]interface{}{
			"type":    errType,
			"message": message,
		},
	})
}

// writeOpenAIError 写 OpenAI 格式错误响应
func writeOpenAIError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"error": map[string]interface{}{
			"message": message,
		},
	})
}