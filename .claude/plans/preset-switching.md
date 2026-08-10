# 运行模式多方案（Preset）支持

## 目标

在「运行模式」页支持保存多套降级链配置为命名方案，并在方案间快速切换，切换立即生效。方案下拉 + 保存按钮放在「刷新模型」左边（同一行右上角区域）。

## 语义确认（已与用户确认）

**方案 = 快照 / 还原点**，不是活的工作区：

- 「保存方案」冻结当前运行模式配置为一份命名快照
- 「切换方案」把快照覆盖到当前配置并立即生效
- 切换后继续编辑降级链，**只改当前配置，不回写方案**
- 想更新方案需再点一次「保存方案」并选同名覆盖
- 支持**删除**和**重命名**已有方案

## 快照包含的字段

只含运行模式四字段，与现有自动保存的范围一致：

`mode` / `autoChain` / `manualFallbacks` / `globalFallback`

**不含** `port` / `apiKey` / `update` —— 这些是环境配置，不该随方案切换而变（尤其 apiKey 变了会让客户端 401）。

---

## 一、后端：数据结构

### `config/config.go`

新增 Preset 结构（放在 `AgentModels` 之后）：

```go
// Preset 运行模式方案快照（只含降级链相关字段，不含 port/apiKey/update）
type Preset struct {
    Name            string                      `json:"name"`
    Mode            string                      `json:"mode"`
    AutoChain       []AgentModels               `json:"autoChain"`
    ManualFallbacks map[string][]proxy.ModelRef `json:"manualFallbacks"`
    GlobalFallback  proxy.ModelRef              `json:"globalFallback"`
}
```

`Config` 结构新增两个字段：

```go
Presets      []Preset `json:"presets"`      // 已保存的方案列表
ActivePreset string   `json:"activePreset"` // 当前激活方案名（仅作 UI 高亮提示，空=未选/已偏离）
```

**关键点**：`ActivePreset` 只是 UI 提示，不参与 `Resolve`。因为快照语义下，切换后编辑会让当前配置偏离方案，此时应清空该字段（下拉显示「自定义」）。

### 三处必须同步改动（否则字段会丢）

1. **`Defaults()`** — 加 `Presets: []Preset{}`, `ActivePreset: ""`
2. **`Clone()`** — 必须深拷贝 Presets（现有 Clone 手写逐字段，漏了就丢数据）：
   ```go
   cp.Presets = make([]Preset, len(c.Presets))
   for i, p := range c.Presets {
       cp.Presets[i] = Preset{
           Name: p.Name, Mode: p.Mode,
           AutoChain: deepCopyChain(p.AutoChain),
           ManualFallbacks: deepCopyFallbacks(p.ManualFallbacks),
           GlobalFallback: p.GlobalFallback,
       }
   }
   ```
   顺带抽两个 helper，因为 AutoChain/ManualFallbacks 的深拷贝逻辑现在要用三次（Config 自身 + Preset），避免复制粘贴。
3. **`Update()`** — 加 `c.Presets = newCfg.Presets` 和 `c.ActivePreset = newCfg.ActivePreset`

### `Validate()` 新增校验

```go
// 方案：名字非空且不重名，mode 合法
seen := map[string]bool{}
for _, p := range c.Presets {
    if strings.TrimSpace(p.Name) == "" {
        return fmt.Errorf("方案名不能为空")
    }
    if seen[p.Name] {
        return fmt.Errorf("方案名重复: %s", p.Name)
    }
    seen[p.Name] = true
    if p.Mode != "auto" && p.Mode != "manual" {
        return fmt.Errorf("方案 %s 的模式无效: %s", p.Name, p.Mode)
    }
    // upstream 校验复用现有循环逻辑
}
```

**注意**：`ActivePreset` 不校验是否存在于 Presets 中 —— 允许它为空或指向已删除的方案（UI 降级显示「自定义」即可），避免因为这个把整份配置判为非法触发重置。现有 `Load()` 在 Validate 失败时会**整份重置为默认**（config.go:126-132），校验过严会让用户丢配置。

---

## 二、后端：Manager 方法

### `config/manager.go`

四个新方法，都复用现有 `SaveConfig` 走校验+写盘+热加载：

```go
// SavePreset 保存/覆盖同名方案（快照当前运行模式配置）
func (m *Manager) SavePreset(name string) error

// ApplyPreset 应用方案到当前配置（覆盖生效）
func (m *Manager) ApplyPreset(name string) error

// DeletePreset 删除方案
func (m *Manager) DeletePreset(name string) error

// RenamePreset 重命名方案
func (m *Manager) RenamePreset(oldName, newName string) error
```

实现要点：
- `SavePreset`：`Get()` 拿克隆 → 找同名则覆盖、否则 append → 设 `ActivePreset = name` → `SaveConfig`
- `ApplyPreset`：找到方案 → 把四字段覆盖到 cfg → 设 `ActivePreset = name` → `SaveConfig`；找不到返回错误
- `DeletePreset`：过滤掉 → 若删的是当前激活的则 `ActivePreset = ""` → `SaveConfig`
- `RenamePreset`：新名去空格、查重名冲突 → 改名 → 同步 `ActivePreset` → `SaveConfig`

