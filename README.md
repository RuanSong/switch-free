# ⚡ Switch Dev

<div align="center">

**不生产 Token，只做 Token 的搬运工。**

一个本地代理 + 桌面管理面板，复用你已安装的 AI 编程工具登录态，
把各家免费模型额度统一搬运成标准 Anthropic / OpenAI 接口，喂给 Claude Code、Codex、cc-switch。

`本地运行` · `复用登录态` · `统一端口` · `多上游自动降级` · `用量可观测` · `MIT 开源`

</div>

---

## 🎯 它解决什么问题

你机器上装着的 JoyCode、DevEco Code、OpenCode、WorkBuddy，后台都自带模型额度——
京东云的 JoyAI / Kimi / GLM、华为的 GLM-5.1、OpenCode Zen 的 free 模型、腾讯 CodeBuddy 的 GLM / MiniMax / Kimi / DeepSeek / 混元。

但这些额度是**登录态**：请求要动态签名、要注入业务字段、Token 会过期要刷新，
没法直接填进 Claude Code。

Switch Dev 在本地把这些脏活累活全部包掉：

> **装好并登录一个工具 → 启动 Switch Dev → 客户端指向 `127.0.0.1:8787` → 开跑。**

它本身不提供任何模型、不签发任何 Token、不经过任何中转服务器——
**你的请求从本机出发，用你自己的登录态，直达各家官方接口。**

---

## ✨ 特色一览

| | 为什么特别 |
|---|---|
| 🔐 **凭据零硬编码** | 启动时从各工具本地存储动态读取登录态，失效自动恢复，重登免重启 |
| 🧩 **四上游统一抽象** | JoyCode / DevEco / OpenCode / WorkBuddy 实现同一套 `Upstream` 接口，路由层无差别遍历 |
| 🪜 **三级降级链** | auto 优先级链、manual 按模型配链、UA 路由按客户端 User-Agent 分流；每条链末尾都有全局兜底 |
| 🌊 **流式全适配** | 上游统一非流式时，代理在本地把完整响应拆成 SSE；WorkBuddy 反向强制流式，内部聚合后再下发 |
| 🛰️ **自带供应商体系** | 可接入任意 OpenAI / Anthropic 兼容 API，凭据本地 argon2id 加密，支持 `.sds` 加密文件分享 |
| 🖥️ **代理即面板** | HTTP 服务与 Wails v3 桌面 GUI 共存，关窗口后台常驻，也可无界面纯服务运行 |
| 📊 **本地可观测** | 每次请求的 token、缓存命中、费用、速率全部记录，按 agent / 模型 / 时间维度统计 |

---

## 🚀 快速开始

### 1. 装一个工具并登录

### 1. 装一个工具并登录

Switch Dev 只搬运你已有的登录态，**不代注册、不代登录**。请先在对应工具里完成登录，并确认该工具本身能正常对话，再启动 Switch Dev。

