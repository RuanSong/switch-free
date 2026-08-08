package upstream

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"switchfree/creds"
)

// OpenCodeUpstream OpenCode Zen 适配器
type OpenCodeUpstream struct {
	mgr    *creds.OpenCodeCredManager
	client *http.Client
}

func NewOpenCodeUpstream(mgr *creds.OpenCodeCredManager) *OpenCodeUpstream {
	return &OpenCodeUpstream{
		mgr: mgr,
		client: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

func (u *OpenCodeUpstream) Name() string { return "opencode" }

func (u *OpenCodeUpstream) EnsureCreds(ctx context.Context) error {
	_, err := u.mgr.EnsureCreds()
	return err
}

func (u *OpenCodeUpstream) InvalidateCreds() { u.mgr.InvalidateCreds() }

func (u *OpenCodeUpstream) VerifyCreds(ctx context.Context) (*VerifyResult, error) {
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

func (u *OpenCodeUpstream) CredStatus() *creds.CredStatusInfo {
	return u.mgr.CredStatus()
}

// HasValidCreds 凭据是否可用
// 宽松判定：apiKey 已加载就返回 true，让 Call 内部的重读逻辑处理失效
func (u *OpenCodeUpstream) HasValidCreds() bool {
	cred := u.mgr.GetCred()
	return cred != nil && cred.APIKey != ""
}

// FetchModels 调 GET /models 拉取可用模型列表
func (u *OpenCodeUpstream) FetchModels(ctx context.Context) ([]FetchedModel, error) {
	cred, err := u.mgr.EnsureCreds()
	if err != nil {
		return nil, err
	}
	url := u.mgr.Config().BaseURL + "/models"

	body, _, err := httpGet(url, map[string]string{
		"Authorization": fmt.Sprintf("Bearer %s", cred.APIKey),
		"Accept":        "application/json",
	})
	if err != nil {
		return nil, err
	}

	var resp struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := creds.ParseJSONPublic(body, &resp); err != nil {
		return nil, err
	}

	result := make([]FetchedModel, 0, len(resp.Data))
	for _, m := range resp.Data {
		if m.ID == "" {
			continue
		}
		result = append(result, FetchedModel{
			ID:    m.ID,
			Label: m.ID,
		})
	}
	return result, nil
}

// Call 调用 OpenCode Zen（带 401 重读重试）
func (u *OpenCodeUpstream) Call(ctx context.Context, body []byte) (*Response, error) {
	cred, err := u.mgr.EnsureCreds()
	if err != nil {
		return nil, err
	}

	resp, err := u.doCall(ctx, body, cred)
	if err != nil {
		return nil, err
	}

	if isOpenCodeKeyInvalid(resp) {
		fmt.Println("[switch-free] OpenCode 收到 401（apiKey 失效），重读 auth.json 并重试一次")
		u.mgr.InvalidateCreds()
		oldKey := cred.APIKey
		newCred, err := u.mgr.EnsureCreds()
		if err != nil {
			return resp, nil
		}
		if newCred.APIKey != oldKey {
			return u.doCall(ctx, body, newCred)
		}
	}
	return resp, nil
}

func (u *OpenCodeUpstream) doCall(ctx context.Context, body []byte, cred *creds.OpenCodeCred) (*Response, error) {
	url := u.mgr.Config().BaseURL + "/chat/completions"
	reqID := fmt.Sprintf("req-%d-%s", time.Now().Unix(), randString(6))

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json; charset=UTF-8")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", cred.APIKey))
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

	return &Response{
		StatusCode: httpResp.StatusCode,
		Body:       respBody,
		ReqID:      reqID,
	}, nil
}

// isOpenCodeKeyInvalid OpenCode apiKey 失效判断
func isOpenCodeKeyInvalid(resp *Response) bool {
	if resp.StatusCode == 401 {
		return true
	}
	var o struct {
		Error struct {
			Type string `json:"type"`
		} `json:"error"`
	}
	if creds.ParseJSONPublic(resp.Body, &o) == nil {
		if strings.Contains(strings.ToLower(o.Error.Type), "auth") {
			return true
		}
	}
	return false
}