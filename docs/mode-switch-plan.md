# 模式切换与模型优先级配置方案

> 目标：把"模型选择"从硬编码变成用户可配置的运行时策略。
> 支持 auto / 手动两种模式，auto 按可配置优先级链尝试，手动严格走指定模型但可配降级链，两者都有全局兜底。

## 一、需求对齐

### 两个模式

**auto 模式**（客户端发 `auto` 或不指定 model，即 cc-switch 默认行为）：
- 按"agent 分组排序的优先级链"依次尝试
- **任何失败都降级**到下一个（凭据失效、4xx/5xx、超时、网络错误、上游业务错误、空内容）
- 全部失败试全局兜底

**手动模式**（客户端发具体 model，如 `glm-5.1`、`Kimi-K2.6-agent`）：
- 严格走该模型所属 agent（如 `glm-5.1` -> DevEco）
- 失败时按该模型配置的 `manualFallbacks` 降级链尝试
- 再失败试全局兜底

### 配置持久化
`~/.config/switch-free/config.json`，GUI 修改后实时热加载（无需重启代理）。

## 二、配置数据结构（方案 B）

```jsonc
{
  "mode": "auto",                    // "auto" | "manual"
  // auto 模式：按 agent 分组的有序链（agent 间有先后，agent 内 models 也有先后）
  "autoChain": [
    { "upstream": "deveco",   "models": ["glm-5.1"] },
    { "upstream": "joycode",  "models": ["Kimi-K2.6-agent", "GLM-5.1-agent"] },
    { "upstream": "opencode", "models": ["deepseek-v4-flash-free"] }
  ],
  // 手动模式：指定模型失败时该模型自己的降级链
  "manualFallbacks": {
    "glm-5.1":         [ { "upstream": "joycode",  "model": "GLM-5.1-agent" } ],
    "JoyAI-Code-1.5":  [ { "upstream": "deveco",   "model": "glm-5.1" } ]
  },
  // 全局兜底（auto 链全失败 / 手动降级链全失败 时用）
  "globalFallback": { "upstream": "joycode", "model": "JoyAI-Code-1.5" }
}
```

### 路由行为

```
auto 模式：
  展开.autoChain = [deveco/glm-5.1, joycode/Kimi-K2.6, joycode/GLM-5.1, opencode/deepseek-free]
  依次尝试，任一成功即返回；全失败试.globalFallback；仍失败返回最后错误

手动模式（客户端发 model=X）：
  1. 解析 X 所属 upstream（如 glm-5.1 -> deveco）
  2. 试 upstream/X
  3. 失败 -> 试 manualFallbacks[X] 链（若配了）
  4. 再失败 -> 试 globalFallback
  5. 仍失败返回最后错误
```

### 默认配置（首次启动无配置文件时）
等价于当前硬编码行为，保证平滑迁移：
```json
{
  "mode": "auto",
  "autoChain": [
    { "upstream": "deveco",  "models": ["glm-5.1"] },
    { "upstream": "joycode", "models": ["JoyAI-Code-1.5"] }
  ],
  "manualFallbacks": {},
  "globalFallback": { "upstream": "joycode", "model": "JoyAI-Code-1.5" }
}
```

## 三、后端改动

### 3.1 新增 `config/config.go` - 配置管理

```go
package config

type ModelRef struct {
    Upstream string `json:"upstream"` // "joycode" | "deveco" | "opencode"
    Model    string `json:"model"`    // 模型 id
}

type AgentModels struct {
    Upstream string   `json:"upstream"`
    Models   []string `json:"models"`
}

type Config struct {
    Mode            string                 `json:"mode"`            // "auto" | "manual"
    AutoChain       []AgentModels          `json:"autoChain"`
    ManualFallbacks map[string][]ModelRef  `json:"manualFallbacks"` // key = 模型 id
    GlobalFallback  ModelRef               `json:"globalFallback"`

    path string // 配置文件路径（内部用）
}

// Load 从 ~/.config/switch-free/config.json 加载；不存在返回默认配置
func Load() (*Config, error)

// Save 保存配置到磁盘
func (c *Config) Save() error

// Defaults 默认配置
func Defaults() *Config

// Validate 校验配置合法性（upstream 名、模型 id 是否在白名单）
func (c *Config) Validate() error
```

