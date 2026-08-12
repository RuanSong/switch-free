package service

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"switchfree/freeapi"
	"switchfree/upstream"
)

// FreeAPIService 免费 API 供应商管理服务（暴露给前端）
type FreeAPIService struct {
	mgr     *freeapi.Manager
	loader  *freeapi.CatalogLoader
	monitor *freeapi.Monitor
	core    *Core
}

// NewFreeAPIService 创建免费 API 服务
func NewFreeAPIService(mgr *freeapi.Manager, loader *freeapi.CatalogLoader, monitor *freeapi.Monitor, core *Core) *FreeAPIService {
	return &FreeAPIService{mgr: mgr, loader: loader, monitor: monitor, core: core}
}

// freeRefreshCallback 重建免费上游 + 模型列表的回调（main 注入，包级避免 Wails 暴露）
var freeRefreshCallback func()

// SetFreeRefreshCallback 注入重建回调（main 调用，指向 registerFreeAPIRefresh 的 rebuild）
// 注意：此方法不导出到 Wails 绑定（由 main 直接调用）
func SetFreeRefreshCallback(fn func()) {
	freeRefreshCallback = fn
}

// refresh 内部触发重建（若已注入回调）
func (s *FreeAPIService) refresh() {
	if freeRefreshCallback != nil {
		freeRefreshCallback()
	}
}

// GetProviders 返回所有已添加供应商（含模型 verified/healthy 状态）
func (s *FreeAPIService) GetProviders() map[string]*freeapi.ProviderConfig {
	return s.mgr.GetProviders()
}

// UpsertProvider 新增/更新供应商配置（保存到 free_apis.json + 重建上游）
func (s *FreeAPIService) UpsertProvider(cfg *freeapi.ProviderConfig) error {
	if strings.TrimSpace(cfg.ID) == "" {
		return fmt.Errorf("供应商 id 不能为空")
	}
	if strings.TrimSpace(cfg.BaseURL) == "" {
		return fmt.Errorf("BaseURL 不能为空")
	}
	if strings.TrimSpace(cfg.APIKey) == "" {
		return fmt.Errorf("API Key 不能为空")
	}
	if err := s.mgr.UpsertProvider(cfg); err != nil {
		return err
	}
	s.refresh()
	return nil
}

// RemoveProvider 删除供应商
func (s *FreeAPIService) RemoveProvider(id string) error {
	if err := s.mgr.RemoveProvider(id); err != nil {
		return err
	}
	s.refresh()
	return nil
}

// AddVerifiedModel 评测通过后把模型加入供应商
func (s *FreeAPIService) AddVerifiedModel(providerID string, model freeapi.ProviderModel) error {
	if err := s.mgr.AddVerifiedModel(providerID, model); err != nil {
		return err
	}
	// 重建上游 + 模型列表（新模型进入路由）
	s.refresh()
	return nil
}

// GetCatalog 返回免费 API 目录（embed + GitHub + 本地缓存）
func (s *FreeAPIService) GetCatalog() *freeapi.Catalog {
	return s.loader.LoadCatalog()
}

// RefreshCatalog 强制从 GitHub 拉最新目录
func (s *FreeAPIService) RefreshCatalog() (*freeapi.Catalog, bool, error) {
	return s.loader.RefreshCatalog()
}

// FetchProviderModels 测试 base_url + apiKey 能否拉取模型
// 返回模型 id 列表（能力字段尽量解析）
func (s *FreeAPIService) FetchProviderModels(baseURL, apiKey string) ([]upstream.FetchedModel, error) {
	if strings.TrimSpace(baseURL) == "" || strings.TrimSpace(apiKey) == "" {
		return nil, fmt.Errorf("BaseURL 和 API Key 不能为空")
	}
	base := strings.TrimSuffix(baseURL, "/")
	url := base + "/models"
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "application/json")
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		snippet := string(body)
		if len(snippet) > 120 {
			snippet = snippet[:120]
		}
		return nil, fmt.Errorf("GET /models 返回 %d: %s", resp.StatusCode, snippet)
	}

	// 解析 OpenAI 兼容 /models（可能带能力字段）
	var data struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, fmt.Errorf("解析模型列表失败: %w", err)
	}
	result := make([]upstream.FetchedModel, 0, len(data.Data))
	for _, m := range data.Data {
		if m.ID == "" {
			continue
		}
		result = append(result, upstream.FetchedModel{ID: m.ID, Label: m.ID, Stream: true})
	}
	return result, nil
}

