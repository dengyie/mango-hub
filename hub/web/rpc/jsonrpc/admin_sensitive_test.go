package jsonrpc

import (
	"testing"

	"github.com/dengyie/mangohub/hub/pkg/rpc"
)

// miningControl 与 exec 同属远控敏感操作，必须登记在敏感操作表里，
// 由 RPC 边界强制走敏感操作二次验证（API Key principal 豁免不受影响）。
// 本包 init() 侧的 rpc.MarkSensitive 随导入生效，这里验证登记结果。
func TestRemoteControlMethodsAreMarkedSensitive(t *testing.T) {
	for _, m := range []string{"admin:miningControl", "admin:exec"} {
		if !rpc.IsSensitive(m) {
			t.Errorf("%s should be marked sensitive", m)
		}
	}
	if rpc.IsSensitive("admin:getNodes") {
		t.Error("admin:getNodes should not be sensitive")
	}
}
