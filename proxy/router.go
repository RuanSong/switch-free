package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync/atomic"
	"time"

	"switchfree/upstream"
)

// ModelRef 配置中的模型引用（类型别名，避免循环引用 config 包）
type ModelRef struct {
	Upstream string `json:"upstream"`
	Model    string `json:"model"`
}

// ConfigResolver 配置解析接口（Server 持有，避免直接依赖 config 包）
type ConfigResolver interface {
	Resolve(requestedModel string) []ModelRef
	GetMode() string
	GetAPIKey() string
}

// callUpstreamAnthropic Anthropic 入口的上游分发（基于配置链遍历）
// 返回 (响应, 上游名, 实际用到的模型, 错误)
func (s *Server) callUpstreamAnthropic(ctx context.Context, body *AnthropicRequest) (*upstream.Response, string, string, error) {
	requestedModel := body.Model
	if requestedModel == "" {
		requestedModel = "auto"
	}
	chain := s.ConfigResolver.Resolve(requestedModel)
	return s.executeChain(ctx, body, chain, requestedModel)
}

// callUpstreamOpenAI OpenAI 直通入口的上游分发
func (s *Server) callUpstreamOpenAI(ctx context.Context, rawBody map[string]interface{}) (*upstream.Response, string, string, error) {
	requestedModel, _ := rawBody["model"].(string)
	if requestedModel == "" {
		requestedModel = "auto"
	}
	chain := s.ConfigResolver.Resolve(requestedModel)
	return s.executeChain(ctx, rawBody, chain, requestedModel)
}

// executeChain 遍历配置链，尝试每个模型引用，任意成功即返回
// 返回 (响应, 上游名, 实际用到的模型, 错误)
func (s *Server) executeChain(ctx context.Context, body interface{}, chain []ModelRef, requestedModel string) (*upstream.Response, string, string, error) {
	var lastErr error
	var lastUpstream string

	for _, ref := range chain {
		up := s.pickUpstream(ref.Upstream)
		if up == nil {
			continue
		}

		// 凭据无效的直接跳过（不发起请求）
		if !up.HasValidCreds() {
			fmt.Printf("[switch-free] 跳过 %s/%s（凭据无效）\n", ref.Upstream, ref.Model)
			s.recordSkipLog(requestedModel, ref, "cred_invalid")
			continue
		}

		// 构造 OpenAI body
		oaiBytes, err := s.buildOpenAIBody(body, ref)
		if err != nil {
			lastErr = err
			lastUpstream = ref.Upstream
			continue
		}

		// 发起请求
		resp, err := up.Call(ctx, oaiBytes)
		if err != nil {
			fmt.Printf("[switch-free] %s/%s 调用失败: %v，降级\n", ref.Upstream, ref.Model, err)
			lastErr = err
			lastUpstream = ref.Upstream
			s.recordFallbackLog(requestedModel, ref, "call_error", err.Error())
			continue
		}

		// 检查上游错误响应
		if isUpstreamErrorResponse(resp) {
			snippet := upstreamErrSnippet(resp)
			fmt.Printf("[switch-free] %s/%s 上游错误 (status=%d %s)，降级\n", ref.Upstream, ref.Model, resp.StatusCode, snippet)
			lastErr = fmt.Errorf("upstream error: %s", snippet)
			lastUpstream = ref.Upstream
			s.recordFallbackLog(requestedModel, ref, "upstream_error", snippet)
			continue
		}

		// 成功！返回实际用到的模型引用
		return resp, ref.Upstream, ref.Model, nil
	}

	if lastErr != nil {
		return nil, lastUpstream, "", lastErr
	}
	return nil, "", "", fmt.Errorf("配置链全失败，且无有效凭据的 upstream")
}

