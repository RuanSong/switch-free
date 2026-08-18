package service

import (
	"context"

	"github.com/wailsapp/wails/v3/pkg/application"

	"switchdev/updater"
)

// UpdaterService 自动升级服务（暴露给前端）
type UpdaterService struct {
	updater *updater.Updater
}

func NewUpdaterService(up *updater.Updater) *UpdaterService {
	return &UpdaterService{updater: up}
}

// GetCurrentVersion 当前版本号
func (s *UpdaterService) GetCurrentVersion() string {
	return s.updater.GetCurrentVersion()
}

// CheckUpdate 检查是否有新版本
func (s *UpdaterService) CheckUpdate() (*updater.UpdateInfo, error) {
	return s.updater.CheckUpdate(context.Background())
}

// ApplyUpdate 下载并应用更新（阻塞，进度通过事件推送）
func (s *UpdaterService) ApplyUpdate(info *updater.UpdateInfo) error {
	progress := func(status updater.UpdateStatus) {
		s.emitProgress(status)
	}
	return s.updater.ApplyUpdate(context.Background(), info, progress)
}

// EmitUpdateAvailable 推送有可用更新事件（供 main 启动检查用）
func (s *UpdaterService) EmitUpdateAvailable(info *updater.UpdateInfo) {
	s.emit("update:available", info)
}

// emitProgress 推送更新进度事件
func (s *UpdaterService) emitProgress(status updater.UpdateStatus) {
	s.emit("update:progress", status)
}

// emit 推送事件
func (s *UpdaterService) emit(event string, data interface{}) {
	if app := application.Get(); app != nil {
		app.Event.Emit(event, data)
	}
}
