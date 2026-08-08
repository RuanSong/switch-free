# 缓存命中率统计方案

> 目标：统计请求的缓存命中情况，在日志页趋势图展示。
> 实测确认：OpenCode 返回 `prompt_tokens_details.cached_tokens`（prompt 884 中 768 命中），
> DevEco/JoyCode 目前不返回缓存字段。

## 一、需求对齐

| 项 | 决定 |
|----|------|
| 命中率口径 | 请求命中率 + token 命中率 都展示 |
| 展示位置 | 日志页趋势图 |
| 数据入库 | LogEntry 加 `cacheHitTokens` 字段，新请求开始记录；历史日志按 0 计 |
| 缓存来源 | 上游 usage 里的缓存字段（当前仅 OpenCode） |

### 命中率定义
- **请求命中率** = 有 `cacheHitTokens > 0` 的请求数 / 总请求数
- **token 命中率** = 命中缓存 token 总数 / 输入 token 总数

## 二、缓存字段提取

上游响应 usage 里可能出现两种格式：

| 格式 | 来源 | 提取 |
|------|------|------|
| `prompt_tokens_details.cached_tokens` | OpenCode / DeepSeek | `usage.prompt_tokens_details.cached_tokens` |
| `cache_read_input_tokens` | Anthropic 标准 | `usage.cache_read_input_tokens` |

`enrichUsage` 里同时解析两种，命中任一即记录 `cacheHitTokens`。

## 三、后端改动

### 3.1 LogEntry 新增字段
```go
type LogEntry struct {
    ...
    CacheHitTokens int `json:"cacheHitTokens,omitempty"` // 命中缓存的输入 token
}
```

### 3.2 响应解析（proxy/handlers.go enrichUsage）
```go
// 解析缓存字段
if u.PromptTokensDetails.CachedTokens > 0 {
    entry.CacheHitTokens = u.PromptTokensDetails.CachedTokens
}
if u.CacheReadInputTokens > 0 {
    entry.CacheHitTokens = u.CacheReadInputTokens
}
```
`OpenAIUsage` 结构扩展 `PromptTokensDetails` 和 `CacheReadInputTokens` 字段。

### 3.3 趋势聚合（service/logtrend.go）
`TrendPoint` 加缓存字段：
```go
type TrendPoint struct {
    Label        string `json:"label"`
    Tokens       int64  `json:"tokens"`
    Reqs         int64  `json:"reqs"`
    CacheHitTokens int64 `json:"cacheHitTokens"` // 命中缓存 token
    CacheHitReqs  int64  `json:"cacheHitReqs"`   // 命中缓存的请求数
}
```
`ComputeUsageTrend` 填充缓存统计。

### 3.4 历史日志兼容
历史日志无 `cacheHitTokens` 字段，反序列化为 0，不影响统计（命中率按 0 计）。

## 四、前端改动（日志页趋势图）

`UsageTrendChart.tsx` 增强：
- 每条柱悬停 tooltip 加缓存信息：`命中 X / 输入 Y (Z%)`
- 图上加一条 **缓存命中率折线/比例**（用 token 命中率）或第二层柱
- 右上角汇总：请求命中率 + token 命中率

设计：双柱（总 token 柱 + 命中缓存柱 叠加）+ tooltip 显示命中率。

## 五、边界情况

| 情况 | 处理 |
|------|------|
| 上游不返回缓存字段 | `cacheHitTokens=0`，命中率 0，tooltip 显示"不支持缓存" |
| 首次请求（前缀无缓存） | 命中 0，正常 |
| 历史日志 | cacheHitTokens=0 |
| 输入 token 为 0 | token 命中率按 0 处理，避免除零 |

## 六、改动文件清单

### 后端
- `proxy/types.go` - LogEntry 加 `CacheHitTokens`；`OpenAIUsage` 加 `PromptTokensDetails`/`CacheReadInputTokens`
- `proxy/handlers.go` - enrichUsage 提取缓存字段
- `service/logtrend.go` - TrendPoint 加缓存字段 + 聚合
- 重新生成绑定

### 前端
- `frontend/src/components/UsageTrendChart.tsx` - 缓存命中率展示

## 七、验证

1. OpenCode 连续两次相同长请求：第二次日志 cacheHitTokens=768
2. 趋势图 tooltip 显示命中率
3. DevEco 请求 cacheHitTokens=0（不支持缓存）
4. 历史日志不报错，命中率 0
