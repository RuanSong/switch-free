package updater

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/minio/selfupdate"

	"switchfree/config"
	"switchfree/version"
)

// Updater 自动升级管理器
type Updater struct {
	cfg        *config.Config
	currentVer string
}

// NewUpdater 创建升级管理器
func NewUpdater(cfg *config.Config) *Updater {
	return &Updater{
		cfg:        cfg,
		currentVer: version.GetVersion(),
	}
}

// SetVersion 覆盖当前版本（测试用）
func (u *Updater) SetVersion(v string) { u.currentVer = v }

// GetCurrentVersion 当前版本号
func (u *Updater) GetCurrentVersion() string { return u.currentVer }

// CheckUpdate 检查是否有新版本（不下载）
func (u *Updater) CheckUpdate(ctx context.Context) (*UpdateInfo, error) {
	uc := u.cfg.AutoUpdate
	if !uc.Enabled {
		return nil, nil
	}
	// 自定义 URL 优先
	if uc.UpdateURL != "" {
		return checkCustomURL(ctx, uc.UpdateURL, u.currentVer)
	}
	// GitHub
	if uc.Provider == "github" || uc.Provider == "" {
		if uc.GitHub.Owner == "" || uc.GitHub.Repo == "" {
			return nil, nil
		}
		return CheckGitHubRelease(uc.GitHub, u.currentVer)
	}
	return nil, nil
}

// ApplyUpdate 下载并应用更新（原子替换当前二进制）
func (u *Updater) ApplyUpdate(ctx context.Context, info *UpdateInfo, progress func(UpdateStatus)) error {
	if info == nil {
		return fmt.Errorf("无更新信息")
	}
	if progress != nil {
		progress(UpdateStatus{State: "downloading", Message: "开始下载"})
	}
	// 下载到临时文件
	tmpFile, err := downloadAsset(info.AssetURL, info.AssetSize, progress)
	if err != nil {
		if progress != nil {
			progress(UpdateStatus{State: "error", Message: err.Error()})
		}
		return err
	}
	// 应用完（或失败）后清理临时下载文件
	defer os.Remove(tmpFile)

	if progress != nil {
		progress(UpdateStatus{State: "applying", Message: "应用更新"})
	}
	// selfupdate 原子替换当前运行中的二进制
	file, err := openFile(tmpFile)
	if err != nil {
		if progress != nil {
			progress(UpdateStatus{State: "error", Message: err.Error()})
		}
		return err
	}
	defer file.Close()
	err = selfupdate.Apply(file, selfupdate.Options{})
	if err != nil {
		if progress != nil {
			progress(UpdateStatus{State: "error", Message: err.Error()})
		}
		return err
	}
	if progress != nil {
		progress(UpdateStatus{State: "done", Message: "更新完成，请重启应用"})
	}
	return nil
}

// checkCustomURL 自定义检查地址（返回 JSON：{version, notes, assetUrl, assetSize}）
func checkCustomURL(ctx context.Context, url, currentVer string) (*UpdateInfo, error) {
	body, err := fetchBytes(ctx, url)
	if err != nil {
		return nil, err
	}
	info := &UpdateInfo{}
	if err := json.Unmarshal(body, info); err != nil {
		return nil, err
	}
	if info.Version == "" {
		return nil, nil
	}
	if versionCompare(trimV(info.Version), currentVer) <= 0 {
		return nil, nil
	}
	// 若响应未显式指定 critical，按版本号段变化判定（minor 变化=强制）
	if !info.Critical {
		cur := parseVersion(currentVer)
		nw := parseVersion(trimV(info.Version))
		info.Critical = nw[0] != cur[0] || nw[1] != cur[1]
	}
	return info, nil
}

// fetchBytes GET 请求返回 body
func fetchBytes(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

// openFile 打开文件
func openFile(path string) (*os.File, error) {
	return os.Open(path)
}
