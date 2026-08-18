// Package keystore 提供跨平台的系统密钥串存取。
//
// macOS  : Login Keychain（security 命令，无需 cgo）
// windows: Credential Manager（凭证管理器）
// linux  : 暂回退到 0600 文件（后续可接 libsecret）
package keystore

// Service 在系统密钥串中的服务名
const Service = "switch-dev"

// Account 在系统密钥串中的账户名（供应商 API 主密码）
const Account = "providerapi-master"

// backend 由平台文件注入
type backend interface {
	set(service, account, value string) error
	get(service, account string) (string, error)
	delete(service, account string) error
}

var impl backend = platformBackend

// Set 保存密钥；value 为空时删除
func Set(account, value string) error {
	if value == "" {
		return Delete(account)
	}
	return impl.set(Service, account, value)
}

// Get 读取密钥；不存在返回 ("", nil)
func Get(account string) (string, error) {
	return impl.get(Service, account)
}

// Delete 删除密钥（不存在不报错）
func Delete(account string) error {
	return impl.delete(Service, account)
}
