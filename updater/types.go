package updater

// UpdateInfo 更新信息
type UpdateInfo struct {
	Version   string `json:"version"`   // 新版本号（如 "0.1.0"）
	Notes     string `json:"notes"`     // 更新说明
	AssetURL  string `json:"assetUrl"`  // 资产下载地址
	AssetName string `json:"assetName"` // 资产文件名
	AssetSize int64  `json:"assetSize"` // 资产大小（字节）
}

// UpdateStatus 更新状态（用于进度回调）
type UpdateStatus struct {
	State      string  `json:"state"`      // "checking" | "downloading" | "applying" | "done" | "error"
	Downloaded int64   `json:"downloaded"` // 已下载字节
	Total      int64   `json:"total"`      // 总字节
	Percent    float64 `json:"percent"`    // 0-100
	Message    string  `json:"message"`    // 附加信息
}