特性：
- 启动时 `Load()`，文件不存在用 `Defaults()` 并写盘
- 配置变更通过 `fsnotify` 监听文件变化，或 GUI 保存后主动通知（选后者更简单）
- 热加载：`Config` 持有 `sync.RWMutex`，路由层每次请求读最新配置

### 3.2 新增 `config/resolver.go` - 模型解析器

替代现有 `proxy/models.go` 里的 `ResolveModel` / `ResolveUpstream` 硬编码逻辑：

```go
// Resolve 解析请求 model -> 有序尝试列表（含降级链 + 兜底）
// 返回的列表按顺序尝试，第一个成功即返回
func (c *Config) Resolve(requestedModel string) []ModelRef {
    if requestedModel == "" || requestedModel == "auto" {
        // auto 模式：展开 autoChain
        return c.expandAutoChain()
    }
    // 手动模式：指定模型 + 它的降级链 + 兜底
    return c.expandManual(requestedModel)
}

func (c *Config) expandAutoChain() []ModelRef
func (c *Config) expandManual(model string) []ModelRef
```

### 3.3 改造 `proxy/router.go` - 路由层

当前 `callUpstreamAnthropic` / `callUpstreamOpenAI` 的 auto 降级逻辑（硬编码 DevEco->JoyCode）替换为：

```go
func (s *Server) callUpstream(ctx context.Context, body, requestedModel) (*Response, error) {
    cfg := s.configManager.Get()              // 拿当前配置
    chain := cfg.Resolve(requestedModel)      // 有序尝试列表

    var lastErr error
    for _, ref := range chain {
        upstream := s.pickUpstream(ref.Upstream)
        // 检查该 upstream 凭据是否有效，无效直接跳过（不算尝试，省一次失败请求）
        if !upstream.HasValidCreds() {
            continue
        }
        resp, err := upstream.Call(ctx, body)
        if err == nil && !isUpstreamErrorResponse(resp) {
            return resp, nil  // 成功
        }
        lastErr = wrapErr(ref, err, resp)
        s.recordLog(... "fallback" ...)  // 记录降级
        continue
    }
    return nil, lastErr  // 全失败
}
```

关键变化：
- 不再硬编码"DevEco 失败降级 JoyCode"，改为遍历 `cfg.Resolve()` 返回的链
- **凭据无效的上游跳过**（不发起请求，直接降级），这是相对当前行为的重要改进--现在凭据失效会发起请求收到 401 才降级，配置化后可预判跳过
- 每次降级记一条 `fallback` 日志，前端可见

### 3.4 `proxy.Server` 持有配置管理器

```go
type Server struct {
    ...
    configManager *config.Manager  // ★ 新增
}
```

### 3.5 新增 `service/config_service.go` - 配置服务（暴露给前端）

```go
type ConfigService struct {
    mgr *config.Manager
}

func (s *ConfigService) GetConfig() *config.Config            // 拉当前配置
func (s *ConfigService) SaveConfig(c *config.Config) error    // 保存（校验 + 写盘 + 热加载）
func (s *ConfigService) ResetConfig() error                   // 重置为默认
func (s *ConfigService) GetAvailableModels() []AgentModels    // 可用模型（按 upstream 分组，仅凭据有效的）
```

`GetAvailableModels` 只返回**当前凭据有效**的 upstream 的模型，引导用户只配可用的。

## 四、前端改动

### 4.1 新增「设置」页（侧边栏加一项 ⚙️）

布局：
```
┌─ 模式设置 ────────────────────────────┐
│ 运行模式： (●) auto  ( ) 手动          │
│                                        │
│ ─ auto 模式优先级链 ─                 │
│ ┌─────────────────────────────┐ ┌────┐ │
│ │ DevEco / glm-5.1            │ │ ↑↓ │ │
│ │ JoyCode / Kimi-K2.6-agent   │ │ ↑↓ │ │
│ │ JoyCode / GLM-5.1-agent     │ │ ↑↓ │ │
│ │ OpenCode / deepseek-free    │ │ ↑↓ │ │
│ └─────────────────────────────┘ │ +  │ │
│                                 │ ✕  │ │
│ [+ 添加模型到链]                      │
│                                        │
│ ─ 全局兜底 ─                          │
│ 兜底模型： [JoyCode / JoyAI-Code-1.5 ▾] │
│                                        │
│ ─ 手动模式降级链（可选） ─            │
│ [glm-5.1] -> [JoyCode/GLM-5.1-agent]   │
│ [+ 添加]                               │
│                                        │
│ [保存]  [重置默认]                     │
└────────────────────────────────────────┘
```

