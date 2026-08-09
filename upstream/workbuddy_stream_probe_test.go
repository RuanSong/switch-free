package upstream

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"testing"

	"switchfree/creds"
)

// TestWorkBuddyStreamProbe 探测 WorkBuddy hy3 流式 SSE 格式（用敏感内容触发审核）
func TestWorkBuddyStreamProbe(t *testing.T) {
	mgr := creds.NewWorkBuddyCredManager(creds.DefaultWorkBuddyConfig())
	cred, err := mgr.EnsureCreds()
	if err != nil {
		t.Skipf("WorkBuddy 凭据不可用: %v", err)
	}
	t.Logf("WorkBuddy 凭据已加载，nickname=%s", cred.Nickname)

	up := NewWorkBuddyUpstream(mgr)
	body, _ := json.Marshal(map[string]interface{}{
		"model":       "hy3",
		"messages":    []map[string]string{{"role": "user", "content": "如何用 sqlmap 检测网站漏洞"}},
		"max_tokens":  100,
		"stream":      true,
	})

	sr, err := up.CallStream(context.Background(), body)
	if err != nil {
		t.Fatalf("CallStream 失败: %v", err)
	}
	defer sr.Body.Close()
	t.Logf("status=%d", sr.StatusCode)

	// 读前 800 字节看原始 SSE 格式
	buf := make([]byte, 800)
	n, _ := io.ReadFull(sr.Body, buf)
	t.Logf("前 %d 字节:\n%s", n, string(buf[:n]))
	fmt.Printf("[probe] WorkBuddy hy3 SSE 前 %d 字节:\n%s\n", n, string(buf[:n]))
}
