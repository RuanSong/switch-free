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
	// recover 依次尝试各存储副本，返回第一个能通过 valid 校验的密码；
	// 用于主存储（钥匙串）持有过期密码、而兜底副本仍正确时自愈。
	// 单存储平台（windows/linux）等价于「取出并校验」一次。
	recover(service, account string, valid func(string) bool) string
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

// Recover 在主存储可能持有过期密码时，依次尝试各存储副本，
// 返回第一个能通过 valid 校验的密码。若通过校验的是兜底副本，
// 会把它回填到主存储以自愈。没有候选通过校验时返回 ""。
//
// valid 应为幂等的密码校验函数（如尝试解开 DEK），返回 true 表示密码正确。
func Recover(account string, valid func(string) bool) string {
	if valid == nil {
		return ""
	}
	return impl.recover(Service, account, valid)
}