// BenchmarkModel 对单个模型直接发请求评测（不经过代理，验证模型可用 + 测 TPS）
// 返回评测结果；通过（success）后由前端决定是否 AddVerifiedModel
func (s *FreeAPIService) BenchmarkModel(baseURL, apiKey, modelID, prompt string, maxTokens int) (map[string]interface{}, error) {
	if strings.TrimSpace(baseURL) == "" || strings.TrimSpace(apiKey) == "" || strings.TrimSpace(modelID) == "" {
		return nil, fmt.Errorf("BaseURL、API Key、模型不能为空")
	}
	if prompt == "" {
		prompt = "请用一句话介绍你自己。"
	}
	if maxTokens <= 0 {
		maxTokens = 256
	}

	base := strings.TrimSuffix(baseURL, "/")
	payload := map[string]interface{}{
		"model":      modelID,
		"messages":   []map[string]string{{"role": "user", "content": prompt}},
		"max_tokens": maxTokens,
		"stream":     false,
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequest("POST", base+"/chat/completions", strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "application/json")
	client := &http.Client{Timeout: 60 * time.Second}

	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	duration := time.Since(start).Milliseconds()

	out := map[string]interface{}{
		"success":    resp.StatusCode == 200,
		"durationMs": duration,
		"statusCode": resp.StatusCode,
	}
	if resp.StatusCode == 200 {
		// 提取输出内容 + token
		var oai struct {
			Choices []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			} `json:"choices"`
			Usage struct {
				PromptTokens     int `json:"prompt_tokens"`
				CompletionTokens int `json:"completion_tokens"`
			} `json:"usage"`
		}
		_ = json.Unmarshal(respBody, &oai)
		if len(oai.Choices) > 0 {
			out["content"] = oai.Choices[0].Message.Content
		}
		out["outputTokens"] = oai.Usage.CompletionTokens
		out["inputTokens"] = oai.Usage.PromptTokens
		out["tps"] = 0.0
		if oai.Usage.CompletionTokens > 0 && duration > 0 {
			out["tps"] = float64(oai.Usage.CompletionTokens) / (float64(duration) / 1000.0)
		}
	} else {
		snippet := string(respBody)
		if len(snippet) > 200 {
			snippet = snippet[:200]
		}
		out["errorMsg"] = snippet
	}
	return out, nil
}

// GetStatus 返回各供应商凭据 + 模型 verified/healthy 状态
func (s *FreeAPIService) GetStatus() map[string]interface{} {
	providers := s.mgr.GetProviders()
	out := map[string]interface{}{}
	for id, p := range providers {
		models := make([]map[string]interface{}, 0, len(p.Models))
		for _, mo := range p.Models {
			models = append(models, map[string]interface{}{
				"id":       mo.ID,
				"context":  mo.Context,
				"verified": mo.Verified,
				"healthy":  mo.Healthy,
			})
		}
		out[id] = map[string]interface{}{
			"name":     p.Name,
			"baseURL":  p.BaseURL,
			"verified": p.Verified,
			"models":   models,
			"cred":     s.mgr.CredStatus(id),
		}
	}
	return out
}

// OpenURL 用系统浏览器打开外部链接（获取 API Key、官网等）
func (s *FreeAPIService) OpenURL(url string) {
	if strings.TrimSpace(url) == "" {
		return
	}
	openBrowser(url)
}

// emitFreeChange 推送免费 API 配置变化事件
func (s *FreeAPIService) emitFreeChange() {
	if s.core != nil {
		s.core.EmitEvent("freeapi:change", s.GetProviders())
		s.core.EmitEvent("cred:change", s.core.GetCredStatus())
	}
}
