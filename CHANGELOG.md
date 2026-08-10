# Changelog

本文件记录 Switch Free 每个版本的变更。格式基于 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/)，
版本号遵循 [语义化版本](https://semver.org/lang/zh-CN/)。

发布时，`make release` 会自动从本文件提取对应版本的章节作为 GitHub Release Notes。

更新分级规则：
- **patch（0.0.x）**：普通更新，用户可忽略/稍后更新
- **minor（0.x.0）/ major**：强制更新，客户端弹窗不可忽略

维护方式：
- 日常开发：改动随手往下面 `[Unreleased]` 章节加一条（写给用户看的话，不是 commit 信息）
- 忘记记录了：`make changelog-auto` 从上个 tag 到 HEAD 的 commit 生成草稿，再人工润色
- 发布定版：`make changelog-release V=x.y.z` 自动把 `[Unreleased]` 改名为 `[x.y.z] - 日期` 并新开空 Unreleased

分类小标题固定用：`### 新增` / `### 修复` / `### 变更` / `### 移除` / `### 废弃` / `### 安全`

## [Unreleased]

### 新增
- 运行模式支持多方案：可把当前降级链配置保存为命名方案，在方案间一键切换并立即生效，支持重命名和删除
- 删除降级链条目后可点「撤销」恢复

### 变更
- 运行模式的配置项改动后自动保存生效，不再需要单独点「保存配置」
- 移除「保存运行模式」和「重置默认」按钮，界面整体上移；「刷新模型」移到运行模式卡片右上角，方案下拉与保存按钮在其左侧

## [0.0.5] - 2026-08-10

### 新增
- 添加自动更新机制，支持周期性检查更新并区分强制与可选更新


## [0.0.4] - 2025-08-10
### 修复
- 修复 Windows 上托盘右键退出卡死（WindowClosing 钩子在退出时仍 Cancel 导致窗口无法销毁）
- 修复 Windows 上 WorkBuddy 凭据识别不到（文件名实际为 `workbuddy-desktop-ai.info`，非 macOS 的 `workbuddy-desktop.info`）
- 修复 Windows 上 DevEco 凭据无效（Node.js CLI 在 Windows 也用字面 XDG 路径 `~/.config`、`~/.local/share`，之前错误映射到 `%APPDATA%`）
- 修复自动更新后临时文件被提前删除导致 "file not found" 错误（`downloadAsset` 的 `defer Remove` 时机错误）
- 修复 `GetAgents` 漏返回 WorkBuddy 的 valid 状态

### 新增
- 跨平台 agent 安装探测：CLI 工具（DevEco/OpenCode）除凭据文件外，还在 PATH 和 npm 全局目录（`%APPDATA%\npm`）查找可执行文件
- 后台周期探测 agent 安装状态，新安装工具后自动校验凭据并刷新 UI（无需重启）
- 引导弹窗增加「🔄 重新检测」按钮
- 自动更新支持强制更新分级：minor（0.x.0）变化为强制更新，patch（0.0.x）为普通更新
- 自动更新改为启动时及每 6 小时周期检测
- DMG 改为 Universal Binary（同时支持 arm64 和 x86_64 Mac）
- `make deploy` 一键构建裸二进制 + DMG + NSIS 安装包并上传 Release

### 变更
- WorkBuddy 下载链接改为 https://workbuddy.ai
- OpenCode 安装命令改为 `npm i -g opencode-ai`（跨平台，替代仅 macOS 的 brew）
- 移除仅含 macOS 路径的硬编码 ProbePaths，改用 paths 包跨平台候选路径（单一真相）

## [0.0.3] - 2025-08-08
### 验证
- 四上游凭据全部有效（JoyCode / DevEco / OpenCode / WorkBuddy）
- `/v1/messages`、`/v1/chat/completions` 流式/非流式正常
- 首页速率统计正常

## [0.0.2]
### 修复
- JoyCode sqlite 读取从外部 sqlite3 CLI 改为 `modernc.org/sqlite` 纯 Go 驱动（跨平台，无需额外依赖）

## [0.0.1]
### 新增
- 初始版本：JoyCode / DevEco / OpenCode / WorkBuddy 四上游支持
- 跨平台桌面应用（macOS / Windows），系统托盘常驻
- 自动降级链（auto 模式）+ 手动模型选择
- 自动更新（GitHub Releases）
- 模型测评、请求日志、用量统计
