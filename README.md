# ⚡ Switch Dev

<div align="center">

**让你的 Claude Code / Codex 免费跑 GLM-5.1、Kimi、DeepSeek、MiniMax、豆包、混元、Hy3**

一个本地代理 + 桌面管理面板，复用你已安装的 AI 编程工具登录态，
把免费模型额度暴露为标准 Anthropic / OpenAI 接口。

`免费` · `本地运行` · `零 API Key` · `多上游自动切换` · `用量统计`

</div>

---

## 🎯 30 秒看懂它

**你已经有免费的大模型额度，只是用不上。**

装过 JoyCode、DevEco Code、OpenCode 或 WorkBuddy 的开发者，后台都有免费模型额度（京东云的 JoyAI/Kimi/GLM、华为的 GLM-5.1、OpenCode Zen 的 free 模型、腾讯 CodeBuddy 的 GLM/MiniMax/Kimi/DeepSeek/混元）。但这些工具的凭据是**登录态**，请求要**动态签名**、要**注入业务字段**，没法直接配进 Claude Code / Codex。

Switch Dev 把这一切封装成一句话：

> **装好并登录一个工具 → 启动 Switch Dev → Claude Code 指向 `127.0.0.1:8787` → 免费开跑。**

---

## ✨ 为什么值得用

| 对比 | 官方 API | **Switch Dev** |
|------|---------|-----------------|
| 成本 | 💸 按 token 付费 | **🆓 复用已有免费额度** |
| API Key | 要申请、要绑定支付 | **不需要**，动态读登录态 |
| 配置 | 每家一套 | **统一一个端口** |
| 模型选择 | 单一供应商 | **四上游 32 模型随意切** |
| 用量 | 官网查 | **本地可视化统计 + 费用** |

**适合你，如果：**
- 🧑💻 是开发者，装过 JoyCode / DevEco / OpenCode / WorkBuddy，想省 Claude Code 的钱
- 🔄 想要多模型自由切换，不锁死一个供应商
- 📊 想看清每次请求用了多少 token、花了多少钱、缓存命中率多少
- 🖥 想要一个桌面面板管凭据、看日志、做统计，而不是写配置文件

---

## 🚀 快速开始（3 步）

### 第 1 步：装一个工具并登录（越多模型越丰富）

