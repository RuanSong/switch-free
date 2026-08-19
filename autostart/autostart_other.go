//go:build !darwin && !windows && !linux

package autostart

import "fmt"

// IsEnabled 在不支持的平台始终返回 false。
func (a *App) IsEnabled() bool { return false }

// Enable 在不支持的平台返回错误。
func (a *App) Enable() error { return fmt.Errorf("autostart: 当前平台不支持") }

// Disable 在不支持的平台无操作。
func (a *App) Disable() error { return nil }
