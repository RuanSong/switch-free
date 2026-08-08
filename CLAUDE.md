# Switch Free (Wails v3 版)

## 项目性质
把 JoyCode/DevEco/OpenCode 三套 AI 编程工具的模型能力，通过本地代理暴露为标准 Anthropic/OpenAI 接口，供 Claude Code/cc-switch 复用。带 Wails v3 桌面 GUI。

## 关键架构
- 代理 HTTP 服务独立运行在 `127.0.0.1:8787`（`proxy.Server`），与 Wails 窗口共存
- 三上游统一实现 `upstream.Upstream` 接口（Call/VerifyCreds/EnsureCreds/InvalidateCreds/CredStatus）
- 凭据动态读取，不硬编码：JoyCode 从 state.vscdb（用 sqlite3 CLI），DevEco 三层 AES-256-GCM 解密，OpenCode 明文 auth.json
- auto 模式：DevEco GLM-5.1 主力，失败降级 JoyCode JoyAI-Code-1.5
- 上游统一非流式；下游要流式时代理把完整响应拆成 SSE（伪流式）
- service.Core 实现 proxy.EventLogger，记录日志 + 通过 `application.Get().Event.Emit` 推送事件到前端

## 构建
```bash
wails3 build          # 产物 bin/switch-free
wails3 dev            # 开发热重载
wails3 task build:server  # 无 GUI 服务模式
```
改了 Go service 签名后需重新生成绑定：`wails3 generate bindings -ts -d frontend/bindings ./...`

## 验证过的能力
- /health 返回三上游凭据状态
- /v1/models 返回 18 个模型
- /v1/messages 非流式（auto->DevEco）✓
- /v1/messages 流式（JoyAI-Code-1.5->JoyCode）✓
- /v1/chat/completions ✓
- 凭据三上游全部有效（实测 2026-08-08）

## 与旧版关系
`../switch-free/` 是原 Node.js 单文件版（joycode-color-proxy.js）。本目录是 Go+GUI 重构，端口/路由/格式完全兼容，可 drop-in 替换。逆向过程文档在旧版目录。
