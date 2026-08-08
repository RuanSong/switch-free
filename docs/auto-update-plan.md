# 自动升级方案

> 目标：对已发布的 switch-free 程序做后续增量更新，客户端能自动检查新版本并升级。
> 平台：macOS（.app 内单二进制）/ Windows（exe）/ Linux。
> 已确认：版本号从 build/config.yml 读；先裸二进制自更新，签名/公证后补。

## 一、技术选型

| 项 | 选择 | 理由 |
|----|------|------|
| 更新库 | `minio/selfupdate` | 主流，原子替换运行中二进制，跨平台（macOS/Windows/Linux）|
| 发布渠道 | GitHub Releases | 免费、有版本 API、资产直链下载 |
| 更新包粒度 | 全量包 | 当前二进制 ~11MB，全量替换简单可靠 |
| 检查时机 | 启动后 3s + 手动检查按钮 | 用户确认后更新 |
| 服务器地址 | 可配置（存 config.json） | 灵活，换 repo 不改代码 |
| 版本号来源 | 从 build/config.yml 的 info.version 读取 | 统一配置源 |
| macOS 策略 | 先裸二进制自更新，签名/公证后补 | 快速落地，后增强 |

## 二、更新流程

```
启动 → 3s 后后台检查 GitHub Releases
        │
        ├─ 无新版本 → 静默，不打扰
        └─ 有新版本 → 弹窗提示（当前版 → 新版本 + 更新说明）
                        │
                        用户点「立即更新」
                        │
                        下载新版本资产（进度条）
                        │
                        minio/selfupdate 原子替换二进制
                        │
                        提示「更新完成，需重启生效」
```

手动路径：设置页「关于/更新」区域点「检查更新」→ 同上。

## 三、配置

`config.json` 新增字段：

```jsonc
{
  // ... 现有字段
  "update": {
    "enabled": true,
    "provider": "github",
    "github": {
      "owner": "RuanSong",       // GitHub 用户名
      "repo": "switch-free",     // 仓库名
      "token": ""                 // 可选：私有仓库的 PAT（公开仓库可留空）
    },
    "updateUrl": "",              // 替代 provider 的自定义检查地址（可选，优先于 github）
    "channel": "stable"           // stable | beta（可选，默认 stable）
  }
}
```

- 优先用 `updateUrl`（自定义 JSON 检查地址），为空则用 `github.owner/repo`
- 可配置化 → 换发布源不用发新版

## 四、GitHub 发布约定

发布一个 Release（用 `gh release create` 或网页），tag 如 `v0.1.0`：

```
资产命名（按平台/架构）：
  switch-free-darwin-amd64    # macOS Intel
  switch-free-darwin-arm64    # macOS Apple Silicon
  switch-free-windows-amd64.exe
  switch-free-linux-amd64
```

`selfupdate` 替换的是**单个二进制**，所以发布物是裸二进制（非 .app 包）。macOS 场景：把新二进制塞进现有 .app 的 Contents/MacOS 替换旧的。

> ⚠️ macOS 已签名/公证的 .app 直接替换内部二进制会导致签名失效。两种处理：
> 1. 应用内替换二进制后重新签名（需开发者证书，复杂）
> 2. 更新的是"裸二进制启动模式"（当前开发时就是直接跑 bin/switch-free）
> 发布正式版时建议用**签名后打包 .app + 整包替换**，或对二进制自签。

## 五、版本来源

- 当前版本号：从 `build/config.yml` 的 `info.version`（`wails3 build` 注入）或代码常量
- 新版本检测：GitHub Releases API `GET /repos/{owner}/{repo}/releases/latest`，比对 tag/version

## 六、后端实现（新增 updater 包）

```
updater/
├── updater.go      // 检查更新 + 下载 + 替换主流程
├── github.go       // GitHub Releases API 封装（latest release + 资产 URL）
└── types.go        // 版本/更新信息结构
```

### updater.Updater
```go
type Updater struct {
    CurrentVersion string        // 当前版本
    config         *config.Config // 更新配置
    httpClient     *http.Client
}

// CheckUpdate 检查是否有新版本（不下载）
// 返回 nil 表示无更新；有则返回新版本信息
func (u *Updater) CheckUpdate(ctx) (*UpdateInfo, error)

type UpdateInfo struct {
    Version   string // 新版本号
    Notes     string // 更新说明
    AssetURL  string // 下载地址
    AssetSize int64  // 大小
}

// ApplyUpdate 下载并替换二进制（原子）
func (u *Updater) ApplyUpdate(ctx, info *UpdateInfo) error
```

### GitHub API
```go
GET https://api.github.com/repos/{owner}/{repo}/releases/latest
→ {
    "tag_name": "v0.1.0",
    "name": "...",
    "body": "更新说明",
    "assets": [{ "name": "switch-free-darwin-amd64", "browser_download_url": "...", "size": 12345 }]
}
```

按当前 GOOS/GOARCH 匹配资产名下载。

## 七、前端

设置页新增「更新」tab（或并入通用）：
- 当前版本号显示
- 「检查更新」按钮 → 调用 `UpdaterService.CheckUpdate()`
- 有新版本：显示新版本号 + 更新说明 + 「立即更新」按钮 + 下载进度
- 更新完成：提示重启
- 启动时自动检查（后端 3s 后静默调 CheckUpdate，有新版本发事件给前端弹窗）

### 服务
```go
// UpdaterService（暴露给前端）
type UpdaterService struct{}

func (s *UpdaterService) CheckUpdate() (*UpdateInfo, error)   // 检查
func (s *UpdaterService) ApplyUpdate() error                  // 下载+替换（可能阻塞，前端显示进度）
func (s *UpdaterService) GetCurrentVersion() string           // 当前版本
```

### 事件
- `update:available` → 前端弹更新提示
- `update:progress` → 下载进度
- `update:done` → 更新完成，提示重启

## 八、改动文件清单

### 后端
- `updater/updater.go` - 主流程
- `updater/github.go` - GitHub API
- `updater/types.go` - 结构
- `service/updater_service.go` - 暴露给前端
- `config/config.go` - 加 update 配置 + Validate
- `main.go` - 初始化 Updater + 启动后台检查
- 依赖：`github.com/minio/selfupdate`

### 前端
- `frontend/src/components/UpdatePanel.tsx` - 设置页更新区
- `frontend/src/components/Settings.tsx` - 加「更新」tab
- `frontend/src/App.tsx` - 订阅 update:available 弹窗

### 发布
- `scripts/release.sh` - 构建各平台二进制 + `gh release create`
- 更新 README 发布说明

## 九、验证

1. 本机改 version 到旧版，发布一个新版到 GitHub，启动后 3s 弹更新提示
2. 点「立即更新」→ 下载 → 替换二进制 → 重启验证版本号
3. 无新版本时不打扰
4. 更新地址可配置：改 updateUrl 指向自定义服务器也生效

## 十、风险与限制

| 风险 | 应对 |
|------|------|
| macOS 签名/公证失效 | 正式发布用签名二进制 + 整包替换，或接受裸二进制模式 |
| 下载中断 | selfupdate 先下载到临时文件再原子替换，失败不破坏原二进制 |
| 新版本损坏 | selfupdate 替换前校验 SHA256（GitHub 资产可配 checksum）|
| 更新期间应用被杀 | selfupdate 原子性保证要么旧要么新，不会半更新 |
| Windows 文件占用 | 运行中 exe 被锁，需要先退出再替换或延迟替换（selfupdate 处理）|
