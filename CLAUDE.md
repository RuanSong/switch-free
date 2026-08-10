# Switch Free (Wails v3 版)

## 项目性质
把 JoyCode/DevEco/OpenCode/WorkBuddy 四套 AI 编程工具的模型能力，通过本地代理暴露为标准 Anthropic/OpenAI 接口，供 Claude Code/cc-switch 复用。带 Wails v3 桌面 GUI。

## 关键架构
- 代理 HTTP 服务独立运行在 `127.0.0.1:8787`（`proxy.Server`），与 Wails 窗口共存
- 四上游统一实现 `upstream.Upstream` 接口（Call/VerifyCreds/EnsureCreds/InvalidateCreds/CredStatus）
- 凭据动态读取，不硬编码：JoyCode 从 state.vscdb（用 `modernc.org/sqlite` 纯 Go 驱动，无需外部 sqlite3 CLI），DevEco 三层 AES-256-GCM 解密，OpenCode 明文 auth.json，WorkBuddy 明文 info + refreshToken 续期
- 凭据路径跨平台解析（`paths.*Candidates()`，单一真相）：macOS `~/Library/Application Support`、Linux `~/.config`、Windows `%APPDATA%`；`IsAgentInstalled` 探测与 `EnsureCreds` 加载共用同一套候选路径，环境变量 `JOYCODE_VSCDB`/`WORKBUDDY_INFO_PATH`/`OPENCODE_AUTH_PATH` 等可覆盖
- 运行模式分 auto / manual，降级链均可通过前端 Settings 自由配置
- auto 模式：`Config.AutoChain []AgentModels` 按优先级排列，每个 AgentModels 按 upstream 分组包含模型列表；`expandAutoChain()` 扁平化为 `[]ModelRef` 执行，末尾追加 `GlobalFallback`（去重）
- manual 模式：`Config.ManualFallbacks map[string][]ModelRef` 为每个模型单独配降级链，`expandManual()` 解析请求模型 → 查链 → 追加 GlobalFallback
- 全局兜底：`Config.GlobalFallback ModelRef`，两种模式共享，所有链都失败时的最终保底
- 降级执行：`router.executeChain` / `executeChainStream` 逐个尝试，跳过凭据无效的 upstream，任意成功即返回
- 默认 auto 链为空（用户自行配置）；旧版硬编码的 DevEco→JoyCode 降级已废弃（`models.go` 中 `AutoModel`/`AutoModelJoyCodeFallback` 常量为遗留残留）
- 上游统一非流式；下游要流式时代理把完整响应拆成 SSE（伪流式）
- **WorkBuddy 例外**：上游强制 stream:true（非流式返回 `code:11101`），`Call` 内部读 SSE 流经 `aggregateOpenAISSE` 聚合成完整 OpenAIResponse JSON，对上层透明，复用非流式处理逻辑
- 模型 id 隔离：WorkBuddy 模型用 `wb/` 前缀（如 `wb/glm-5.0`），避免与 DevEco 的 glm-5.1 重名；`ResolveUpstream` 识别前缀路由，发上游前 `stripWbPrefix` 还原
- service.Core 实现 proxy.EventLogger，记录日志 + 通过 `application.Get().Event.Emit` 推送事件到前端

## 构建
```bash
wails3 build          # 产物 bin/switch-free
wails3 dev            # 开发热重载
wails3 task build:server  # 无 GUI 服务模式
make build-binaries V=x.y.z  # 构建全平台裸二进制到 dist/（发布/自动更新用）
```
改了 Go service 签名后需重新生成绑定：`wails3 generate bindings -ts -d frontend/bindings ./...`

## 自动更新机制
- 版本号来源：`build/config.yml` 的 `info.version`，构建时 `make sync-version` 同步到 `version/config.yml`（Go embed 读取）
- 检测时机：启动后延迟 3s 后台检查（`main.go: startUpdateCheck`）+ 前端 UpdatePanel 手动「检查更新」
- 检测逻辑（`updater/github.go`）：GET `https://api.github.com/repos/{Owner}/{Repo}/releases/latest`，比较 `tag_name`（去 v 前缀）与当前版本，仅看 **non-prerelease 的最新 release**
- 资产名匹配（`assetName()` 硬编码）：`switch-free-darwin-arm64` / `switch-free-darwin-amd64` / `switch-free-windows-amd64.exe`（+ linux 运行时检测 `switch-free-linux-amd64`，但 `make build-binaries` 暂不构建 linux），**发布时资产文件名必须精确匹配**
- 下载应用（`updater/updater.go`）：`downloadAsset` 下载到临时文件 -> `github.com/minio/selfupdate` 原子替换运行中二进制 -> 提示重启
- 配置：`Config.AutoUpdate`（Enabled/Provider/GitHub{Owner,Repo,Token}/UpdateURL/Channel），默认 `RuanSong/switch-free` 公开仓库，无需 Token

## 发布流程（一键）
```bash
make deploy V=0.0.4   # 构建 -> 推码 -> 打 tag -> 创建 Release -> 上传裸二进制
```
deploy 链：`build-binaries -> push -> tag -> release -> upload`
- `build-binaries` 依赖 `build-frontend`（只构建一次）+ 三个平台裸二进制
- 产物名必须匹配 `assetName()`，否则 updater 找不到对应平台资产
- 注意：tag 必须是 `vX.Y.Z` 格式，Release 不能是 prerelease（否则 `/releases/latest` 返回不到）

## 验证过的能力
- /health 返回四上游凭据状态
- /v1/models 返回 31 个模型（含 13 个 `wb/*` 免费档）
- /v1/messages 非流式（auto 降级链）✓
- /v1/messages 流式（降级链 + 伪流式拆分）✓
- /v1/messages 非流式 + 流式（`wb/glm-5.0`->WorkBuddy，SSE 聚合 + 伪流式拆分）✓
- /v1/chat/completions ✓
- 首页「今日输出速率」统计（加权 tok/s + 按模型排名）✓
- 凭据四上游全部有效（实测 2026-08-08）

## 与旧版关系
`../switch-free/` 是原 Node.js 单文件版（joycode-color-proxy.js）。本目录是 Go+GUI 重构，端口/路由/格式完全兼容，可 drop-in 替换。逆向过程文档在旧版目录。

## 降级链关键文件
| 文件 | 职责 |
|---|---|
| `config/config.go` | Config 数据结构（AutoChain/ManualFallbacks/GlobalFallback）、默认值、Validate 校验、加载/保存 |
| `config/resolver.go` | Resolve/expandAutoChain/expandManual — 降级链展开逻辑 |
| `config/manager.go` | Manager 线程安全包装、SaveConfig/ResetConfig |
| `proxy/router.go` | executeChain/executeChainStream — 降级链遍历执行、ModelRef/ConfigResolver 定义 |
| `proxy/models.go` | ResolveModel/ResolveUpstream — 模型名解析、AutoModel 常量（遗留） |
| `service/config_service.go` | 前端-后端桥接，SaveConfig/GetConfig/GetAvailableModels |
| `frontend/src/components/Settings.tsx` | 降级链编辑 UI（auto 链拖排、手动降级链、全局兜底选择器） |