**并发注意**：这四个方法都不能自己持 `m.mu` 再调 `SaveConfig`（后者会 `m.mu.Lock()`）—— 会死锁。用 `m.Get()` 取克隆、改完调 `SaveConfig`，与现有 `ResetConfig`(manager.go:83) 完全同一套写法。

---

## 三、后端：Service 暴露

### `service/config_service.go`

四个透传方法，都复用现有 `emitConfigChange()` 推事件：

```go
func (s *ConfigService) SavePreset(name string) error
func (s *ConfigService) ApplyPreset(name string) error
func (s *ConfigService) DeletePreset(name string) error
func (s *ConfigService) RenamePreset(oldName, newName string) error
```

每个都是 `if err := s.mgr.Xxx(...); err != nil { return err }` + `s.emitConfigChange()` + `return nil`。

**不需要**改 `main.go` —— `ConfigService` 已注册（main.go:103），加方法即可。这比 pricing 那种新建包的路径省一大截。

**端口无关**：方案不含 port，所以不需要 `restartProxyOnPort`。

---

## 四、重新生成绑定

```bash
wails3 generate bindings -ts -d frontend/bindings ./...
```

会更新 `frontend/bindings/switchfree/config/models.ts`（新增 `Preset` class、`Config` 加两字段）和 `configservice.ts`（新增四个方法）。生成物不手改。

---

## 五、前端：UI

### `frontend/src/components/Settings.tsx`

**5.1 布局** —「运行模式」卡片标题行改成三段（现在是 `justify-between` 两段）：

```
┌─ 运行模式 ────────── [方案 ▾] [💾 保存方案] [🔄 刷新模型] ─┐
```

即标题左对齐，右侧一组按钮 `flex items-center gap-2`，顺序：方案下拉 → 保存方案 → 刷新模型（满足「放在刷新模型左边」）。

**5.2 方案下拉** — 自定义下拉（不用原生 `<select>`，因为每项要带 ✕ 和 ✎ 图标）：

- 顶部显示当前状态：有 `activePreset` 显示其名，否则显示「自定义」
- 每项：方案名 + `mode` 徽章 + 链长度提示（如 `auto · 3 个模型`）
- 每项右侧 hover 出 ✎（重命名）和 ✕（删除）
- 点方案名 → `ApplyPreset` → 立即生效
- 空列表时显示「暂无方案，点击右侧保存」

可参考同目录 `ModelSelect.tsx` 已有的自定义下拉实现（带搜索、点外部关闭），保持交互一致。

**5.3 保存方案** — 点击弹输入框（复用已有的 modal 样式，即之前那个「未保存的更改」弹窗的骨架）：
- 预填当前 `activePreset` 名（方便更新同名方案）
- 输入已存在的名字 → 按钮变「覆盖」并提示「将覆盖已有方案」
- 空名字禁用按钮

**5.4 偏离检测** — 关键交互细节：

切换方案后用户编辑降级链，需要把 `ActivePreset` 清空让下拉显示「自定义」，否则会误以为当前就是那个方案。

实现：在现有自动保存 effect 里判断 —— 若当前四字段与 `activePreset` 对应的快照不一致，则连同 `activePreset: ""` 一起保存。用 JSON 比较（和之前删掉的 `modeSnapshot` 思路一样，但这次比的是「当前 vs 方案」而非「当前 vs 已保存」）。

**5.5 撤销复用** — 删除方案接上刚做的 `flashUndo`：删除后提示可撤销，点撤销重新 `SavePreset`。
注意撤销要恢复的是**方案本身**而非 cfg 快照，所以不能直接用 `flashUndo(text, cfgSnapshot)`，需要单独写一个走 `SavePreset` 的撤销回调。

---

## 六、验证

1. `cd frontend && npx tsc --noEmit` + `npx vite build`
2. `go build ./...`
3. `go test ./config/...`（若有测试）
4. **必须手工验证的快照语义**（自动化覆盖不到）：
   - 存方案 A → 改链 → 下拉应显示「自定义」
   - 切回 A → 应完整还原当初存的样子
   - 重命名 → 激活状态跟着走
   - 删除当前激活方案 → 下拉回到「自定义」，当前配置不变
   - 重启应用 → 方案列表还在

---

## 风险点

1. **`Clone()` 漏拷贝会静默丢方案** —— 这是最容易出错的地方，现有 Clone 是手写逐字段的，新增字段必须同步。
2. **`Validate()` 过严会触发整份配置重置** —— `Load()` 在校验失败时直接 `*c = *Defaults()`，用户所有配置都没了。所以 `ActivePreset` 刻意不做存在性校验。
3. **死锁** —— Manager 新方法若在持锁状态调 `SaveConfig` 会自锁，必须走 `Get()` + `SaveConfig` 模式。
4. 方案里存的模型可能因上游下架而失效 —— 沿用现有宽松策略（`isValidModel` 恒返回 true），切换后靠 UI 上的 ✓/✗ 凭据标记提示，不阻止切换。

## 不做的事

- 方案不含 port/apiKey/update（理由见上）
- 不做方案导入导出（现有「查看运行模式配置 JSON」+ 复制已能覆盖手工搬运需求）
- 不做方案排序拖拽（列表通常很短）
