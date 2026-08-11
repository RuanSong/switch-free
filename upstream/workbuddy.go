package upstream

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"switchfree/creds"
)

// WorkBuddyUpstream 腾讯 CodeBuddy(WorkBuddy) 适配器
// 上游强制 stream:true，Call 内部聚合 SSE 成完整 JSON 返回，对上层透明
type WorkBuddyUpstream struct {
	mgr    *creds.WorkBuddyCredManager
	client *http.Client
}

func NewWorkBuddyUpstream(mgr *creds.WorkBuddyCredManager) *WorkBuddyUpstream {
	return &WorkBuddyUpstream{
		mgr: mgr,
		client: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

func (u *WorkBuddyUpstream) Name() string { return "workbuddy" }

func (u *WorkBuddyUpstream) EnsureCreds(ctx context.Context) error {
	_, err := u.mgr.EnsureCreds()
	return err
}

func (u *WorkBuddyUpstream) InvalidateCreds() { u.mgr.InvalidateCreds() }

func (u *WorkBuddyUpstream) VerifyCreds(ctx context.Context) (*VerifyResult, error) {
	cred := u.mgr.GetCred()
	if cred == nil {
		c, err := u.mgr.LoadCreds()
		if err != nil {
			return &VerifyResult{Valid: false, Status: -1}, err
		}
		cred = c
	}
	valid, status, err := u.mgr.VerifyCreds(cred)
	if err != nil {
		return &VerifyResult{Valid: false, Status: -1}, err
	}
	return &VerifyResult{Valid: valid, Status: status}, nil
}

func (u *WorkBuddyUpstream) CredStatus() *creds.CredStatusInfo {
	return u.mgr.CredStatus()
}

// HasValidCreds 凭据是否可用（宽松判定：accessToken 已加载即返回 true）
func (u *WorkBuddyUpstream) HasValidCreds() bool {
	cred := u.mgr.GetCred()
	return cred != nil && cred.AccessToken != ""
}

// FetchModels WorkBuddy 无 /v2/models 端点，返回 error 让上层回退本地白名单
func (u *WorkBuddyUpstream) FetchModels(ctx context.Context) ([]FetchedModel, error) {
	return nil, fmt.Errorf("workbuddy 使用本地白名单，不实时拉取")
}

// Call 调用 WorkBuddy（强制 stream:true + 聚合 SSE + 401 重试）
func (u *WorkBuddyUpstream) Call(ctx context.Context, body []byte) (*Response, error) {
	cred, err := u.mgr.EnsureCreds()
	if err != nil {
		return nil, err
	}

	resp, err := u.doCall(ctx, body, cred)
	if err != nil {
		return nil, err
	}

	if isWorkBuddyTokenInvalid(resp) {
		fmt.Println("[switch-free] WorkBuddy 收到 401/403（token 失效），刷新并重试一次")
		u.mgr.InvalidateCreds()
		newCred, err := u.mgr.EnsureCreds()
		if err != nil {
			return resp, nil
		}
		return u.doCall(ctx, body, newCred)
	}
	return resp, nil
}

func (u *WorkBuddyUpstream) doCall(ctx context.Context, body []byte, cred *creds.WorkBuddyCred) (*Response, error) {
	// 强制 stream:true（WorkBuddy 上游非流式返回 code:11101）
	var m map[string]interface{}
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, fmt.Errorf("解析请求 body 失败: %w", err)
	}
	m["stream"] = true
	m["stream_options"] = map[string]bool{"include_usage": true} // 让上游在最后一个 chunk 返回 usage
	bodyBytes, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("重新编码请求 body 失败: %w", err)
	}

	url := u.mgr.Config().BaseURL + "/chat/completions"
	reqID := fmt.Sprintf("req-%d-%s", time.Now().Unix(), randString(6))

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json; charset=UTF-8")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", cred.AccessToken))
	req.Header.Set("Accept", "application/json")
	// 不设 Accept-Encoding，让 Go http.Client 自动透明解压 gzip

	httpResp, err := u.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer httpResp.Body.Close()

	// 非 200：原样返回错误体，让上层 isUpstreamErrorResponse 判断
	if httpResp.StatusCode != 200 {
		errBody, _ := io.ReadAll(httpResp.Body)
		return &Response{
			StatusCode: httpResp.StatusCode,
			Body:       errBody,
			ReqID:      reqID,
		}, nil
	}

	// 200：聚合 SSE 流成完整 OpenAIResponse JSON
	aggregated, err := aggregateOpenAISSE(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("聚合 WorkBuddy SSE 失败: %w", err)
	}
	return &Response{
		StatusCode: 200,
		Body:       aggregated,
		ReqID:      reqID,
	}, nil
}

// isWorkBuddyTokenInvalid token 失效判断（401/403）
func isWorkBuddyTokenInvalid(resp *Response) bool {
	return resp.StatusCode == 401 || resp.StatusCode == 403
}
