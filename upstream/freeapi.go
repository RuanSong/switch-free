package upstream

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"switchfree/creds"
)

// FreeAPIUpstream 通用 OpenAI 兼容免费 API 上游适配器
// 支持任意 base_url + Bearer key 的免费模型（Groq/NVIDIA NIM/自定义等）
// 与内置 4 上游平级，可进降级链
type FreeAPIUpstream struct {
	name        string // provider id（如 "groq"、"custom-xxx"）
	displayName string // 供应商显示名（如 "Groq"）
	getBaseURL  func() string
	getAPIKey   func() string
	client      *http.Client
}

// NewFreeAPIUpstream 创建免费 API 上游
// providerID 用于路由标识；getBaseURL/getAPIKey 返回当前绑定的 provider 配置
func NewFreeAPIUpstream(providerID string, getBaseURL, getAPIKey func() string) *FreeAPIUpstream {
	return &FreeAPIUpstream{
		name:       providerID,
		getBaseURL: getBaseURL,
		getAPIKey:  getAPIKey,
		client:     &http.Client{Timeout: 120 * time.Second},
	}
}

// SetDisplayName 设置供应商显示名（前端展示用）
func (u *FreeAPIUpstream) SetDisplayName(name string) {
	u.displayName = name
}

// Rebind 热切换绑定的 provider 配置（激活切换时调用）
func (u *FreeAPIUpstream) Rebind(getBaseURL, getAPIKey func() string) {
	u.getBaseURL = getBaseURL
	u.getAPIKey = getAPIKey
}

func (u *FreeAPIUpstream) Name() string { return u.name }

func (u *FreeAPIUpstream) EnsureCreds(ctx context.Context) error {
	if u.getAPIKey == nil || u.getAPIKey() == "" {
		return fmt.Errorf("免费 API 供应商 %s 未配置 API Key", u.name)
	}
	return nil
}

func (u *FreeAPIUpstream) InvalidateCreds() {}

func (u *FreeAPIUpstream) VerifyCreds(ctx context.Context) (*VerifyResult, error) {
	if u.getAPIKey == nil || u.getAPIKey() == "" {
		return &VerifyResult{Valid: false, Status: -1}, fmt.Errorf("未配置 API Key")
	}
	_, status, err := u.getModels(u.getAPIKey())
	if err != nil {
		return &VerifyResult{Valid: false, Status: status}, err
	}
	return &VerifyResult{Valid: status == 200, Status: status}, nil
}

func (u *FreeAPIUpstream) CredStatus() *creds.CredStatusInfo {
	name := u.name
	if u.displayName != "" {
		name = u.displayName
	}
	return &creds.CredStatusInfo{
		Valid:      u.HasValidCreds(),
		Installed:  u.HasValidCreds(),
		KeyPreview: u.keyPreview(),
		Source:     name,
	}
}

func (u *FreeAPIUpstream) HasValidCreds() bool {
	return u.getAPIKey != nil && u.getAPIKey() != ""
}

// FetchModels 拉取可用模型列表
func (u *FreeAPIUpstream) FetchModels(ctx context.Context) ([]FetchedModel, error) {
	key := u.getAPIKey()
	if key == "" {
		return nil, fmt.Errorf("未配置 API Key")
	}
	body, status, err := u.getModels(key)
	if err != nil {
		return nil, err
	}
	if status != 200 {
		return nil, fmt.Errorf("GET /models 返回 %d", status)
	}
	var resp struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	result := make([]FetchedModel, 0, len(resp.Data))
	for _, m := range resp.Data {
		if m.ID == "" {
			continue
		}
		result = append(result, FetchedModel{ID: m.ID, Label: m.ID, Stream: true})
	}
	return result, nil
}

// Call 非流式调用
func (u *FreeAPIUpstream) Call(ctx context.Context, body []byte) (*Response, error) {
	key := u.getAPIKey()
	if key == "" {
		return nil, fmt.Errorf("免费 API 供应商 %s 未配置 API Key", u.name)
	}
	return u.doCall(ctx, body, key)
}

// CallStream 真流式调用
func (u *FreeAPIUpstream) CallStream(ctx context.Context, body []byte) (*StreamResponse, error) {
	key := u.getAPIKey()
	if key == "" {
		return nil, fmt.Errorf("免费 API 供应商 %s 未配置 API Key", u.name)
	}
	return u.doCallStream(ctx, body, key)
}

func (u *FreeAPIUpstream) base() string {
	return strings.TrimSuffix(u.getBaseURL(), "/")
}

func (u *FreeAPIUpstream) doCall(ctx context.Context, body []byte, key string) (*Response, error) {
	url := u.base() + "/chat/completions"
	reqID := fmt.Sprintf("req-%d-%s", time.Now().Unix(), randString(6))

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json; charset=UTF-8")
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Accept", "application/json")

	httpResp, err := u.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer httpResp.Body.Close()
	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, err
	}
	return &Response{StatusCode: httpResp.StatusCode, Body: respBody, ReqID: reqID}, nil
}

func (u *FreeAPIUpstream) doCallStream(ctx context.Context, body []byte, key string) (*StreamResponse, error) {
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, fmt.Errorf("解析请求 body 失败: %w", err)
	}
	m["stream"] = true
	m["stream_options"] = map[string]bool{"include_usage": true}
	bodyBytes, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("重新编码请求 body 失败: %w", err)
	}

	url := u.base() + "/chat/completions"
	reqID := fmt.Sprintf("req-%d-%s", time.Now().Unix(), randString(6))

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Accept", "text/event-stream")

	httpResp, err := u.client.Do(req)
	if err != nil {
		return nil, err
	}
	if httpResp.StatusCode != 200 {
		errBody, _ := io.ReadAll(httpResp.Body)
		httpResp.Body.Close()
		return &StreamResponse{StatusCode: httpResp.StatusCode, Body: io.NopCloser(bytes.NewReader(errBody)), ReqID: reqID}, nil
	}

	// peek 检测空流 + 非 SSE
	br := bufio.NewReader(httpResp.Body)
	peeked, _ := br.Peek(256)
	peekStr := string(peeked)
	if len(peeked) == 0 {
		httpResp.Body.Close()
		return &StreamResponse{
			StatusCode: 502,
			Body:       io.NopCloser(bytes.NewReader([]byte(`{"error":"upstream empty stream"}`))),
			ReqID:      reqID,
		}, nil
	}
	if !strings.Contains(peekStr, "data:") || !strings.Contains(peekStr, "choices") {
		httpResp.Body.Close()
		snippet := peekStr
		if len(snippet) > 200 {
			snippet = snippet[:200]
		}
		return &StreamResponse{
			StatusCode: 502,
			Body:       io.NopCloser(bytes.NewReader([]byte(fmt.Sprintf(`{"error":"non-sse: %s"}`, snippet)))),
			ReqID:      reqID,
		}, nil
	}
	return &StreamResponse{StatusCode: 200, Body: &bufferedReadCloser{br: br, c: httpResp.Body}, ReqID: reqID}, nil
}

// getModels GET /models
func (u *FreeAPIUpstream) getModels(key string) ([]byte, int, error) {
	url := u.base() + "/models"
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Accept", "application/json")
	resp, err := u.client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, err
	}
	return body, resp.StatusCode, nil
}

func (u *FreeAPIUpstream) keyPreview() string {
	key := u.getAPIKey()
	if len(key) > 8 {
		return key[:8] + "..."
	}
	if key != "" {
		return "***"
	}
	return ""
}