// executeChainStream 流式版 executeChain：只走支持 StreamCaller 的上游
// 返回 nil, "", "", nil 表示无流式上游可用（调用方回退伪流式）
// 返回 nil, up, "", err 表示有流式上游但全部失败
func (s *Server) executeChainStream(ctx context.Context, body interface{}, chain []ModelRef, requestedModel string) (*upstream.StreamResponse, string, string, error) {
	var lastErr error
	var lastUpstream string
	triedStream := false

	for _, ref := range chain {
		up := s.pickUpstream(ref.Upstream)
		if up == nil {
			continue
		}
		if !up.HasValidCreds() {
			fmt.Printf("[switch-free] 跳过 %s/%s（凭据无效）\n", ref.Upstream, ref.Model)
			s.recordSkipLog(requestedModel, ref, "cred_invalid")
			continue
		}
		// 类型断言：只走支持真流式的上游（WorkBuddy/OpenCode）
		sc, ok := up.(upstream.StreamCaller)
		if !ok {
			continue // 不支持流式，跳过（JoyCode/DevEco 走伪流式）
		}
		triedStream = true

		oaiBytes, err := s.buildOpenAIBody(body, ref)
		if err != nil {
			lastErr = err
			lastUpstream = ref.Upstream
			continue
		}

		sr, err := sc.CallStream(ctx, oaiBytes)
		if err != nil {
			fmt.Printf("[switch-free] %s/%s 流式调用失败: %v，降级\n", ref.Upstream, ref.Model, err)
			lastErr = err
			lastUpstream = ref.Upstream
			s.recordFallbackLog(requestedModel, ref, "call_error", err.Error())
			continue
		}

		if sr.StatusCode != 200 {
			errBody, _ := io.ReadAll(sr.Body)
			sr.Body.Close()
			snippet := string(errBody)
			if len(snippet) > 120 {
				snippet = snippet[:120]
			}
			fmt.Printf("[switch-free] %s/%s 流式上游错误 (status=%d %s)，降级\n", ref.Upstream, ref.Model, sr.StatusCode, snippet)
			lastErr = fmt.Errorf("upstream error: %s", snippet)
			lastUpstream = ref.Upstream
			s.recordFallbackLog(requestedModel, ref, "upstream_error", snippet)
			continue
		}

		return sr, ref.Upstream, ref.Model, nil
	}

	if !triedStream {
		return nil, "", "", nil // 无流式上游可用，回退伪流式
	}
	return nil, lastUpstream, "", lastErr // 有流式上游但全失败
}

// callUpstreamStream 流式上游分发（Anthropic/OpenAI 入口共用）
func (s *Server) callUpstreamStream(ctx context.Context, body interface{}) (*upstream.StreamResponse, string, string, error) {
	var requestedModel string
	switch b := body.(type) {
	case *AnthropicRequest:
		requestedModel = b.Model
	case map[string]interface{}:
		requestedModel, _ = b["model"].(string)
	}
	if requestedModel == "" {
		requestedModel = "auto"
	}
	chain := s.ConfigResolver.Resolve(requestedModel)
	return s.executeChainStream(ctx, body, chain, requestedModel)
}

// pickUpstream 按 upstream 名获取适配器
func (s *Server) pickUpstream(name string) upstream.Upstream {
	switch name {
	case "joycode":
		return s.JoyCode
	case "deveco":
		return s.DevEco
	case "opencode":
		return s.OpenCode
	case "workbuddy":
		return s.WorkBuddy
	}
	return nil
}

// buildOpenAIBody 根据 body 类型和模型引用构造 OpenAI 请求 body
func (s *Server) buildOpenAIBody(body interface{}, ref ModelRef) ([]byte, error) {
	switch b := body.(type) {
	case *AnthropicRequest:
		return s.buildAnthropicOpenAIBody(b, ref)
	case map[string]interface{}:
		return s.buildOpenAIPassthroughBody(b, ref)
	}
	return nil, fmt.Errorf("unknown body type: %T", body)
}

// buildAnthropicOpenAIBody Anthropic 请求 -> OpenAI body（按上游类型选择转换器）
func (s *Server) buildAnthropicOpenAIBody(body *AnthropicRequest, ref ModelRef) ([]byte, error) {
	cp := *body
	cp.Model = ref.Model
	var oaiBody *OpenAIRequest
	switch ref.Upstream {
	case "joycode":
		oaiBody, _ = AnthropicToOpenAI(&cp)
	case "deveco":
		oaiBody = AnthropicToOpenAIDeveco(&cp)
	case "opencode":
		oaiBody = AnthropicToOpenAIOpencode(&cp)
	case "workbuddy":
		oaiBody = AnthropicToOpenAIWorkbuddy(&cp)
	default:
		return nil, fmt.Errorf("unknown upstream: %s", ref.Upstream)
	}
	return json.Marshal(oaiBody)
}