| 工具 | 安装 | 登录 / 前置条件 |
|------|------|----------------|
| **JoyCode** | [官网下载](https://joycode.jd.com) | 客户端扫码登录，确认 JoyCode 内能正常发起对话 |
| **DevEco Code** | `npm i -g @deveco/deveco-code` | ⚠️ 需先[注册华为开发者账号并完成实名认证](https://developer.huawei.com)，再 `deveco auth login` 登录；确认 DevEco Studio / DevEco Code 内可正常调用模型 |
| **OpenCode** | `brew install opencode-ai/tap/opencode` | `opencode auth login` 登录，确认 opencode 内能正常调用 free 模型 |
| **WorkBuddy** | [官网下载](https://workbuddy.app) | 客户端登录，确认 WorkBuddy 内能正常发起对话 |

> 装一个就能用；四个全装就是「全家桶」。首次启动会自动探测安装状态并引导登录。
> 若某上游单独使用时就无法调用（未登录、未实名、额度耗尽），Switch Dev 里同样会失败——请先在原工具里排查。

### 2. 启动

```bash
./bin/switch-dev        # macOS / Linux；Windows 双击 .exe
```

代理服务 `127.0.0.1:8787` 立即可用，托盘图标常驻（关窗口不退出）。

### 3. 接入客户端

**Claude Code**（`~/.claude/settings.json`）：
```json
{
  "env": {
    "ANTHROPIC_BASE_URL": "http://127.0.0.1:8787",
    "ANTHROPIC_AUTH_TOKEN": "sk-switchdev",
    "ANTHROPIC_MODEL": "auto"
  }
}
```

**Codex**（`~/.codex/config.toml`）：
```toml
model = "auto"
model_provider = "switchdev"
[model_providers.switchdev]
base_url = "http://127.0.0.1:8787/v1"
wire_api = "chat"
```

**cc-switch**：baseURL `http://127.0.0.1:8787`，apiKey 任意，模型 `auto`。

> `auto` 按你配置的优先级链自动选模型；也可指定具体模型（如 `glm-5.1`、`wb/hy3`）。

---

## 🧠 核心机制

### 四上游适配器

| 上游 | 凭据来源 | 特殊处理 |
|------|---------|---------|
| **JoyCode** | `state.vscdb`（纯 Go `modernc.org/sqlite` 读取，无需外部 sqlite3） | Color 网关签名 + 业务字段注入 + 401 重试 |
| **DevEco** | 三层 AES-256-GCM 解密 | Bearer + Chat-Id + JWT 自动刷新 |
| **OpenCode** | 明文 `auth.json` | 标准 OpenAI 兼容 |
| **WorkBuddy** | 明文 info + refreshToken 续期 | 上游强制 `stream:true`，代理聚合 SSE 后再下发；模型用 `wb/` 前缀隔离重名 |

路径解析跨平台且单一真相（macOS `~/Library/Application Support`、Linux `~/.config`、Windows `%APPDATA%`），
并支持 `JOYCODE_VSCDB` / `WORKBUDDY_INFO_PATH` / `OPENCODE_AUTH_PATH` 等环境变量覆盖。

### 三级路由 + 降级

- **auto**：按优先级链 `[]AgentModels` 依次尝试，凭据失效自动跳过，任意成功即返回，末尾追加全局兜底（自动去重）
- **manual**：为每个模型单独配降级链，未命中走全局兜底
- **ua**：按请求 `User-Agent`（Claude Code / Codex）分流到不同模型，可独立配置兜底
- **方案（Preset）**：把整套路由配置存为命名快照，一键切换；快照是冻结的，切换后继续编辑不回写，需同名保存才更新

### 流式处理

- 上游统一走非流式调用，下游要流式时，代理在本地把完整 JSON 拆成标准 SSE 事件（伪流式）
- **WorkBuddy 例外**：非流式返回 `code:11101`，上游强制 `stream:true`，`aggregateOpenAISSE` 把 chunk 聚合成完整响应后，再复用非流式处理逻辑
- 完整支持 Anthropic `/v1/messages`、OpenAI `/v1/chat/completions`、`/v1/responses`，含 SSE 错误透传

### 供应商生态

除了四家内置上游，还能接入**任意 OpenAI / Anthropic 兼容 API**：

- 凭据本地落盘，argon2id 派生 KEK 包裹 DEK，API Key 以 AES-GCM 密文存储
- 支持**主密码 + 恢复码**：主密码解锁查看，恢复码用于重置；自动加密模式下随机密码写入系统钥匙串无感解锁
- **`.sds` 加密分享**：勾选供应商导出为加密文件，可设自定义密码或随机 6 位码，对方导入即解密；支持覆盖 / 跳过 / 重命名解决 id 冲突
- 目录内置免费 / 注册送 Token 的供应商清单，一键拉取模型并批量测速

### 桌面体验

- 代理服务（`127.0.0.1:8787`）与 GUI 同进程共存，托盘常驻，关闭 / 最小化即后台运行
- 仪表盘聚合：今日 token / 费用 / 输出速率（加权 tok/s）、降级链状态、凭据健康度
- 无界面模式 `wails3 task build:server` 可纯 HTTP 部署
- 接入 apiKey 默认开启并随机生成，可一键开关（关闭后网关不鉴权）

---

## 🛠 构建

依赖：Go 1.25+、Node.js、wails3 CLI。

```bash
# 安装 wails3 CLI
go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-alpha.74

wails3 build              # 桌面二进制 → bin/switch-dev
wails3 dev                # 开发热重载
wails3 task build:server  # 无 GUI 纯代理服务

# 改了 Go service 签名后重新生成前端绑定
wails3 generate bindings -ts -d frontend/bindings ./...

# 全平台裸二进制（自动更新用）
make build-binaries V=x.y.z
```

发布流程见 [CLAUDE.md](./CLAUDE.md) 与 `make help`，一键 `make deploy V=x.y.z`
完成构建 → 推码 → 打 tag → GitHub Release → 上传产物，支持 macOS Universal DMG 与 Windows NSIS。

---

## 📊 统计与费率

- 每次请求记录输入 / 输出 token、缓存命中 token、费用、耗时、状态
- 按天 / 周 / 月 / 自定义范围，按 agent 或模型维度聚合
- 缓存命中率双指标（请求命中 + token 命中），今日按小时、周 / 月按天趋势图
- 内置费率库，设置页可视化增删改查，改完即时生效
- 内置测速：对供应商模型批量基准测评，按 TPS 排名

---

## 🔄 自动更新

- 启动 3s 后首次检查，之后每 6 小时检查 GitHub Releases
- 语义化分级：major / minor 变化为强制更新，patch 为可选
- `github.com/minio/selfupdate` **原子替换**运行中二进制，下载失败不损坏原程序
- 资产名按平台精确匹配（`switch-dev-darwin-arm64` / `-amd64` / `-windows-amd64.exe`）

---

## 🏗 架构

```
Claude Code / Codex / cc-switch
   │  Anthropic /v1/messages   或   OpenAI /v1/chat/completions
   ▼
┌──────────── Switch Dev（Wails v3 · Go · React）────────────┐
│  前端面板：仪表盘 / 供应商 / 凭据 / 模型 / 统计 / 日志 / 设置  │
│       ↕ Wails 绑定 + 事件推送                                │
│  Go 后端                                                     │
│   ├ 代理核心：协议转换 / 签名 / SSE / auto·manual·UA 降级    │
│   ├ 上游适配器：JoyCode / DevEco / OpenCode / WorkBuddy      │
│   ├ 供应商：自定义接入 / argon2id 加密 / .sds 分享           │
│   └ 凭据：vscdb / AES 三层解密 / JWT 刷新 / 钥匙串           │
│                                                              │
│  独立 HTTP 服务 :8787（与 GUI 共存，可无界面运行）           │
└──────────────────────────────────────────────────────────────┘
   │           │           │           │
   ▼           ▼           ▼           ▼
京东云        华为云       OpenCode     腾讯云
(JoyCode)     (DevEco)     Zen          (WorkBuddy)
```

---

## 📂 项目结构

```
switch-dev/
├── main.go                # 入口：Wails app + 窗口 + 托盘 + 代理 + 更新
├── proxy/                 # 代理核心（协议转换 / SSE / 路由 / 鉴权）
├── upstream/              # 四上游适配器（统一 Upstream 接口）
├── providerapi/           # 自定义供应商 + argon2id 凭据库 + .sds 分享
├── creds/                 # 各工具登录态读取与解密
├── config/                # 运行配置（模式 / 降级链 / 方案 / 端口 / 鉴权）
├── pricing/               # 费率库（内置默认 + 可编辑）
├── service/               # Wails 服务层（暴露给前端）
├── updater/               # GitHub Releases + 原子自更新
├── paths/                 # 跨平台路径解析（单一真相）
├── version/               # 版本号（build/config.yml 同步）
├── frontend/              # React + TypeScript + Tailwind v4
└── build/                 # Wails 打包配置 + 图标
```

---

## 📂 数据目录

| 路径 | 内容 |
|------|------|
| `~/.config/switch-dev/config.json` | 运行配置（模式 / 降级链 / 方案 / 端口 / 鉴权）|
| `~/.config/switch-dev/credentials.json` | 自定义供应商凭据（argon2id 加密）|
| `~/.config/switch-dev/pricing.json` | 费率库 |
| `~/.config/switch-dev/switch-dev.db` | 请求日志与统计 |
| `~/.config/switch-dev/logs/` | 按天日志 |

---

## ❓ FAQ

**Q：它自己提供模型吗？**
不。Switch Dev 不生产一个 Token，也不中转任何流量到第三方服务器。它只在你本机把你已登录工具的请求搬运到各家官方接口。

**Q：要花钱吗？**
复用的是你已登录工具自带的额度，不经过任何付费 API。费用统计是按内置费率表的**估算值**，真实消耗看各工具后台。

**Q：一个工具都不装能跑吗？**
能启动，但没有内置上游可用。装好任意一个并登录后自动恢复，无需重启；也可以在「供应商」页接入你自己的 API。

**Q：凭据安全吗？**
凭据只存在你本地各工具的配置里，动态读取、不硬编码、不上传。自定义供应商的 API Key 用 argon2id + AES-GCM 加密落盘。代理仅监听 `127.0.0.1`，**不要暴露公网**。

**Q：支持流式吗？**
支持。上游非流式时代理本地拆 SSE；WorkBuddy 反向聚合流式，客户端体验一致。

**Q：Claude Code 和 Codex 能同时用吗？**
能。分别走 Anthropic / OpenAI 入口，共用 8787 端口；UA 路由还能让两个客户端自动分流到不同模型。

**Q：上游接口变了怎么办？**
模型列表实时拉取，上游新增模型自动出现。签名 / 登录机制变更属于逆向适配问题，欢迎提 PR。

---

## 🔒 安全说明

- 代理仅监听 `127.0.0.1:8787`，**切勿暴露公网**
- 自定义供应商凭据以 argon2id 派生密钥加密存储；主密码 / 恢复码只在本机出现
- 日志记录请求 / 响应体（各截断 4KB），分享日志前注意脱敏
- 接入 apiKey 可在设置中关闭；关闭后网关不鉴权，仅适合受信任的本地环境

---

## 📄 License

[MIT License](./LICENSE)

代码以 MIT 协议开源，可自由使用、修改、商用分发。
本项目不内置、不转授权任何第三方模型或 Token；
你接入的各 AI 编程工具及其额度，其权利归属与使用约束以对应工具的服务条款为准。
