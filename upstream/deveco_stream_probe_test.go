package upstream

import (
	"context"
	"encoding/json"
	"testing"

	"switchdev/creds"
)

// TestDevEcoStreamProbe 探测 DevEco 流式 SSE 格式（验证 CallStream 实现）
//
// 运行：go test -run TestDevEcoStreamProbe -v ./upstream/
func TestDevEcoStreamProbe(t *testing.T) {
	mgr := creds.NewDevEcoCredManager(creds.DefaultDevEcoConfig())
	cred, err := mgr.EnsureCreds()
	if err != nil {
		t.Skipf("DevEco 凭据不可用，跳过探测: %v", err)
	}
	t.Logf("DevEco 凭据已加载，accessToken 前 8 位=%s", safePrefix(cred.AccessToken, 8))

	up := NewDevEcoUpstream(mgr)
	body := map[string]interface{}{
		"model": "glm-5.1",
		"messages": []map[string]string{
			{"role": "user", "content": "用一句话介绍你自己"},
		},
		"max_tokens": 50,
	}
	bodyBytes, _ := json.Marshal(body)

	sr, err := up.CallStream(context.Background(), bodyBytes)
	if err != nil {
		t.Fatalf("CallStream 失败: %v", err)
	}
	defer sr.Body.Close()

	t.Logf("status=%d", sr.StatusCode)
	buf := make([]byte, 2048)
	n, _ := sr.Body.Read(buf)
	t.Logf("响应前 %d 字节:\n%s", n, string(buf[:n]))
}

func safePrefix(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
