package proxy

// ====== JoyCode 模型白名单 ======

type JoyCodeModel struct {
	ID              string `json:"id"`
	Label           string `json:"label"`
	Stream          bool   `json:"stream"`
	OutputMaxTokens int    `json:"outputMaxTokens"`
}

var JoyCodeModels = []JoyCodeModel{
	{ID: "JoyAI-Code-1.5", Label: "JoyAI-Code-1.5", Stream: true, OutputMaxTokens: 64000},
	{ID: "MiniMax-M3-agent", Label: "MiniMax-M3", Stream: true, OutputMaxTokens: 64000},
	{ID: "MiniMax-M2.7-agent", Label: "MiniMax-M2.7", Stream: false, OutputMaxTokens: 64000},
	{ID: "Kimi-K2.6-agent", Label: "Kimi-K2.6", Stream: true, OutputMaxTokens: 64000},
	{ID: "GLM-5.1-agent", Label: "GLM-5.1", Stream: true, OutputMaxTokens: 64000},
	{ID: "GLM-5-agent", Label: "GLM-5", Stream: true, OutputMaxTokens: 64000},
	{ID: "DeepSeek-V4-Pro-agent", Label: "DeepSeek-V4-Pro", Stream: true, OutputMaxTokens: 64000},
	{ID: "Doubao-Seed-2.0-pro-agent", Label: "Doubao-Seed-2.0-pro", Stream: false, OutputMaxTokens: 64000},
}

var JoyCodeModelIDs map[string]bool
var JoyCodeLabelToID map[string]string
var JoyCodeModelByID map[string]*JoyCodeModel

func init() {
	JoyCodeModelIDs = make(map[string]bool)
	JoyCodeLabelToID = make(map[string]string)
	JoyCodeModelByID = make(map[string]*JoyCodeModel)
	for i := range JoyCodeModels {
		m := &JoyCodeModels[i]
		JoyCodeModelIDs[m.ID] = true
		JoyCodeLabelToID[m.Label] = m.ID
		JoyCodeLabelToID[m.ID] = m.ID // id 也映射到自己
		JoyCodeModelByID[m.ID] = m
	}
}

// ====== DevEco 模型 ======

type DevEcoModel struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Upstream string `json:"upstream"`
	Context int    `json:"context"`
	Output  int    `json:"output"`
}

var DevEcoModels = []DevEcoModel{
	{ID: "glm-5.1", Label: "GLM-5.1 (DevEco)", Upstream: "GLM-5.1", Context: 170000, Output: 131072},
}

var DevEcoModelIDs map[string]bool
var DevEcoLabelToID map[string]string
var DevEcoModelByID map[string]*DevEcoModel

func init() {
	DevEcoModelIDs = make(map[string]bool)
	DevEcoLabelToID = make(map[string]string)
	DevEcoModelByID = make(map[string]*DevEcoModel)
	for i := range DevEcoModels {
		m := &DevEcoModels[i]
		DevEcoModelIDs[m.ID] = true
		DevEcoLabelToID[m.ID] = m.ID
		DevEcoLabelToID[m.Label] = m.ID
		DevEcoLabelToID[m.Upstream] = m.ID
		DevEcoModelByID[m.ID] = m
	}
}

// ====== OpenCode Zen 模型 ======

type OpenCodeModel struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Context int    `json:"context"`
	Output  int    `json:"output"`
}

var OpenCodeModels = []OpenCodeModel{
	{ID: "deepseek-v4-flash-free", Label: "DeepSeek V4 Flash Free", Context: 200000, Output: 128000},
	{ID: "mimo-v2.5-free", Label: "MiMo V2.5 Free", Context: 262144, Output: 64000},
	{ID: "ling-3.0-flash-free", Label: "Ling 3.0 Flash Free", Context: 262144, Output: 32768},
	{ID: "ling-3.0-tiny-free", Label: "Ling 3.0 Tiny Free", Context: 0, Output: 0},
	{ID: "nemotron-3-ultra-free", Label: "Nemotron 3 Ultra Free", Context: 0, Output: 0},
	{ID: "north-mini-code-free", Label: "North Mini Code Free", Context: 256000, Output: 64000},
	{ID: "laguna-s-2.1-free", Label: "Laguna S 2.1 Free", Context: 256000, Output: 32000},
	{ID: "longcat-2.0-free", Label: "LongCat 2.0 Free", Context: 0, Output: 0},
}

var OpenCodeModelIDs map[string]bool
var OpenCodeModelByID map[string]*OpenCodeModel

func init() {
	OpenCodeModelIDs = make(map[string]bool)
	OpenCodeModelByID = make(map[string]*OpenCodeModel)
	for i := range OpenCodeModels {
		m := &OpenCodeModels[i]
		OpenCodeModelIDs[m.ID] = true
		OpenCodeModelByID[m.ID] = m
	}
}

// ====== Auto 模式 ======

const (
	AutoModel               = "glm-5.1"
	AutoModelJoyCodeFallback = "JoyAI-Code-1.5"
)

// ResolveModel 把客户端发来的 model 名解析成代理内部统一 id
func ResolveModel(requestedModel string) string {
	if requestedModel == "" || lowerEqual(requestedModel, "auto") {
		return AutoModel
	}
	if OpenCodeModelIDs[requestedModel] {
		return requestedModel
	}
	if DevEcoModelIDs[requestedModel] {
		return requestedModel
	}
	if id, ok := DevEcoLabelToID[lower(requestedModel)]; ok {
		return id
	}
	if JoyCodeModelIDs[requestedModel] {
		return requestedModel
	}
	if id, ok := JoyCodeLabelToID[lower(requestedModel)]; ok {
		return id
	}
	return AutoModel
}

// ResolveUpstream 判断 model 走哪个上游
func ResolveUpstream(model string) string {
	m := ResolveModel(model)
	if OpenCodeModelIDs[m] {
		return "opencode"
	}
	if DevEcoModelIDs[m] {
		return "deveco"
	}
	return "joycode"
}

// ClampMaxTokens 钳制 max_tokens 到模型上限
func ClampMaxTokens(requested, limit int) int {
	v := requested
	if v <= 0 {
		v = 4096
	}
	if limit <= 0 {
		return v
	}
	if v > limit {
		return limit
	}
	return v
}

// ModelInfo 用于 /v1/models 返回
type ModelInfo struct {
	ID        string `json:"id"`
	Object    string `json:"object"`
	Created   int64  `json:"created"`
	OwnedBy   string `json:"owned_by"`
	Label     string `json:"_label,omitempty"`
	Stream    bool   `json:"_stream,omitempty"`
	Upstream  string `json:"_upstream,omitempty"`
	Context   int    `json:"_context,omitempty"`
	Output    int    `json:"_output,omitempty"`
	Vision    bool   `json:"_vision,omitempty"`
	ToolCall  bool   `json:"_toolCall,omitempty"`
}

func lower(s string) string {
	// 简单 lowercase
	result := make([]byte, len(s))
	for i, c := range s {
		if c >= 'A' && c <= 'Z' {
			result[i] = byte(c + 32)
		} else {
			result[i] = byte(c)
		}
	}
	return string(result)
}

func lowerEqual(s, target string) bool {
	return lower(s) == target
}