### 4.2 组件拆分
- `Settings.tsx` - 设置页主容器
- `ModeSwitch.tsx` - auto/手动 模式切换
- `AutoChainEditor.tsx` - auto 链排序编辑器（拖拽或上下移动按钮）
- `FallbackEditor.tsx` - 兜底 + 手动降级链编辑器
- `ModelPicker.tsx` - 模型选择下拉（upstream + model 二级选择，仅显示有效的）

### 4.3 交互
- 模式切换：单选，切换后立即生效（保存配置）
- auto 链：列表项可上下移动、删除、添加；每项是「upstream + model」下拉
- 兜底：单个「upstream + model」下拉
- 手动降级链：key-value 列表，左边选模型，右边配它的降级链
- 保存按钮：调 `ConfigService.SaveConfig`，成功后 toast 提示
- 重置：调 `ResetConfig`

### 4.4 仪表盘显示当前模式
Dashboard 顶部加一行：「当前模式：auto / 手动」，auto 时显示链首模型，手动时提示「按客户端指定模型」。

## 五、改动文件清单

### 后端（新增）
- `config/config.go` - Config 结构 + Load/Save/Defaults/Validate
- `config/resolver.go` - Resolve 逻辑（auto/manual 展开降级链 + 兜底）
- `config/manager.go` - 配置管理器（加载/热加载/Get 线程安全）
- `service/config_service.go` - 暴露给前端的服务

### 后端（改造）
- `proxy/router.go` - `callUpstream` 改用 `config.Resolve` 遍历链，凭据预判跳过
- `proxy/server.go` - 持有 `config.Manager`
- `proxy/models.go` - 保留模型白名单和映射，但 `ResolveModel`/`ResolveUpstream` 的 auto 路由逻辑移到 `config/resolver.go`
- `main.go` - 初始化 `config.Manager`，注入 Server 和 ConfigService
- `service/proxy_service.go` - `GetStatus` 加返回当前模式

### 前端（新增）
- `frontend/src/components/Settings.tsx`
- `frontend/src/components/ModeSwitch.tsx`
- `frontend/src/components/AutoChainEditor.tsx`
- `frontend/src/components/FallbackEditor.tsx`
- `frontend/src/components/ModelPicker.tsx`

### 前端（改造）
- `frontend/src/App.tsx` - 侧边栏加「设置」tab
- `frontend/src/components/Dashboard.tsx` - 显示当前模式

### 重新生成绑定
- `wails3 generate bindings -ts -d frontend/bindings ./...`

## 六、兼容性

- **cc-switch 配置不变**：`http://127.0.0.1:8787` + `sk-joycode` + model=`auto`
- **默认配置等价当前行为**：auto 链 = DevEco/glm-5.1 -> JoyCode/JoyAI-Code-1.5，兜底 = JoyCode/JoyAI-Code-1.5，跟现在硬编码一致
- 老用户升级后无感知，新配置文件自动生成

## 七、边界情况

| 情况 | 处理 |
|------|------|
| 配置文件损坏/非法 JSON | 用默认配置 + 日志警告 |
| auto 链为空 | 直接用 globalFallback |
| 手动指定的 model 不在任何 upstream 白名单 | 返回错误「未知模型 X」 |
| manualFallbacks 里 key 是未知模型 | Validate 时忽略并告警 |
| 链里某 upstream 凭据无效 | 跳过该模型（不发起请求），记 fallback 日志 |
| 所有链 + 兜底都失败 | 返回最后一条错误 |
| 用户配置了 upstream 不支持的模型（如 joycode/glm-5.1） | Validate 报错拒绝保存 |

## 八、验证方式

1. 默认配置启动，发 `auto` 请求 -> 走 DevEco，行为同现在
2. 改 auto 链顺序，发 `auto` -> 按新顺序走
3. 发具体 model（手动模式）-> 严格走该模型，失败按 manualFallbacks 降级
4. 配置一个凭据失效的 upstream 到链里 -> 该项被跳过，日志可见
5. 全部失败 -> 走 globalFallback
6. GUI 改配置保存 -> 下一个请求立即生效（热加载）
7. 重启应用 -> 配置从文件恢复
