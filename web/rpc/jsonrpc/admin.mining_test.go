package jsonrpc

import (
	"errors"
	"reflect"
	"sort"
	"testing"
)

// TestPartitionMiningControlTargets 覆盖 adminMiningControl 的三态分区：
// 在线可即时下发 / 在线排队补发 / 离线，以及混合输入下的顺序保持。
func TestPartitionMiningControlTargets(t *testing.T) {
	cases := []struct {
		name                   string
		clients                []string
		connected, online      map[string]bool
		wantOnline, wantQueued []string
		wantOffline            []string
	}{
		{
			name:       "connected goes to online",
			clients:    []string{"a"},
			connected:  map[string]bool{"a": true},
			online:     map[string]bool{"a": true},
			wantOnline: []string{"a"},
		},
		{
			name:       "online but disconnected goes to queued",
			clients:    []string{"a"},
			connected:  map[string]bool{},
			online:     map[string]bool{"a": true},
			wantQueued: []string{"a"},
		},
		{
			name:        "not online goes to offline",
			clients:     []string{"a"},
			connected:   map[string]bool{},
			online:      map[string]bool{},
			wantOffline: []string{"a"},
		},
		{
			name:        "mixed input keeps caller order in every partition",
			clients:     []string{"a", "b", "q-c", "off-d", "e"},
			connected:   map[string]bool{"a": true, "b": true, "e": true},
			online:      map[string]bool{"a": true, "b": true, "q-c": true, "e": true},
			wantOnline:  []string{"a", "b", "e"},
			wantQueued:  []string{"q-c"},
			wantOffline: []string{"off-d"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			online, queued, offline := partitionTargets(tc.clients,
				func(u string) bool { return tc.connected[u] },
				func(u string) bool { return tc.online[u] },
			)
			if !reflect.DeepEqual(online, tc.wantOnline) {
				t.Fatalf("online = %v, want %v", online, tc.wantOnline)
			}
			if !reflect.DeepEqual(queued, tc.wantQueued) {
				t.Fatalf("queued = %v, want %v", queued, tc.wantQueued)
			}
			if !reflect.DeepEqual(offline, tc.wantOffline) {
				t.Fatalf("offline = %v, want %v", offline, tc.wantOffline)
			}
		})
	}
}

// TestSliceIsolationPreventsAliasing 校验组合切片独立分配，防止对合并切片的 append 污染原始 online 结果。
func TestSliceIsolationPreventsAliasing(t *testing.T) {
	online := make([]string, 1, 10)
	online[0] = "node-1"
	queued := []string{"node-2"}
	offline := []string{"node-3"}

	taskClients := make([]string, 0, len(online)+len(queued)+len(offline))
	taskClients = append(taskClients, online...)
	taskClients = append(taskClients, queued...)
	taskClients = append(taskClients, offline...)

	if len(online) != 1 || online[0] != "node-1" {
		t.Fatalf("online slice was corrupted: %v", online)
	}
	if len(taskClients) != 3 {
		t.Fatalf("taskClients length = %d, want 3", len(taskClients))
	}
}

// TestDispatchToConnectedContinuesOnFailure 验证单点写失败不中断整批：
// 中间的 b 失败后 c 仍被下发，失败清单精确为 [b]。
func TestDispatchToConnectedContinuesOnFailure(t *testing.T) {
	var calls []string
	sent, failed := dispatchToConnected([]byte("{}"), []string{"a", "b", "c"}, func(u string) error {
		calls = append(calls, u)
		if u == "b" {
			return errors.New("broken pipe")
		}
		return nil
	})
	if !reflect.DeepEqual(calls, []string{"a", "b", "c"}) {
		t.Fatalf("dispatch must continue past failures, calls = %v", calls)
	}
	if !reflect.DeepEqual(sent, []string{"a", "c"}) {
		t.Fatalf("sent = %v, want [a c]", sent)
	}
	if !reflect.DeepEqual(failed, []string{"b"}) {
		t.Fatalf("failed = %v, want [b]", failed)
	}
}

// TestDispatchToConnectedSentPlusFailedCoversAll 成功+失败必须恰好覆盖全部入参（不重不漏）。
func TestDispatchToConnectedSentPlusFailedCoversAll(t *testing.T) {
	sent, failed := dispatchToConnected([]byte("{}"), []string{"a", "b", "c", "d"}, func(u string) error {
		if u == "c" {
			return errors.New("down")
		}
		return nil
	})
	got := append(append([]string{}, sent...), failed...)
	sort.Strings(got)
	want := []string{"a", "b", "c", "d"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("sent+failed = %v, want %v", got, want)
	}
	if !reflect.DeepEqual(failed, []string{"c"}) {
		t.Fatalf("failed = %v, want [c]", failed)
	}
}

// TestDispatchToConnectedAllFail 全部失败时 sent 为空、failed 完整，调用方据此落失败结果。
func TestDispatchToConnectedAllFail(t *testing.T) {
	sent, failed := dispatchToConnected([]byte("{}"), []string{"a", "b"}, func(string) error {
		return errors.New("down")
	})
	if len(sent) != 0 {
		t.Fatalf("sent = %v, want empty", sent)
	}
	if !reflect.DeepEqual(failed, []string{"a", "b"}) {
		t.Fatalf("failed = %v, want [a b]", failed)
	}
}
