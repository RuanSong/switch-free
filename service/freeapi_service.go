package service

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"switchfree/freeapi"
	"switchfree/upstream"
	"switchfree/version"
)

// IdleLockSeconds 无操作多少秒后自动锁定（需要主密码解锁）。默认 300 秒（5 分钟），
// 打包时可用 -ldflags "-X switchfree/service.IdleLockSeconds=600" 覆盖。设为 0 关闭自动锁定。
var IdleLockSeconds = 300

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

// UpsertProvider 新增/更新供应商配置（保存到 credentials.json + 重建上游）
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

// RemoveModel 从供应商移除某个已加入模型（编辑界面取消加入）
func (s *FreeAPIService) RemoveModel(providerID, modelID string) error {
	if err := s.mgr.RemoveModel(providerID, modelID); err != nil {
		return err
	}
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
	return benchmarkOnce(baseURL, apiKey, modelID, prompt, maxTokens), nil
}

// benchmarkOnce 实际发请求并组装结果
func benchmarkOnce(baseURL, apiKey, modelID, prompt string, maxTokens int) map[string]interface{} {
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
		return map[string]interface{}{"success": false, "errorMsg": err.Error()}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "application/json")
	client := &http.Client{Timeout: 60 * time.Second}

	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return map[string]interface{}{"success": false, "errorMsg": err.Error()}
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	duration := time.Since(start).Milliseconds()

	out := map[string]interface{}{
		"modelId":    modelID,
		"success":    resp.StatusCode == 200,
		"durationMs": duration,
		"statusCode": resp.StatusCode,
	}
	if resp.StatusCode == 200 {
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
	return out
}

// BatchBenchmarkItem 批量测评的单个输入
type BatchBenchmarkItem struct {
	ModelID string `json:"modelId"`
}

// BatchBenchmark 以有限并发批量测评多个模型，通过 freeapi:bench 事件推送每个结果。
// 并发数根据模型数量自动调整：20 个以内用 4，超过 20 用 8。
func (s *FreeAPIService) BatchBenchmark(baseURL, apiKey, prompt string, maxTokens int, modelIDs []string) error {
	if strings.TrimSpace(baseURL) == "" || strings.TrimSpace(apiKey) == "" {
		return fmt.Errorf("BaseURL 和 API Key 不能为空")
	}
	if len(modelIDs) == 0 {
		return nil
	}
	total := len(modelIDs)
	queue := append([]string(nil), modelIDs...)
	// 根据模型数量动态调整并发：20 个以内用 4，超过 20 用 8
	conc := 4
	if total > 20 {
		conc = 8
	}
	if conc > total {
		conc = total
	}

	// 在后台运行，方法立即返回，避免 Wails 长 RPC 被中止导致 HTTP 请求被取消。
	// 进度通过 freeapi:bench 事件推送，全部完成后推 freeapi:bench-done。
	go func() {
		defer func() {
			if r := recover(); r != nil {
				if s.core != nil {
					s.core.EmitEvent("freeapi:bench-done", map[string]interface{}{"total": total, "error": fmt.Sprintf("%v", r)})
				}
			}
		}()
		var mu sync.Mutex
		var wg sync.WaitGroup
		done := 0
		for i := 0; i < conc; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for {
					mu.Lock()
					if len(queue) == 0 {
						mu.Unlock()
						return
					}
					modelID := queue[0]
					queue = queue[1:]
					mu.Unlock()

					res := benchmarkOnce(baseURL, apiKey, modelID, prompt, maxTokens)

					mu.Lock()
					done++
					progress := done
					mu.Unlock()

					if s.core != nil {
						s.core.EmitEvent("freeapi:bench", map[string]interface{}{
							"modelId": modelID,
							"result":  res,
							"done":    progress,
							"total":   total,
						})
					}
				}
			}()
		}
		wg.Wait()
		if s.core != nil {
			s.core.EmitEvent("freeapi:bench-done", map[string]interface{}{
				"total": total,
			})
		}
	}()
	return nil
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

// ====== 供应商分享（.sds：口令加密，一次性强密码）======

// GenerateSharePassword 生成一次性强分享密码（16 位）
func (s *FreeAPIService) GenerateSharePassword() (string, error) {
	return freeapi.GenerateSharePassword()
}

// ExportShare 导出选中供应商为加密 .sds 内容（base64）。
// 文件始终加密（供应商信息也是密文）；includeKey=false 时剥离 API Key。
func (s *FreeAPIService) ExportShare(ids []string, password string, includeKey bool) (string, error) {
	data, err := s.mgr.EncryptShare(ids, password, freeapi.ShareOptions{
		IncludeAPIKey: includeKey,
		Encrypt:       true,
	}, version.GetVersion(), time.Now().Format(time.RFC3339))
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(data), nil
}

