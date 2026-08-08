package version

import (
	"embed"
	"regexp"
	"strings"
)

//go:embed config.yml
var configYAML embed.FS

// Version 应用当前版本（可被 ldflags 覆盖：-X switchfree/version.Version=x.y.z）
var Version = ""

func init() {
	if Version == "" {
		Version = readFromConfigYAML()
	}
	if Version == "" {
		Version = "0.0.0"
	}
}

// GetVersion 返回当前版本号
func GetVersion() string { return Version }

// readFromConfigYAML 从 build/config.yml 的 info.version 读取版本号
func readFromConfigYAML() string {
	data, err := configYAML.ReadFile("config.yml")
	if err != nil {
		return ""
	}
	text := string(data)
	// 定位 info: 块，取其下的 version: "x.y.z"
	infoIdx := strings.Index(text, "info:")
	if infoIdx < 0 {
		return ""
	}
	infoBlock := text[infoIdx:]
	// info 块到下一个顶层键（缩进为0的行）
	lines := strings.Split(infoBlock, "\n")
	var block []string
	for i, l := range lines {
		if i > 0 && l != "" && !strings.HasPrefix(l, " ") && !strings.HasPrefix(l, "\t") {
			break
		}
		block = append(block, l)
	}
	blockText := strings.Join(block, "\n")

	re := regexp.MustCompile(`version:\s*"?([0-9]+\.[0-9]+\.[0-9]+)"?`)
	if m := re.FindStringSubmatch(blockText); len(m) >= 2 {
		return m[1]
	}
	return ""
}