// buildOpenAIPassthroughBody OpenAI 直通 body -> 注入 model + stream:false
// DevEco 需要把配置里的 model id 映射成网关认的 upstream 名
func (s *Server) buildOpenAIPassthroughBody(body map[string]interface{}, ref ModelRef) ([]byte, error) {
	cp := make(map[string]interface{}, len(body)+2)
	for k, v := range body {
		cp[k] = v
	}
	model := ref.Model
	if ref.Upstream == "deveco" {
		if dm := DevEcoModelByID[ref.Model]; dm != nil {
			model = dm.Upstream
		}
	} else if ref.Upstream == "workbuddy" {
		model = stripWbPrefix(ref.Model)
	}
	cp["model"] = model
	cp["stream"] = false
	return json.Marshal(cp)
}

// recordFallbackLog 记录降级日志
func (s *Server) recordFallbackLog(requestedModel string, ref ModelRef, reason, detail string) {
	entry := &LogEntry{
		Model:     requestedModel,
		Upstream:  ref.Upstream + "/" + ref.Model,
		Status:    "fallback",
		ErrorMsg:  fmt.Sprintf("[%s] %s", reason, detail),
	}
	s.recordLog(entry)
}

// recordSkipLog 记录跳过日志
func (s *Server) recordSkipLog(requestedModel string, ref ModelRef, reason string) {
	entry := &LogEntry{
		Model:     requestedModel,
		Upstream:  ref.Upstream + "/" + ref.Model,
		Status:    "fallback",
		ErrorMsg:  fmt.Sprintf("[%s] 跳过（凭据无效）", reason),
	}
	s.recordLog(entry)
}

// nowMillis / incRequests / timestampNow 辅助
var _counter int64

func nowMillis() int64                   { return atomic.AddInt64(&_counter, 1) }
func (s *Server) incRequests() int64     { return atomic.AddInt64(&s.requests, 1) }
func timestampNow() string               { return time.Now().Format("15:04:05") }

// ====== 以下来自旧 router.go（被 handlers.go 引用） ======

// isUpstreamErrorResponse 判断上游响应是否为错误响应
func isUpstreamErrorResponse(resp *upstream.Response) bool {
	trimmed := strings.TrimSpace(string(resp.Body))
	if trimmed == "" {
		return resp.StatusCode >= 400
	}
	var o map[string]interface{}
	if err := json.Unmarshal([]byte(trimmed), &o); err != nil {
		return resp.StatusCode >= 400
	}
	if choices, ok := o["choices"]; ok {
		// choices 存在但为空数组 → 上游返回了不完整响应，视为错误
		if arr, ok := choices.([]interface{}); ok && len(arr) == 0 {
			return true
		}
		return false
	}
	_, hasError := o["error"]
	_, hasCode := o["code"]
	_, hasErrorCode := o["errorCode"]
	return hasError || hasCode || hasErrorCode
}

// upstreamErrSnippet 取上游错误响应的前 120 字符
func upstreamErrSnippet(resp *upstream.Response) string {
	trimmed := strings.TrimSpace(string(resp.Body))
	if len(trimmed) > 120 {
		return trimmed[:120]
	}
	return trimmed
}

// extractUpstreamError 从错误响应体提取 msg + code
func extractUpstreamError(body []byte) (msg, code string) {
	var o map[string]interface{}
	if err := json.Unmarshal(body, &o); err != nil {
		return "upstream error", "upstream_error"
	}
	if e, ok := o["error"].(map[string]interface{}); ok {
		if m, ok := e["message"].(string); ok {
			msg = m
		}
		if c, ok := e["code"]; ok {
			code = fmt.Sprint(c)
		}
	}
	if msg == "" {
		if e, ok := o["echo"].(string); ok {
			msg = e
		}
	}
	if msg == "" {
		if m, ok := o["msg"].(string); ok {
			msg = m
		}
	}
	if code == "" {
		if c, ok := o["code"]; ok {
			code = fmt.Sprint(c)
		}
	}
	if code == "" {
		if c, ok := o["errorCode"]; ok {
			code = fmt.Sprint(c)
		}
	}
	if msg == "" {
		msg = "upstream error"
	}
	if code == "" {
		code = "upstream_error"
	}
	return msg, code
}

// clampMaxTokensFromMap 从 map 里取 max_tokens，钳制后返回
func clampMaxTokensFromMap(raw interface{}, limit int) int {
	v := 0
	switch n := raw.(type) {
	case float64:
		v = int(n)
	case int:
		v = n
	case int64:
		v = int(n)
	}
	return ClampMaxTokens(v, limit)
}