// SaveShareFile 弹出原生"另存为"对话框，把加密内容写入用户选择的路径。
// 返回最终保存的绝对路径；用户取消则返回空字符串。
func (s *FreeAPIService) SaveShareFile(dataB64 string) (string, error) {
	data, err := base64.StdEncoding.DecodeString(dataB64)
	if err != nil {
		return "", fmt.Errorf("文件内容解码失败: %w", err)
	}
	app := application.Get()
	if app == nil {
		return "", errors.New("无法访问应用窗口（非 GUI 模式）")
	}
	defaultName := "switch-dev-share-" + time.Now().Format("20060102-150405") + ".sds"
	dlg := app.Dialog.SaveFileWithOptions(&application.SaveFileDialogOptions{
		Title:                "保存分享文件",
		Filename:             defaultName,
		ButtonText:           "保存",
		CanCreateDirectories: true,
		Filters: []application.FileFilter{
			{DisplayName: "Switch Dev 分享文件 (*.sds)", Pattern: "*.sds"},
		},
	})

	path, err := dlg.PromptForSingleSelection()
	if err != nil {
		return "", err
	}
	if path == "" {
		return "", nil // 用户取消
	}
	// 若用户没带后缀，补上 .sds
	if !strings.HasSuffix(strings.ToLower(path), ".sds") {
		path += ".sds"
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", fmt.Errorf("写入失败: %w", err)
	}
	return path, nil
}

// InspectShare 读取 .sds 文件头信息（不解密），用于导入前判断是否需要密码、显示版本
func (s *FreeAPIService) InspectShare(dataB64 string) (*freeapi.SharePreview, error) {
	data, err := base64.StdEncoding.DecodeString(dataB64)
	if err != nil {
		return nil, fmt.Errorf("文件内容解码失败: %w", err)
	}
	return freeapi.InspectShare(data)
}

// DecryptShare 用密码解密 .sds，返回供应商列表（导入预览，不写入配置）
func (s *FreeAPIService) DecryptShare(dataB64 string, password string) ([]freeapi.ShareProvider, error) {
	data, err := base64.StdEncoding.DecodeString(dataB64)
	if err != nil {
		return nil, fmt.Errorf("文件内容解码失败: %w", err)
	}
	return freeapi.DecryptShare(data, password)
}

// ImportShare 按指令导入供应商（已在前端解决冲突），完成后重建上游
func (s *FreeAPIService) ImportShare(items []freeapi.ImportItem) (int, error) {
	if len(items) == 0 {
		return 0, fmt.Errorf("没有要导入的供应商")
	}
	before := len(s.mgr.GetProviders())
	if err := s.mgr.ImportProviders(items); err != nil {
		return 0, err
	}
	// 异步重建上游，避免阻塞 Wails 调用返回
	go s.refresh()
	return len(s.mgr.GetProviders()) - before, nil
}

// ── 本地凭据加密（启动锁 / 主密码 / 恢复码）──

// GetLockStatus 返回锁状态
func (s *FreeAPIService) GetLockStatus() freeapi.LocksetInfo {
	return s.mgr.GetLocksetInfo()
}

// Unlock 用主密码解锁
func (s *FreeAPIService) Unlock(password string) error {
	if err := s.mgr.Unlock(password); err != nil {
		return err
	}
	go s.refresh()
	return nil
}

// SetMasterPassword 设置/更换主密码，返回新恢复码
func (s *FreeAPIService) SetMasterPassword(password string, remember bool) (string, error) {
	code, err := s.mgr.SetMasterPassword(password, remember)
	if err != nil {
		return "", err
	}
	// 立即重建上游：remember=false 时管理器已清空 DEK/配置，重建后运行中的代理
	// 也会用空 key 重绑定，真正即时锁定；remember=true 时密钥仍在内存，重建无副作用。
	s.refresh()
	return code, nil
}

// RecoverWithCode 用恢复码重置主密码，返回新恢复码
func (s *FreeAPIService) RecoverWithCode(recoveryCode, newPassword string, remember bool) (string, error) {
	code, err := s.mgr.RecoverWithCode(recoveryCode, newPassword, remember)
	if err != nil {
		return "", err
	}
	// 恢复成功后重建上游：remember=true 时密钥已在内存，重绑定后立即可用；
	// remember=false 时管理器已清空 DEK，重建后绑定空 key（即时锁定）。
	s.refresh()
	return code, nil
}

// ClearRememberedPassword 清除"记住密码"
func (s *FreeAPIService) ClearRememberedPassword() error {
	return s.mgr.ClearRememberedPassword()
}

// Lock 立即锁定（清除内存 DEK），下次需要主密码解锁
func (s *FreeAPIService) Lock() {
	s.mgr.Lock()
	// 重建上游：用空 key 重绑定，使锁定后无法再用旧密钥发起调用；模型列表仍保留。
	s.refresh()
}

// ClearMasterPassword 清除用户主密码，回到自动加密模式（随机密码存钥匙串，启动自动解锁）。
// 不改动已加密的 apiKey，只重新包裹 DEK；清除后立即重建上游（key 已在内存，调用不受影响）。
func (s *FreeAPIService) ClearMasterPassword() error {
	if err := s.mgr.ClearMasterPassword(); err != nil {
		return err
	}
	s.refresh()
	return nil
}

// GetIdleLockSeconds 返回无操作自动锁定秒数（构建时可注入）
func (s *FreeAPIService) GetIdleLockSeconds() int {
	return IdleLockSeconds
}

// ResetVault 销毁本地加密配置（忘记密码兜底，数据会丢失）
func (s *FreeAPIService) ResetVault() error {
	if err := s.mgr.ResetVault(); err != nil {
		return err
	}
	go s.refresh()
	return nil
}
