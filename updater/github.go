package updater

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"time"

	"switchfree/config"
)

// githubRelease GitHub Releases API 响应结构（只取需要的字段）
type githubRelease struct {
	TagName string `json:"tag_name"`
	Body    string `json:"body"`
	Assets  []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
		Size               int64  `json:"size"`
	} `json:"assets"`
}

// assetName 按当前平台/架构生成资产文件名
func assetName() string {
	switch runtime.GOOS {
	case "darwin":
		if runtime.GOARCH == "arm64" {
			return "switch-free-darwin-arm64"
		}
		return "switch-free-darwin-amd64"
	case "windows":
		return "switch-free-windows-amd64.exe"
	case "linux":
		return "switch-free-linux-amd64"
	}
	return "switch-free-" + runtime.GOOS + "-" + runtime.GOARCH
}

// CheckGitHubRelease 查 GitHub latest release
// 有更新返回 *UpdateInfo；无更新或同版本返回 nil
func CheckGitHubRelease(gh config.GitHubConfig, currentVersion string) (*UpdateInfo, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", gh.Owner, gh.Repo)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "switch-free-updater")
	if gh.Token != "" {
		req.Header.Set("Authorization", "Bearer "+gh.Token)
	}

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == 404 {
		// 无 release，视为无更新
		return nil, nil
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("GitHub API 返回 %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var rel githubRelease
	if err := json.Unmarshal(body, &rel); err != nil {
		return nil, err
	}

	// 去掉 tag 的 v 前缀比较版本
	newVersion := trimV(rel.TagName)
	if newVersion == "" || versionCompare(newVersion, currentVersion) <= 0 {
		return nil, nil // 无新版本
	}

	// 判定是否强制更新：
	// major 或 minor 段变化（含 0.x.x 的 minor 变化）-> 强制
	// 仅 patch 段变化（0.0.x）-> 普通更新，用户可忽略
	cur := parseVersion(currentVersion)
	nw := parseVersion(newVersion)
	critical := nw[0] != cur[0] || nw[1] != cur[1]

	// 找匹配当前平台的资产
	target := assetName()
	for _, a := range rel.Assets {
		if a.Name == target {
			return &UpdateInfo{
				Version:   newVersion,
				Notes:     rel.Body,
				AssetURL:  a.BrowserDownloadURL,
				AssetName: a.Name,
				AssetSize: a.Size,
				Critical:  critical,
			}, nil
		}
	}
	return nil, fmt.Errorf("未找到匹配平台 %s 的资产（需要 %s）", runtime.GOOS+"/"+runtime.GOARCH, target)
}

// trimV 去掉版本号开头的 v
func trimV(s string) string {
	if len(s) > 0 && s[0] == 'v' {
		return s[1:]
	}
	return s
}

// versionCompare 比较两个版本号（x.y.z），a > b 返回正数
func versionCompare(a, b string) int {
	pa := parseVersion(a)
	pb := parseVersion(b)
	for i := 0; i < 3; i++ {
		if pa[i] != pb[i] {
			return pa[i] - pb[i]
		}
	}
	return 0
}

func parseVersion(s string) [3]int {
	var v [3]int
	var major, minor, patch int
	if _, err := fmt.Sscanf(s, "%d.%d.%d", &major, &minor, &patch); err != nil {
		return v
	}
	v[0], v[1], v[2] = major, minor, patch
	return v
}

// downloadAsset 下载资产到临时文件，带进度回调
func downloadAsset(url string, total int64, progress func(UpdateStatus)) (string, error) {
	// 创建临时文件
	tmp, err := os.CreateTemp("", "switch-free-update-*.bin")
	if err != nil {
		return "", err
	}
	// 注意：不在此处 defer Remove，文件交给调用方 ApplyUpdate 在应用完更新后清理
	// （否则函数返回即删除，后续 openFile 会找不到文件）

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return "", err
	}
	client := &http.Client{Timeout: 300 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return "", err
	}
	defer resp.Body.Close()

	downloaded := int64(0)
	buf := make([]byte, 64*1024)
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			tmp.Write(buf[:n])
			downloaded += int64(n)
			if progress != nil && total > 0 {
				progress(UpdateStatus{
					State:      "downloading",
					Downloaded: downloaded,
					Total:      total,
					Percent:    float64(downloaded) / float64(total) * 100,
				})
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			tmp.Close()
			os.Remove(tmp.Name())
			return "", err
		}
	}
	tmp.Close()
	return tmp.Name(), nil
}
