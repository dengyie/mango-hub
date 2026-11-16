package v2

import (
	"encoding/json"
	"testing"
	"time"
)

// These tests pin the v2 wire format. The hub and the agent now share ONE
// protocol package (module github.com/dengyie/mangohub/protocol), so a field
// rename or tag change anywhere in the fleet contract fails here instead of
// silently breaking 31 deployed agents in the field.

func TestMethodConstantsUniqueAndComplete(t *testing.T) {
	methods := []string{
		MethodAgentReport, MethodAgentBasicInfo, MethodAgentPingResult,
		MethodAgentTaskResult, MethodAgentExec, MethodAgentMiningControl,
		MethodAgentPing, MethodAgentMessage, MethodAgentEvent,
		MethodAgentTerminal, MethodAgentPull, MethodAgentFile,
		MethodAgentFileResult, MethodAgentSSHAuthorize, MethodAgentSSHRevoke,
		MethodAgentSSHTicketResult,
	}
	seen := make(map[string]struct{}, len(methods))
	for _, m := range methods {
		if m == "" {
			t.Fatal("empty method constant")
		}
		if _, dup := seen[m]; dup {
			t.Fatalf("duplicate method constant %q", m)
		}
		seen[m] = struct{}{}
	}
	if Version != "2.0" {
		t.Fatalf("Version = %q", Version)
	}
}

func goldenKeys(t *testing.T, raw []byte, path string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("%s: parse golden payload: %v", path, err)
	}
	return m
}

func requireKeys(t *testing.T, m map[string]any, path string, keys ...string) {
	t.Helper()
	for _, key := range keys {
		if _, ok := m[key]; !ok {
			t.Fatalf("%s missing key %q; got %v", path, key, m)
		}
	}
}

func TestReportNotificationEnvelope(t *testing.T) {
	report := `{"cpu":{"usage":42.5},"ram":{"total":1}}`
	raw := BuildReportPayload([]byte(report))
	m := goldenKeys(t, raw, "report notification")
	requireKeys(t, m, "notification", "jsonrpc", "method", "params")
	if m["method"] != MethodAgentReport {
		t.Fatalf("method = %v, want agent.report", m["method"])
	}
	if _, hasID := m["id"]; hasID {
		t.Fatal("report notification must not carry an id")
	}
	params := m["params"].(map[string]any)
	requireKeys(t, params, "report params", "report")
}

func TestReportRequestCarriesAckEventIDs(t *testing.T) {
	raw := BuildReportRequest("req-1", []byte(`{}`), []string{"evt-7"})
	m := goldenKeys(t, raw, "report request")
	requireKeys(t, m, "request", "jsonrpc", "method", "params", "id")
	params := m["params"].(map[string]any)
	requireKeys(t, params, "report request params", "report", "ack_event_ids")
}

func TestBasicInfoEnvelope(t *testing.T) {
	raw := BuildBasicInfoPayload(map[string]interface{}{"cpu_name": "x"})
	m := goldenKeys(t, raw, "basic info")
	if m["method"] != MethodAgentBasicInfo {
		t.Fatalf("method = %v", m["method"])
	}
	params := m["params"].(map[string]any)
	requireKeys(t, params, "basic info params", "info")
}

func TestPingResultParamsShape(t *testing.T) {
	finished := time.Unix(1700000000, 123456789).UTC()
	payload := BuildPingResultPayload(7, "icmp", 33, finished)
	req, ok := payload.(Request)
	if !ok {
		t.Fatalf("BuildPingResultPayload returns %T", payload)
	}
	raw, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal ping result: %v", err)
	}
	m := goldenKeys(t, raw, "ping result")
	if m["method"] != MethodAgentPingResult {
		t.Fatalf("method = %v", m["method"])
	}
	params := m["params"].(map[string]any)
	for _, key := range []string{"task_id", "ping_type", "value", "finished_at"} {
		if _, ok := params[key]; !ok {
			t.Fatalf("ping result params missing %q: %v", key, params)
		}
	}
	if _, ok := params["finished_at"].(string); !ok {
		t.Fatalf("finished_at must be an RFC3339Nano string, got %T", params["finished_at"])
	}
}

func TestSSHAndTicketParamShapes(t *testing.T) {
	authorize := mustJSON(t, SSHAuthorizeParams{TicketID: "t", PublicKey: "pk", TargetUser: "root", ExpiresAt: time.Unix(1, 0).UTC(), SourceUUID: "u"})
	m := goldenKeys(t, authorize, "ssh.authorize params")
	requireKeys(t, m, "ssh.authorize params", "ticket_id", "public_key", "expires_at")

	revoke := mustJSON(t, SSHRevokeParams{TicketID: "t"})
	requireKeys(t, goldenKeys(t, revoke, "ssh.revoke params"), "ssh.revoke params", "ticket_id")

	ticket := mustJSON(t, SSHTicketResultParams{TicketID: "t", Success: true, Error: ""})
	m = goldenKeys(t, ticket, "ssh.ticketResult params")
	requireKeys(t, m, "ssh.ticketResult params", "ticket_id", "success")
}

func TestTaskResultParamsShape(t *testing.T) {
	raw := mustJSON(t, TaskResultParams{TaskID: "task", Result: "out", ExitCode: 0, FinishedAt: time.Unix(2, 0).UTC()})
	m := goldenKeys(t, raw, "taskResult params")
	requireKeys(t, m, "taskResult params", "task_id", "result", "exit_code", "finished_at")
}

func TestEventResultShape(t *testing.T) {
	raw := mustJSON(t, EventResult{Status: "success", Events: []Event{{
		ID: "e1", Method: MethodAgentExec, Params: map[string]any{"task_id": "x"},
		CreatedAt: time.Unix(3, 0).UTC(), ExpiresAt: time.Unix(9, 0).UTC(),
	}}})
	m := goldenKeys(t, raw, "event result")
	events := m["events"].([]any)
	first := events[0].(map[string]any)
	requireKeys(t, first, "event", "id", "method", "created_at", "expires_at")
}

func TestReportStructWireFields(t *testing.T) {
	// Pin the field names of the top-level report object the hub unmarshals.
	raw := mustJSON(t, Report{
		UUID: "u", CPU: CPUReport{Usage: 1}, Ram: RamReport{Total: 1},
		Swap: RamReport{}, Load: LoadReport{}, Disk: DiskReport{},
		Network: NetworkReport{}, GPU: &GPUDetailReport{}, Mining: &MiningReport{},
	})
	m := goldenKeys(t, raw, "report struct")
	for _, key := range []string{"uuid", "cpu", "ram", "swap", "load", "disk", "network", "gpu", "mining"} {
		if _, ok := m[key]; !ok {
			t.Fatalf("Report missing wire field %q; got %v", key, m)
		}
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal %T: %v", v, err)
	}
	return raw
}