| 工具 | 安装 | 登录 | 解锁模型 |
|------|------|------|---------|
| **JoyCode** | [官网下载](https://joycode.jd.com) | 打开客户端扫码 | 京东云 8 个（JoyAI/Kimi/GLM/DeepSeek/MiniMax/豆包）|
| **DevEco Code** | `npm i -g @deveco/deveco-code` | `deveco auth login` | 华为 GLM-5.1 免费额度 |
| **OpenCode** | `brew install opencode-ai/tap/opencode` | `opencode auth login` | OpenCode Zen 8 个 free |
| **WorkBuddy** | [官网下载](https://workbuddy.app) | 打开客户端登录 | 腾讯 CodeBuddy 13 个免费（GLM/MiniMax/Kimi/DeepSeek/混元）|

> 首次启动会自动弹引导，提示你装哪个、怎么登录。装一个就能用，四个全装就是"全家桶"。

### 第 2 步：启动

```bash
./bin/switch-dev        # macOS / Linux
# Windows 双击 .exe；macOS 也可打包成 .app 双击
```

启动后：代理服务 `127.0.0.1:8787` 立即可用，托盘图标常驻（关窗口不退出）。

### 第 3 步：接入你的客户端

**Claude Code**（写进 `~/.claude/settings.json`）：
```json
{
  "env": {
    "ANTHROPIC_BASE_URL": "http://127.0.0.1:8787",
    "ANTHROPIC_AUTH_TOKEN": "sk-switchfree",
    "ANTHROPIC_MODEL": "auto"
  }
}
```

**Codex**（`~/.codex/config.toml`）：
```toml
model = "auto"
model_provider = "switchfree"
[model_providers.switchfree]
base_url = "http://127.0.0.1:8787/v1"
wire_api = "chat"
```

**cc-switch**：添加供应商，baseURL `http://127.0.0.1:8787`，apiKey 任意，模型 `auto`。

> `auto` 会按你配置的优先级链自动选模型；也可以指定具体模型（如 `glm-5.1`、`Kimi-K2.6-agent`）。

---

## 🆓 限时免费模型汇总

各平台限时免费的模型（代理设置页模型名旁有 `free` 标识）：

| 上游 | 模型 ID | 说明 |
|------|---------|------|
| **DevEco** | `glm-5.1` | 华为 GLM-5.1，auto 模式主力 |
| **JoyCode** | `JoyAI-Code-1.5` | 京东 JoyAI，auto 模式降级 |
| **OpenCode** | `deepseek-v4-flash-free` / `mimo-v2.5-free` / `ling-3.0-flash-free` / `north-mini-code-free` / `laguna-s-2.1-free` / `ling-3.0-tiny-free` / `nemotron-3-ultra-free` / `longcat-2.0-free` | OpenCode Zen 全部 8 个 free 模型 |
| **WorkBuddy** | `wb/hy3` | 腾讯混元 Hy3 |

> 免费额度由各平台提供，可能随时调整。建议在 auto 模式配置多个免费模型互为降级，单个失效也能继续用。

---

## 🧩 可用模型（32 个）

### JoyCode · 京东云（8 个）

| 模型 ID | 能力 |
|---------|------|
| `JoyAI-Code-1.5` 🆓 | function_call, agent（限时免费）|
| `MiniMax-M3-agent` | function_call, **vision**, agent |
| `MiniMax-M2.7-agent` | function_call, agent |
| `Kimi-K2.6-agent` | function_call, **vision**, agent |
| `GLM-5.1-agent` | function_call, agent |
| `GLM-5-agent` | function_call, agent |
| `DeepSeek-V4-Pro-agent` | function_call, agent |
| `Doubao-Seed-2.0-pro-agent` | function_call, **vision**, agent |

### DevEco · 华为（1 个）

| 模型 ID | Context | Output |
|---------|---------|--------|
| `glm-5.1` 🆓 | 170k | 131k（限时免费）|

### OpenCode Zen · 开源 free（8 个）

| 模型 ID | Context | Output |
|---------|---------|--------|
| `deepseek-v4-flash-free` | 200k | 128k |
| `mimo-v2.5-free` | 262k | 64k |
| `ling-3.0-flash-free` | 262k | 32k |
| `north-mini-code-free` | 256k | 64k |
| `laguna-s-2.1-free` | 256k | 32k |
| `ling-3.0-tiny-free` / `nemotron-3-ultra-free` / `longcat-2.0-free` | — | — |

### WorkBuddy · 腾讯 CodeBuddy（14 个免费）

模型 id 用 `wb/` 前缀（避免与 DevEco 的 `glm-5.1` 重名）；上游强制流式，代理内部聚合 SSE 成完整响应再转 Anthropic/OpenAI，对客户端透明。

| 模型 ID | Output | 能力 |
|---------|--------|------|
| `wb/hy3` 🆓 | 32k | vision, tool, reasoning（限时免费）|
| `wb/auto` | 32k | 自动选择 |
| `wb/glm-5.0` / `wb/glm-5.1` / `wb/glm-5.0-turbo` / `wb/glm-4.7` | 48k | vision, tool, reasoning |
| `wb/minimax-m2.5` / `wb/minimax-m2.7` | 48k | vision, tool, reasoning |
| `wb/kimi-k2.5` / `wb/kimi-k2.6` / `wb/kimi-k2-thinking` | 32k | vision, tool, reasoning |
| `wb/deepseek-v3-2-volc` | 32k | vision, tool, reasoning |
| `wb/hunyuan-2.0-thinking` | 16k | tool, reasoning |
| `wb/hunyuan-2.0-instruct` | 24k | tool |

> 模型列表从上游接口**实时拉取**，上游新增模型会自动出现；费率内置 188 条，可编辑。

---

## 🏗 架构

```
Claude Code / Codex / cc-switch
   │  Anthropic /v1/messages  或  OpenAI /v1/chat/completions
   ▼
┌─────────────── Switch Dev 桌面应用（Wails v3 + Go + React）──────┐
│                                                                   │
│  前端管理面板：仪表盘 / 凭据 / 模型 / 统计 / 日志 / 设置           │
│       ↕ Wails 绑定 + 事件推送                                     │
│  Go 后端                                                          │
│   ├ 代理核心：Anthropic↔OpenAI 转换 / 签名 / SSE / auto 降级      │
│   ├ 四上游适配器：JoyCode / DevEco / OpenCode / WorkBuddy(流式聚合) │
│   └ 凭据管理：vscdb / 三层解密 / 明文auth / 明文info+refresh       │
│                                                                   │
│  独立 HTTP 代理服务 :8787（与 GUI 共存，也可无界面运行）          │
└───────────────────────────────────────────────────────────────────┘
   │           │           │           │
   ▼           ▼           ▼           ▼
api-ai.jd.com  devecostudio  opencode.ai  copilot.tencent.com
(JoyCode)      .huawei.com   /zen/v1      /v2
               (DevEco)      (OpenCode)   (WorkBuddy)
```

### 核心设计

| 机制 | 说明 |
|------|------|
| **代理与 GUI 共存** | HTTP 代理独立在 `8787`，关窗口后台继续服务 |
| **四上游统一接口** | `Call / EnsureCreds / VerifyCreds / CredStatus`，路由层按配置链遍历 |
| **auto 模式** | 优先级链依次尝试，凭据失效自动跳过，任意成功即返回 |
| **手动模式** | 指定模型 + 该模型降级链 + 全局兜底 |
| **凭据动态读取** | 启动时从本地存储读登录态，失效自动恢复，重登免重启 |
| **模型实时获取** | 上游 models 接口拉取 + 本地元数据合并 |
| **伪流式** | 上游统一非流式，下游要流式时拆成 SSE 事件 |

---

## 🛠 核心特性

### 🎛 模式切换
- **auto**：如 `DevEco/glm-5.1 → JoyCode/Kimi → OpenCode/deepseek`，配好优先级，自动选
- **手动**：指定模型，失败按降级链走，最后全局兜底

### 📊 使用统计
- 每次请求记录：输入/输出 token、命中缓存 token、费用
- 按天/周/月/自定义范围，按 **agent** 或 **模型** 维度统计
- 缓存命中率：请求命中 + token 命中双指标
- 趋势图：今天按小时、周/月按天

### 💰 费率管理
- 内置 188 条模型费率（每百万 token 成本，美元）
- 设置页可视化增删改查，改完即时生效

### 🔄 自动升级
- 启动 3s 后台检查 GitHub Releases
- 一键下载 + **原子替换**二进制（minio/selfupdate），失败不损坏原程序
- 可配置更新源（GitHub / 自定义 URL）

### 🖥 桌面体验
- 关闭/最小化 → 后台运行（托盘常驻）
- 托盘右键「退出」才真正退出
- 凭据状态可视化，未安装自动引导
- 首次启动引导弹窗：安装命令 + 复制 + 下载链接

---

## 🔧 构建

依赖：Go 1.25+、Node.js、wails3 CLI。

```bash
# 安装 wails3 CLI
go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-alpha.74

# 构建（生成绑定 + 前端 + 桌面二进制）
wails3 build
# 产物：bin/switch-dev

# 开发模式（热重载）
wails3 dev

# 无 GUI 服务模式（纯 HTTP 代理，服务器部署）
wails3 task build:server
```

改了 Go service 签名后需重新生成前端绑定：
```bash
wails3 generate bindings -ts -d frontend/bindings ./...
```

---

## 🚢 发布

### 本地构建（macOS 桌面版）
```bash
make build           # wails3 build → bin/switch-dev
make build-server    # 无 GUI 纯 HTTP 代理版
make version         # 查看当前版本
```

### 发布新版本（推码 + 打 tag + 发 Release）
```bash
# 1. 更新 build/config.yml 里的 version 到新版本号
# 2. 一键发布
make deploy v=0.1.0

# 或分步：
make push          # 推代码到 GitHub
make tag v=0.1.0   # 打 tag
make release v=0.1.0  # 创建 GitHub Release
```

发布后，**GitHub Actions 会自动**在 macOS/Windows/Linux 三个平台构建裸二进制，并上传到 Release 作为资产（`switch-dev-darwin-amd64` / `-arm64` / `-windows-amd64.exe` / `-linux-amd64`）。用户端的自动升级即从这些资产下载替换。

> 依赖：`gh` CLI（需 `gh auth login`）、GitHub Actions 首次运行需开启 Actions 权限。

---

## 📁 项目结构
switch-dev/
├── main.go                # 入口：Wails app + 窗口 + 托盘 + 启动代理 + 更新检查
├── proxy/                 # 代理核心
│   ├── models.go          # 模型白名单 + 别名 + auto 策略
│   ├── types.go           # Anthropic/OpenAI 请求响应结构
│   ├── converter.go       # Anthropic ↔ OpenAI 格式转换
│   ├── sse.go             # 伪流式 SSE 拆分
│   ├── router.go          # 上游分发 + 配置链遍历
│   ├── handlers.go        # HTTP 处理器 + 用量/费用/缓存统计
│   └── server.go          # HTTP 服务 + /health /v1/models
├── upstream/              # 四上游适配器（统一 Upstream 接口）
│   ├── joycode.go         # Color 网关签名 + 业务字段注入 + 401 重试 + 模型拉取
│   ├── deveco.go          # Bearer + Chat-Id + JWT 刷新 + 模型拉取
│   ├── opencode.go        # 标准 OpenAI 兼容 + 模型拉取
│   ├── workbuddy.go       # 强制 stream:true + SSE 聚合 + 401 重试
│   └── sse_aggregate.go   # OpenAI SSE chunk 聚合成完整响应（WorkBuddy 用）
├── creds/                 # 凭据管理
│   ├── joycode.go         # vscdb 读取 + 预检
│   ├── deveco.go          # AES-256-GCM 三层解密 + JWT 刷新
│   ├── opencode.go        # 明文 auth.json 读取
│   ├── workbuddy.go       # 明文 info 读取 + refreshToken 续期
│   └── agents.go          # agent 注册表（安装/登录引导元数据）
├── config/                # 运行配置（模式/链/端口/更新）
├── pricing/               # 费率管理（内置 188 条硬编码 + 可编辑）
│   └── rates_default.go   # 内置默认费率
├── service/               # Wails 服务层（暴露给前端）
│   ├── proxy_service.go   # 代理启停/状态/仪表盘
│   ├── creds_service.go   # 凭据状态/刷新/引导
│   ├── config_service.go  # 配置读写 + 模型列表
│   ├── pricing_service.go # 费率增删改查
│   ├── log_service.go     # 请求日志 + 统计 + 趋势
│   └── updater_service.go # 自动升级
├── updater/               # 自动升级（GitHub Releases + selfupdate）
├── version/               # 版本号读取（build/config.yml）
├── frontend/              # React + TS + Tailwind v4
└── build/                 # Wails 打包配置 + 图标
```

---

## 📂 数据目录

| 路径 | 内容 |
|------|------|
| `~/.config/switch-free/config.json` | 运行配置（模式/链/端口/更新）|
| `~/.config/switch-free/pricing.json` | 费率库（首次从内置硬编码还原）|
| `~/.config/switch-free/logs/YYYY-MM-DD.jsonl` | 按天日志（30 天自动清理）|

---

## ❓ FAQ

**Q：真的免费吗？会不会扣我的钱？**
Switch Dev 只调用你已登录工具自带的额度，不经过任何付费 API。费用统计显示的是"按费率表估算"，实际消耗看各工具后台。

**Q：一个都不装能跑吗？**
能启动，但没有上游可用。装任意一个并登录后自动恢复（无需重启）。

**Q：凭据安全吗？**
凭据只存在你本地各工具的配置里，Switch Dev 动态读取、不硬编码、不上传。别把 `8787` 暴露公网。

**Q：支持流式吗？**
支持。上游统一非流式，代理伪流式拆成 SSE，Claude Code 的流式体验正常。

**Q：能同时用 Claude Code 和 Codex 吗？**
能。Claude Code 走 Anthropic 入口，Codex 走 OpenAI 入口，共用同一个 8787 端口，互不干扰。

**Q：上游新增模型怎么办？**
设置页「刷新模型」实时拉取；费率可在设置页增删改查。

**Q：如何更新到新版本？**
设置页「更新」tab 一键检查 + 升级；或启动时自动检测弹窗。

---

## 🔒 安全说明

- 凭据绑定个人账号，泄露可被冒用，**不要外传**
- 代理仅本地使用（`127.0.0.1:8787`），**不要暴露公网**
- 日志记录请求/响应体（各截断 4KB），注意隐私
- 不用时删除 `~/.config/switch-free/` 和各工具凭据文件

---

## ⚠️ 重要免责声明

**本项目仅用于学习、研究、技术交流。** 它复用的是第三方 AI 编程工具（JoyCode / DevEco Code / OpenCode / WorkBuddy 等）自身提供的登录态与额度，**不归属本项目所有**。使用前请务必阅读下方完整免责条款。

- 🚫 **严禁商用**：不得将本项目或其产物用于任何商业用途、生产环境、盈利性服务，不得借助本项目绕过任何工具的付费机制或服务条款
- 📚 **仅限学习**：用于理解大型语言模型调用机制、本地代理原理、凭据与网关交互等技术研究
- ⚖️ **责任自负**：使用本项目产生的任何后果（包括但不限于账号异常、额度消耗、服务条款违规、法律责任）由使用者自行承担
- 🛑 **尊重原工具**：请遵守各 AI 编程工具的用户协议与使用规范；若原工具收费或限制，应停止使用本项目
- 🔧 **及时清理**：不用时请删除本项目及本地凭据数据

### 完整免责条款

### 1. 非商业用途

本项目**仅用于学习、研究、技术交流**，禁止任何形式的商业使用，包括但不限于：

- 对外提供付费/免费的代理服务（SaaS、共享中转站、充值平台等）
- 接入生产环境、企业内部正式系统、商业产品
- 以本项目为基础进行盈利、众筹、售卖、打包分发获利

### 2. 第三方资源归属

本项目复用的模型能力与凭据**属于第三方 AI 编程工具**（JoyCode / DevEco Code / OpenCode / WorkBuddy 等）及其背后的云服务商：

- 本项目**不拥有、不售卖、不转授权**任何 token、额度或模型资源
- 模型可用性、额度、价格由各原工具随时调整，本项目无法保证
- 各原工具可能随时变更接口、签名、登录机制，导致本项目失效——这**不属于本项目缺陷**，请勿向本项目投诉

### 3. 使用者责任

- 使用本项目即表示你同意自行承担全部责任，包括账号、额度、法律等方面
- 违反原工具用户协议、服务条款造成的后果由使用者自行承担
- 因使用本项目产生的直接或间接损失，作者不承担任何责任

### 4. 尊重原工具

- 请遵守各 AI 编程工具的用户协议与使用规范
- 若原工具对非官方调用有收费或限制，应立即停止使用本项目
- 请理性使用，避免对原工具服务造成过度负载

### 5. 及时清理

- 不用时请删除本项目源码、二进制、本地凭据与日志数据
- 若本项目被要求停止，请立即配合

---

## 📄 License

**MIT License**（代码部分）

> 源码以 MIT 协议开放，供学习参考。
> **但请注意**：本项目调用第三方工具的登录态与额度，该部分资源不随 MIT 协议授权；
> 使用本项目须遵守上方免责条款及第三方工具的服务条款，**禁止商用**。
