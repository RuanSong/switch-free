package db

import (
	"encoding/json"
	"sync"

	"switchdev/providerapi"
)

// Crypto 持有当前可用的 DEK，提供字段级加解密。
// DEK 不可用时（锁定状态）加密降级为明文，解密兼容明文。
type Crypto struct {
	mu  sync.RWMutex
	dek []byte
}

func NewCrypto() *Crypto {
	return &Crypto{}
}

// SetDEK 注入 DEK（解锁时）或清除（锁定时传 nil）
func (c *Crypto) SetDEK(dek []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if dek == nil {
		c.dek = nil
		return
	}
	cp := make([]byte, len(dek))
	copy(cp, dek)
	c.dek = cp
}

func (c *Crypto) available() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.dek != nil
}

// EncryptField 加密明文为 sealed JSON 字符串。
// DEK 不可用时返回原始明文（降级）。
func (c *Crypto) EncryptField(plaintext string) string {
	if plaintext == "" {
		return ""
	}
	if !c.available() {
		return plaintext
	}
	c.mu.RLock()
	dek := c.dek
	c.mu.RUnlock()

	sealed, err := providerapi.AesGCMSeal(dek, []byte(plaintext))
	if err != nil {
		return plaintext
	}
	b, err := json.Marshal(sealed)
	if err != nil {
		return plaintext
	}
	return string(b)
}

// DecryptField 尝试解密封装字符串。
// 解析失败/DEK不可用/认证失败时返回原文（兼容明文历史数据）。
func (c *Crypto) DecryptField(ciphertext string) string {
	if ciphertext == "" {
		return ""
	}
	var s providerapi.Sealed
	if err := json.Unmarshal([]byte(ciphertext), &s); err != nil {
		return ciphertext // 不是 sealed JSON，当作明文
	}
	if s.Ciphertext == "" || s.IV == "" {
		return ciphertext
	}
	if !c.available() {
		return "" // 是密文但 DEK 不可用，返回空避免泄露乱码
	}
	c.mu.RLock()
	dek := c.dek
	c.mu.RUnlock()

	plain, err := providerapi.AesGCMOpen(dek, &s)
	if err != nil {
		return "" // 解密失败（DEK 变了/数据损坏），返回空
	}
	return string(plain)
}
