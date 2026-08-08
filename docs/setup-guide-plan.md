# 首次使用引导（SetupGuide）方案

> 目标：新用户打开应用时，若三个 agent 工具（JoyCode / DevEco / OpenCode）都没装没登录，
> 应用能主动引导其安装 + 登录，而不是显示一堆"✗ 无效"让人懵。
> 同时为"后续加入更多 agent 工具"做可配置化铺垫。

## 背景

实测当前行为（用空 HOME 启动，模拟全新环境）：

- 代理照常启动，日志打三条 `⚠️ 凭据不可用` 警告
- `health` 返回三个 `CredValid: false`，`ok: false`
- 客户端发请求会收到 `authentication_error`（502），错误信息带安装命令
- **但 GUI 端缺引导**：仪表盘三卡片全红、凭据页只有一行小字提示、不弹窗、无安装命令、无复制按钮

问题根因：错误信息埋在请求失败的报错里，GUI 没有主动引导；且 `CredStatusInfo` 只有 `valid` 一个维度，无法区分"没装"和"装了但过期"。

## 设计原则

1. **可配置**：agent 工具元数据抽成注册表，加新工具 = 加一条配置，不改逻辑
2. **三态判定**：`Installed`（文件是否存在）× `Valid`（预检是否通过）区分"未装 / 已装未登录 / 正常"
3. **完整引导**：首次启动弹窗 + 凭据卡片增强 + 仪表盘空状态，覆盖新用户全流程
4. **官方推荐安装方式**：JoyCode 给下载页（GUI 应用），DevEco/OpenCode 给 CLI 安装命令

## 一、后端：agent 配置注册表

新增 `creds/agents.go`，定义每个 agent 工具的元数据：

```go
type AgentInfo struct {
    Name        string   // "JoyCode"
    Upstream    string   // "joycode"
    Type        string   // "gui" | "cli"  -- 决定引导方式
    Desc        string   // 一句话描述
    DownloadURL string   // 官网/下载页
    InstallCmd  string   // CLI 安装命令（gui 类型为空）
    LoginCmd    string   // 登录命令（gui 类型为"打开客户端扫码"）
    LoginURL    string   // 浏览器登录页
    ProbePaths  []string // 凭据文件探测路径（判断 Installed），支持 ~ 展开
}

var AgentRegistry = []AgentInfo{
    {
        Name:"JoyCode", Upstream:"joycode", Type:"gui",
        Desc:"京东云 AI 编程工具（VS Code 套壳），pt_key 登录态存于 state.vscdb",
        DownloadURL:"https://joycode.jd.com",
        LoginURL:"https://joycode.jd.com/portal/login",
        LoginCmd:"打开 JoyCode 客户端扫码登录",
        ProbePaths:[]string{"~/Library/Application Support/JoyCode/User/globalStorage/state.vscdb"},
    },
    {
        Name:"DevEco Code", Upstream:"deveco", Type:"cli",
        Desc:"华为 DevEco Code（基于 OpenCode），OAuth 三层加密存本地",
        DownloadURL:"https://cn.devecostudio.huawei.com",
        InstallCmd:"npm i -g @deveco/deveco-code",
        LoginCmd:"deveco auth login",
        LoginURL:"https://cn.devecostudio.huawei.com",
        ProbePaths:[]string{"~/.config/deveco/token.dek"},
    },
    {
        Name:"OpenCode Zen", Upstream:"opencode", Type:"cli",
        Desc:"OpenCode 开源 CLI 的免费模型通道，静态 apiKey 明文存本地",
        DownloadURL:"https://opencode.ai",
        InstallCmd:"brew install opencode-ai/tap/opencode",
        LoginCmd:"opencode auth login",
        LoginURL:"https://opencode.ai",
        ProbePaths:[]string{"~/.local/share/opencode/auth.json"},
    },
}
```

**扩展性**：加新 agent 工具只需往 `AgentRegistry` 追加一条 `AgentInfo`，前端自动渲染对应引导卡片。
后续若需从外部文件加载（如 `agents.json`），可在此基础上改造，当前规模写死在代码里够用。

辅助函数：

```go
// FindAgent 按 upstream 名查 agent 元数据
func FindAgent(upstream string) *AgentInfo

// IsAgentInstalled 探测凭据文件是否存在（~ 自动展开）
func IsAgentInstalled(agent *AgentInfo) bool
```

## 二、后端：CredStatusInfo 扩展

`creds/types.go` 的 `CredStatusInfo` 增加字段：

