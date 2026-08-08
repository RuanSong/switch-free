package upstream

import (
	"io"
	"net/http"
	"strings"
	"time"
)

// FetchedModel 从上游接口实时拉取的模型信息（统一结构）
type FetchedModel struct {
	ID        string // 实际调用用的 model id
	Label     string // 显示名
	Context   int    // 上下文窗口（token）
	Output    int    // 最大输出（token）
	Stream    bool   // 是否支持流式
	Vision    bool   // 是否支持视觉
	ToolCall  bool   // 是否支持工具调用
	Reasoning bool   // 是否推理模型
}

// httpGet 简单 GET 请求（带超时），返回 body + statusCode
func httpGet(url string, headers map[string]string) ([]byte, int, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, 0, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
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

// httpPost 简单 POST 请求（带超时）
func httpPost(url, payload string, headers map[string]string) ([]byte, int, error) {
	req, err := http.NewRequest("POST", url, strings.NewReader(payload))
	if err != nil {
		return nil, 0, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
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
