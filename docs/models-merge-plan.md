# 模型列表实时获取与本地映射合并方案

> 目标：设置页的模型选项改为「接口实时获取 + 本地映射合并」。
> 接口能拿到数据时，以接口返回的模型为主，本地静态白名单作为映射元数据补充；
> 接口拿不到时，回退到本地静态白名单；本地 auto 虚拟模型始终保留。

## 一、合并规则（核心逻辑）

对每个 upstream 独立执行：

```
接口模型列表 = 调上游 models 接口拉取（实时）
本地映射表 = proxy/models.go 的静态白名单（含 id/label/upstream名/context/output/能力）

合并结果 = []
1. 若接口拉取成功：
   a. 遍历接口返回的每个模型：
      - id = 接口返回的 id（如 JoyCode 的 chatApiModel、DevEco 的 model_id、OpenCode 的 id）
      - 若本地映射表里有同 id -> 用本地的 label/context/output/能力 等元数据
      - 若本地没有 -> 用接口能提供的元数据（JoyCode/OpenCode 接口给的信息多，DevEco 也够），缺的字段留默认
      - 加入合并结果
   b. auto 处理：接口都没返回 auto，所以本地 auto 虚拟模型始终保留并加入合并结果（仅 DevEco/joycode 上游加，opencode 不加 auto）
2. 若接口拉取失败（凭据无效/网络错误）：
   - 回退到本地静态白名单（现有行为）
   - 同样保留本地 auto 虚拟模型
```

**auto 虚拟模型保留规则**（按你的要求"本地 auto 需要保留"）：
- JoyCode：保留本地定义的（无独立 auto，auto 是客户端概念）
- DevEco：保留 `glm-5.1` 作为 auto 主力（本地 AutoModel 常量）
- OpenCode：不加 auto（OpenCode 无 auto 概念，且模型太多）

> 注：三个接口实测都不返回 "auto" 模型，所以"以接口 auto 为主"这条不会触发，
> 本地 auto 始终保留。如果未来某上游接口返回了 auto，会优先用接口的。

## 二、各上游接口映射

### JoyCode (`joycode_modelList`)
```
接口字段          -> 本地字段
chatApiModel      -> ID（实际调用用）
label             -> Label
features          -> ToolCall (含 function_call) / Vision (含 vision)
respMaxTokens     -> Output
maxTotalTokens    -> Context
supportStream     -> Stream
```
合并后模型数 = 接口返回数（实测 8 个，与本地白名单一致）

### DevEco (`/codeGenie/modelConfig`)
```
接口字段                          -> 本地字段
model_id                          -> ID（如 "GLM-5.1"）+ 同时作为调用 id
                                   （本地映射：id=glm-5.1, upstream=GLM-5.1，这里要处理大小写映射）
model_id                          -> Label
context_window                    -> Context
output                            -> Output
input_modalities 含 "image"       -> Vision
tool_call_mode != "none"          -> ToolCall
thinking_mode                     -> Reasoning（新字段，是否推理模型）
```
合并后模型数 = 接口返回数（实测 2 个：GLM-5.1 + Qwen3_VL）
**注意**：DevEco 本地白名单只有 `glm-5.1`（id）映射到 `GLM-5.1`（upstream）。
接口返回 `GLM-5.1`，要反向映射成本地 id `glm-5.1` 才能被 ResolveModel 识别。

### OpenCode (`/models`)
```
接口字段 -> 本地字段
id       -> ID + Label
```
合并后模型数 = 接口返回数（实测 61 个，含 8 个 free）
OpenCode 接口只返回 id，无 context/output/能力信息。
本地白名单的 8 个 free 模型有 context/output -> 合并时补上。
其余 53 个非 free 模型 -> 只有 id，元数据留默认（0/未知）。

## 三、后端实现

### 3.1 新增 `upstream/models_fetcher.go` - 各上游拉取模型列表

给 `Upstream` 接口加方法：
```go
// FetchModels 拉取上游可用模型列表（实时）
// 失败时返回 nil + error（由调用方回退本地白名单）
FetchModels(ctx context.Context) ([]FetchedModel, error)
```

`FetchedModel` 统一结构：
```go
type FetchedModel struct {
    ID        string   // 实际调用用的 model id
    Label     string   // 显示名
    Context   int
    Output    int
    Stream    bool
    Vision    bool
    ToolCall  bool
    Reasoning bool
}
```

三个上游分别实现：
- `joycode.go`: 调 `joycode_modelList`，解析 `data[]`
- `deveco.go`: 调 `modelConfig`，解析 `body.inner_models[].model_configs[]`，反向映射 model_id -> 本地 id
- `opencode.go`: 调 `/models`，解析 `data[]`

### 3.2 新增 `proxy/models_merge.go` - 合并逻辑

```go
// MergeModels 合并接口模型 + 本地映射 + auto 虚拟模型
func MergeModels(upstream string, fetched []FetchedModel) []ModelInfo
```
逻辑：
1. 遍历 fetched，每个查本地映射表补充元数据
2. 追加本地 auto 虚拟模型（deveco/joycode）
3. 返回合并结果

### 3.3 改造 `service/config_service.go` 的 `GetAvailableModels`

```go
func (s *ConfigService) GetAvailableModels() []UpstreamModels {
    // 并发拉取三个上游（超时 5s）
    // 每个上游：FetchModels 成功 -> MergeModels；失败 -> 本地白名单 + auto
}
```

需要 `ConfigService` 持有三个 upstream 引用（或通过 Core）。

## 四、前端

无需大改。`GetAvailableModels` 返回的结构不变（`[]UpstreamModels{ upstream, models[] }`），
只是 `models` 内容变成实时合并结果。前端设置页自动显示最新模型。

可选增强：模型项加「来源」标记（实时/本地），让用户知道是接口拉的还是兜底的。
加「刷新模型列表」按钮，重新拉取。

## 五、缓存

接口拉取有成本（3 个网络请求），加内存缓存：
- 首次 `GetAvailableModels` 拉取并缓存
- 提供 `RefreshModels()` 强制刷新
- 缓存有效期 10 分钟（或手动刷新）
- 凭据失效时缓存失效（下次重新拉）

## 六、改动文件清单

### 后端新增
- `upstream/models_fetcher.go` - `FetchedModel` 结构 + `FetchModels` 接口方法
- `proxy/models_merge.go` - `MergeModels` 合并逻辑

### 后端改造
- `upstream/interface.go` - `Upstream` 接口加 `FetchModels`
- `upstream/joycode.go` - 实现 `FetchModels`
- `upstream/deveco.go` - 实现 `FetchModels`（含 model_id 反向映射）
- `upstream/opencode.go` - 实现 `FetchModels`
- `service/config_service.go` - `GetAvailableModels` 改为实时合并 + 缓存；加 `RefreshModels`
- `service/core.go` - Core 持有 upstream 引用（已有），ConfigService 通过 Core 访问

### 前端（可选）
- `Settings.tsx` - 加「刷新模型列表」按钮 + 来源标记

### 重新生成绑定

## 七、验证

1. 三上游凭据有效：设置页显示接口返回的实时模型列表
2. 某上游凭据失效：该上游显示本地白名单（兜底）
3. auto 虚拟模型始终在 DevEco/JoyCode 列表里
4. DevEco 接口返回 GLM-5.1，能正确反向映射到本地 id glm-5.1
5. OpenCode 61 个模型全部显示，8 个 free 带元数据
6. 刷新按钮能重新拉取
