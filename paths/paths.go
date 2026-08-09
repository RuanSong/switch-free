package paths

import (
	"os"
	"path/filepath"
	"runtime"
)

// AppConfigDir switch-free 自身的配置/数据目录（配置、日志、费率等）
//   darwin/linux: ~/.config/switch-free（向后兼容已有用户数据）
//   windows: %APPDATA%\switch-free
func AppConfigDir() string {
	if runtime.GOOS == "windows" {
		if dir := os.Getenv("APPDATA"); dir != "" {
			return filepath.Join(dir, "switch-free")
		}
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "switch-free")
}

// AppSupportDir GUI/VSCode 系工具（JoyCode、WorkBuddy）的数据根目录
//   darwin: ~/Library/Application Support
//   linux:  ~/.config
//   windows: %APPDATA%
func AppSupportDir() string {
	if runtime.GOOS == "windows" {
		return os.Getenv("APPDATA")
	}
	home, _ := os.UserHomeDir()
	if runtime.GOOS == "darwin" {
		return filepath.Join(home, "Library", "Application Support")
	}
	return filepath.Join(home, ".config")
}

// XDGConfigDir CLI 工具配置目录（XDG_CONFIG_HOME）
//   darwin/linux: ~/.config
//   windows: %APPDATA%（Windows 无 XDG 约定，回退）
func XDGConfigDir() string {
	if runtime.GOOS == "windows" {
		return os.Getenv("APPDATA")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config")
}

// XDGDataDir CLI 工具数据目录（XDG_DATA_HOME）
//   darwin/linux: ~/.local/share
//   windows: %APPDATA%
func XDGDataDir() string {
	if runtime.GOOS == "windows" {
		return os.Getenv("APPDATA")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share")
}

// XDGStateDir CLI 工具状态目录（XDG_STATE_HOME）
//   darwin/linux: ~/.local/state
//   windows: %LOCALAPPDATA%
func XDGStateDir() string {
	if runtime.GOOS == "windows" {
		return os.Getenv("LOCALAPPDATA")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "state")
}

// EnvOr 环境变量覆盖：env 非空则返回 env，否则返回 def
// 作为 Windows 路径不准时的逃生口（如 JOYCODE_VSCDB）
func EnvOr(env, def string) string {
	if v := os.Getenv(env); v != "" {
		return v
	}
	return def
}

// FirstExists 返回第一个已存在的路径；都不存在则返回第一个非空候选作为默认
func FirstExists(candidates []string) string {
	for _, c := range candidates {
		if c == "" {
			continue
		}
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	for _, c := range candidates {
		if c != "" {
			return c
		}
	}
	return ""
}

// Resolve 路径解析（启动时自动探测）：
//   envKey 非空 -> 强制用环境变量值（不探测，用户明确指定）
//   否则遍历 candidates，返回第一个已存在的
//   都不存在 -> 返回第一个候选作默认（加载时会报"找不到"）
func Resolve(envKey string, candidates []string) string {
	if e := os.Getenv(envKey); e != "" {
		return e
	}
	return FirstExists(candidates)
}

// JoyCodeVscdbCandidates JoyCode state.vscdb 跨平台候选路径
func JoyCodeVscdbCandidates() []string {
	home, _ := os.UserHomeDir()
	rel := filepath.Join("JoyCode", "User", "globalStorage", "state.vscdb")
	c := []string{
		filepath.Join(home, "Library", "Application Support", rel), // macOS
		filepath.Join(home, ".config", rel),                        // Linux
	}
	if appdata := os.Getenv("APPDATA"); appdata != "" {
		c = append(c, filepath.Join(appdata, rel)) // Windows
	}
	return c
}

// WorkBuddyInfoCandidates WorkBuddy 凭据跨平台候选路径
func WorkBuddyInfoCandidates() []string {
	home, _ := os.UserHomeDir()
	rel := filepath.Join("CodeBuddyExtension", "Data", "Public", "auth", "workbuddy-desktop.info")
	c := []string{
		filepath.Join(home, "Library", "Application Support", rel), // macOS
		filepath.Join(home, ".config", rel),                        // Linux
	}
	if appdata := os.Getenv("APPDATA"); appdata != "" {
		c = append(c, filepath.Join(appdata, rel)) // Windows
	}
	return c
}

// OpenCodeAuthCandidates OpenCode auth.json 跨平台候选路径
func OpenCodeAuthCandidates() []string {
	home, _ := os.UserHomeDir()
	c := []string{
		filepath.Join(home, ".local", "share", "opencode", "auth.json"), // macOS/Linux XDG
		filepath.Join(home, ".config", "opencode", "auth.json"),         // 部分发行版变体
	}
	if appdata := os.Getenv("APPDATA"); appdata != "" {
		c = append(c, filepath.Join(appdata, "opencode", "auth.json")) // Windows
	}
	return c
}

// DevEcoAuthCandidates DevEco auth.json 跨平台候选路径
// KEKDir/KVPath 仍用 XDG 目录（与 AuthPath 平台一致）
func DevEcoAuthCandidates() []string {
	home, _ := os.UserHomeDir()
	c := []string{
		filepath.Join(home, ".local", "share", "deveco", "auth.json"), // macOS/Linux XDG
	}
	if appdata := os.Getenv("APPDATA"); appdata != "" {
		c = append(c, filepath.Join(appdata, "deveco", "auth.json")) // Windows
	}
	return c
}