```go
type CredStatusInfo struct {
    Valid       bool   `json:"valid"`
    Installed   bool   `json:"installed"`    // ★ 新增：工具是否装了（凭据文件存在）
    UserID      string `json:"userId,omitempty"`
    ExpiresAt   string `json:"expiresAt,omitempty"`
    KeyPreview  string `json:"keyPreview,omitempty"`
    Source      string `json:"source"`
    LastCheck   string `json:"lastCheck"`
    // ★ 新增：安装/登录引导元数据（从 AgentRegistry 注入）
    AgentType   string `json:"agentType"`       // "gui" | "cli"
    InstallCmd  string `json:"installCmd,omitempty"`
    LoginCmd    string `json:"loginCmd,omitempty"`
    DownloadURL string `json:"downloadUrl,omitempty"`
    LoginURL    string `json:"loginUrl,omitempty"`
}
```

三态判定逻辑：

| Installed | Valid | 含义 | 前端展示 |
|-----------|-------|------|---------|
| false | false | 未安装 | "未安装"卡 + 安装命令 + 下载链接 |
| true  | false | 已装未登录/过期 | "未登录"卡 + 登录命令 + 登录页 |
| true  | true  | 正常 | 正常凭据卡 |

三个凭据管理器（joycode/deveco/opencode）的 `CredStatus()` 方法改造：
- 用 `IsAgentInstalled` 探测文件存在性填 `Installed`
- 从 `AgentRegistry` 取对应 agent 元数据填充 `AgentType/InstallCmd/LoginCmd/DownloadURL/LoginURL`

## 三、前端：SetupGuide 组件

### 3.1 首次启动弹窗

`App.tsx` 拉到 dashboard 后，若三上游 `installed` 全 false，自动弹出 `SetupGuide` 模态框。
模态框可关闭（关了不再自动弹，本次会话内记住）。

### 3.2 SetupGuide 内容

列出三个 agent，每个显示：
- 名称 + 类型标签（GUI/CLI）
- 安装步骤：
  - CLI 类型：`InstallCmd` 命令 + "复制"按钮
  - GUI 类型：`DownloadURL` 链接 + "打开下载页"按钮
- 登录步骤：`LoginCmd` + "复制"按钮（GUI 类型显示"打开客户端扫码"提示）
- 当前状态徽章（未安装/已装未登录/已就绪）

### 3.3 凭据卡片增强

`Credentials.tsx` 的 `CredCard` 按 `installed`/`valid` 三态渲染不同操作区：

- **未安装**：黄色提示条 + 安装命令（带复制按钮）+ 下载页链接
- **已装未登录**：黄色提示条 + 登录命令（带复制按钮）+ 登录页按钮
- **正常**：现有凭据详情

### 3.4 仪表盘空状态

`Dashboard.tsx` 检测三上游全无效时，在顶部显示"快速开始"引导卡片，
提示"检测到尚未配置任何 agent 工具，前往凭据页安装登录"，带跳转按钮。

### 3.5 复制按钮

通用 `CopyButton` 组件：点击复制文本到剪贴板，显示"已复制"反馈 1.5s。
用 Wails runtime 的剪贴板 API 或 `navigator.clipboard`。

## 四、改动文件清单

### 后端
- `creds/agents.go`（新增）：`AgentInfo` + `AgentRegistry` + `FindAgent` + `IsAgentInstalled`
- `creds/types.go`：`CredStatusInfo` 加字段
- `creds/joycode.go`：`CredStatus()` 填充新字段
- `creds/deveco.go`：`CredStatus()` 填充新字段
- `creds/opencode.go`：`CredStatus()` 填充新字段
- 重新生成前端绑定：`wails3 generate bindings -ts -d frontend/bindings ./...`

### 前端
- `frontend/src/components/SetupGuide.tsx`（新增）：引导弹窗组件
- `frontend/src/components/CopyButton.tsx`（新增）：通用复制按钮
- `frontend/src/components/Credentials.tsx`：`CredCard` 三态渲染
- `frontend/src/components/Dashboard.tsx`：空状态引导卡片
- `frontend/src/App.tsx`：首次启动自动弹 SetupGuide
- `frontend/src/hooks/useClipboard.ts`（新增，可选）：剪贴板 hook

## 五、验证方式

1. 空环境（`HOME=/tmp/empty`）启动：弹窗自动出现，三卡片全"未安装"
2. 只装 DevEco：DevEco 卡"已装未登录"，其余"未安装"
3. 装并登录 DevEco：DevEco 卡正常，弹窗不再自动出现（除非手动打开）
4. 复制按钮：点击后命令进剪贴板
5. 下载/登录链接：点击用系统浏览器打开
