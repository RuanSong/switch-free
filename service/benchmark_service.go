package service

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// BenchmarkTarget 测评目标（上游 + 模型）
type BenchmarkTarget struct {
	Upstream string `json:"upstream"` // joycode/deveco/opencode/workbuddy
	Model    string `json:"model"`    // 代理内部 id
}

// BenchmarkResult 单个上游模型测评结果
type BenchmarkResult struct {
	Upstream      string  `json:"upstream"`
	UpstreamLabel string  `json:"upstreamLabel"`
	Model         string  `json:"model"`
	Success       bool    `json:"success"`
	DurationMs    int64   `json:"durationMs"`   // 总耗时（端到端）
	OutputTokens  int     `json:"outputTokens"` // 输出 token
	TPS           float64 `json:"tps"`          // output_tokens / (duration/1000)
	Content       string  `json:"content,omitempty"` // 返回文本（供比对）
	ErrorMsg      string  `json:"errorMsg,omitempty"`
	StartedAt     int64   `json:"startedAt"` // 开始时间戳（ms）
}

// BenchmarkService 模型测评服务（暴露给前端）
type BenchmarkService struct {
	core *Core
}

func NewBenchmarkService(core *Core) *BenchmarkService {
	return &BenchmarkService{core: core}
}

func benchmarkLabel(up string) string {
	switch up {
	case "joycode":
		return "京东 JoyCode"
	case "deveco":
		return "华为 DevEco"
	case "opencode":
		return "OpenCode Zen"
	case "workbuddy":
		return "腾讯 WorkBuddy"
	default:
		return up
	}
}

// RunBenchmark 并发测评多个 target，每完成一个 emit "benchmark:progress"，返回全部结果
func (s *BenchmarkService) RunBenchmark(targets []BenchmarkTarget, prompt string, maxTokens int) []BenchmarkResult {
	if prompt == "" {
		prompt = "请详细介绍 Go 语言的 goroutine 和 channel 并发模型，包括基本概念、使用示例和注意事项。"
	}
	if maxTokens <= 0 {
		maxTokens = 1024
	}

	port := 8787
	if srv := s.core.Server(); srv != nil && srv.Port > 0 {
		port = srv.Port
	}
	url := fmt.Sprintf("http://127.0.0.1:%d/v1/messages", port)

	results := make([]BenchmarkResult, len(targets))
	var wg sync.WaitGroup

	for i, t := range targets {
		wg.Add(1)
		go func(idx int, target BenchmarkTarget) {
			defer wg.Done()
			res := s.benchOne(url, target, prompt, maxTokens)
			results[idx] = res
			s.core.EmitEvent("benchmark:progress", res)
		}(i, t)
	}
	wg.Wait()
	return results
}

// benchOne 测单个 target：走本代理 /v1/messages 流式，实时推送 content chunk 给前端
func (s *BenchmarkService) benchOne(url string, target BenchmarkTarget, prompt string, maxTokens int) BenchmarkResult {
	res := BenchmarkResult{
		Upstream:      target.Upstream,
		UpstreamLabel: benchmarkLabel(target.Upstream),
		Model:         target.Model,
		StartedAt:     time.Now().UnixMilli(),
	}

	reqBody := map[string]interface{}{
		"model": target.Model,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
		"max_tokens": maxTokens,
		"stream":     true,
	}
	bodyBytes, _ := json.Marshal(reqBody)

	client := &http.Client{} // 流式：不设整体超时，用 context 控制
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	// 带上配置的 apiKey（代理严格鉴权，不带会 401）
	if srv := s.core.Server(); srv != nil && srv.ConfigResolver != nil {
		if key := srv.ConfigResolver.GetAPIKey(); key != "" {
			req.Header.Set("x-api-key", key)
		}
	}

	start := time.Now()
	httpResp, err := client.Do(req)
	if err != nil {
		res.DurationMs = time.Since(start).Milliseconds()
		res.ErrorMsg = fmt.Sprintf("请求失败: %v", err)
		return res
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode != 200 {
		errBody, _ := io.ReadAll(httpResp.Body)
		res.DurationMs = time.Since(start).Milliseconds()
		snippet := string(errBody)
		if len(snippet) > 100 {
			snippet = snippet[:100]
		}
		res.ErrorMsg = fmt.Sprintf("HTTP %d: %s", httpResp.StatusCode, snippet)
		return res
	}

	// 读 SSE 流，解析 Anthropic content_block_delta，累积 content 并实时推送 chunk 事件
	var contentBuilder strings.Builder
	var outputTokens int
	scanner := bufio.NewScanner(httpResp.Body)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			break
		}
		var ev struct {
			Type  string `json:"type"`
			Delta struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"delta"`
			Usage struct {
				OutputTokens int `json:"output_tokens"`
			} `json:"usage"`
		}
		if err := json.Unmarshal([]byte(data), &ev); err != nil {
			continue
		}
		if ev.Type == "content_block_delta" && ev.Delta.Type == "text_delta" && ev.Delta.Text != "" {
			contentBuilder.WriteString(ev.Delta.Text)
			s.core.EmitEvent("benchmark:chunk", map[string]interface{}{
				"upstream": target.Upstream,
				"delta":    ev.Delta.Text,
			})
		}
		if ev.Type == "message_delta" && ev.Usage.OutputTokens > 0 {
			outputTokens = ev.Usage.OutputTokens
		}
	}

	res.DurationMs = time.Since(start).Milliseconds()
	res.Success = true
	res.OutputTokens = outputTokens
	if res.DurationMs > 0 && outputTokens > 0 {
		res.TPS = float64(outputTokens) / (float64(res.DurationMs) / 1000.0)
	}
	res.Content = contentBuilder.String()
	return res
}
