package upstream

import (
	"context"

	"switchfree/creds"
)

// Upstream 上游适配器统一接口
type Upstream interface {
	// Call 调用上游（传入 OpenAI 格式 body 的 JSON 字节），返回上游原始响应
	Call(ctx context.Context, body []byte) (*Response, error)
	// VerifyCreds 预检凭据有效性
	VerifyCreds(ctx context.Context) (*VerifyResult, error)
	// EnsureCreds 确保凭据有效（含自动恢复）
	EnsureCreds(ctx context.Context) error
	// InvalidateCreds 标记凭据失效
	InvalidateCreds()
	// CredStatus 当前凭据状态
	CredStatus() *creds.CredStatusInfo
	// HasValidCreds 凭据当前是否有效（快速检查，不触发网络请求）
	HasValidCreds() bool
	// FetchModels 拉取上游可用模型列表（实时，失败返回 error 由调用方回退本地白名单）
	FetchModels(ctx context.Context) ([]FetchedModel, error)
	// Name 上游名称
	Name() string
}

// Response 上游响应
type Response struct {
	StatusCode int
	Body       []byte
	ReqID      string
}

// VerifyResult 预检结果
type VerifyResult struct {
	Valid  bool
	Code   int
	Status int
}