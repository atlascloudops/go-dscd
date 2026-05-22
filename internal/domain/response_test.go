package domain

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestOkResponse(t *testing.T) {
	resp := OkResponse("workspace.list", []string{"ws1"})
	if resp.Version != "v2" {
		t.Fatalf("expected version v1, got %s", resp.Version)
	}
	if resp.Status != "ok" {
		t.Fatalf("expected status ok, got %s", resp.Status)
	}
	if resp.Error != nil {
		t.Fatal("expected nil error")
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]interface{}
	json.Unmarshal(data, &m)
	if m["version"] != "v2" || m["command"] != "workspace.list" || m["status"] != "ok" {
		t.Fatalf("unexpected JSON: %s", data)
	}
	if m["error"] != nil {
		t.Fatalf("expected null error in JSON, got %v", m["error"])
	}
}

func TestErrorResponse(t *testing.T) {
	resp := ErrorResponse("workspace.provision", ErrorInfo{
		Code:    ErrCloneFailed,
		Message: "git clone failed",
		Detail:  "exit status 128",
	})
	if resp.Status != "error" {
		t.Fatalf("expected status error, got %s", resp.Status)
	}
	if resp.Data != nil {
		t.Fatal("expected nil data")
	}
	if resp.Error.Code != ErrCloneFailed {
		t.Fatalf("expected code CLONE_FAILED, got %s", resp.Error.Code)
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]interface{}
	json.Unmarshal(data, &m)
	if m["data"] != nil {
		t.Fatalf("expected null data in JSON, got %v", m["data"])
	}
	errObj := m["error"].(map[string]interface{})
	if errObj["code"] != "CLONE_FAILED" {
		t.Fatalf("unexpected error code in JSON: %v", errObj["code"])
	}
}

func TestIDEInfoFromInstance_Nil(t *testing.T) {
	info := IDEInfoFromInstance(nil)
	if info != nil {
		t.Fatal("expected nil for nil IDEInstance")
	}
}

func TestIDEInfoFromInstance_Ready(t *testing.T) {
	ide := &IDEInstance{
		Adapter: "openvscode-server",
		Port:    9100,
		Status:  StatusReady,
	}
	info := IDEInfoFromInstance(ide)
	if info == nil {
		t.Fatal("expected non-nil IDEInfo")
	}
	if info.Adapter != "openvscode-server" {
		t.Errorf("adapter: expected openvscode-server, got %q", info.Adapter)
	}
	if info.Port != 9100 {
		t.Errorf("port: expected 9100, got %d", info.Port)
	}
	if !info.Active {
		t.Error("expected Active=true when status is ready")
	}
}

func TestIDEInfoFromInstance_Failed(t *testing.T) {
	ide := &IDEInstance{
		Adapter: "openvscode-server",
		Port:    9100,
		Status:  StatusFailed,
	}
	info := IDEInfoFromInstance(ide)
	if info.Active {
		t.Error("expected Active=false when status is failed")
	}
}

func TestIDEInfoFromInstance_Pending(t *testing.T) {
	ide := &IDEInstance{
		Adapter: "openvscode-server",
		Port:    9100,
		Status:  StatusPending,
	}
	info := IDEInfoFromInstance(ide)
	if info.Active {
		t.Error("expected Active=false when status is pending")
	}
}

func TestIDEInfo_JSON(t *testing.T) {
	info := IDEInfo{Adapter: "openvscode-server", Port: 9100, Active: true}
	data, err := json.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	if !strings.Contains(s, `"adapter":"openvscode-server"`) {
		t.Error("missing adapter in JSON")
	}
	if !strings.Contains(s, `"port":9100`) {
		t.Error("missing port in JSON")
	}
	if !strings.Contains(s, `"active":true`) {
		t.Error("missing active in JSON")
	}
}

func TestWorkspaceInspectData_IDEInfoOmittedWhenNil(t *testing.T) {
	inspect := WorkspaceInspectData{
		WorkspaceInstance: WorkspaceInstance{Status: StatusReady},
		BareRoot:         "/tmp/.bare",
		WorktreeCount:    1,
		Worktrees:        []string{"/tmp/default"},
	}
	data, err := json.Marshal(inspect)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), `"ide_info"`) {
		t.Error("expected ide_info to be omitted when nil")
	}
}

func TestWorkspaceInspectData_IDEInfoPresent(t *testing.T) {
	inspect := WorkspaceInspectData{
		WorkspaceInstance: WorkspaceInstance{Status: StatusReady},
		BareRoot:         "/tmp/.bare",
		WorktreeCount:    1,
		Worktrees:        []string{"/tmp/default"},
		IDEInfo:          &IDEInfo{Adapter: "openvscode-server", Port: 9100, Active: true},
	}
	data, err := json.Marshal(inspect)
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	if !strings.Contains(s, `"ide_info"`) {
		t.Error("expected ide_info in JSON")
	}
	if !strings.Contains(s, `"active":true`) {
		t.Error("expected active:true in ide_info")
	}
}

func TestWorkspaceListItemFromInstance_NoIDE(t *testing.T) {
	inst := &WorkspaceInstance{
		Spec:   WorkspaceSpec{Name: "myrepo"},
		Status: StatusReady,
	}
	item := WorkspaceListItemFromInstance(inst)
	if item.IDEPort != 0 {
		t.Errorf("expected ide_port=0 (omitted), got %d", item.IDEPort)
	}

	// Verify ide_port is omitted from JSON
	data, _ := json.Marshal(item)
	if strings.Contains(string(data), `"ide_port"`) {
		t.Error("expected ide_port to be omitted from JSON when no IDE")
	}
}

func TestWorkspaceListItemFromInstance_IDEActive(t *testing.T) {
	inst := &WorkspaceInstance{
		Spec:   WorkspaceSpec{Name: "myrepo"},
		Status: StatusReady,
		IDE: &IDEInstance{
			Adapter: "openvscode-server",
			Port:    9100,
			Status:  StatusReady,
		},
	}
	item := WorkspaceListItemFromInstance(inst)
	if item.IDEPort != 9100 {
		t.Errorf("expected ide_port=9100, got %d", item.IDEPort)
	}

	data, _ := json.Marshal(item)
	if !strings.Contains(string(data), `"ide_port":9100`) {
		t.Error("expected ide_port:9100 in JSON")
	}
}

func TestWorkspaceListItemFromInstance_IDEInactive(t *testing.T) {
	inst := &WorkspaceInstance{
		Spec:   WorkspaceSpec{Name: "myrepo"},
		Status: StatusReady,
		IDE: &IDEInstance{
			Adapter: "openvscode-server",
			Port:    9100,
			Status:  StatusFailed,
		},
	}
	item := WorkspaceListItemFromInstance(inst)
	if item.IDEPort != 0 {
		t.Errorf("expected ide_port=0 when IDE not active, got %d", item.IDEPort)
	}

	data, _ := json.Marshal(item)
	if strings.Contains(string(data), `"ide_port"`) {
		t.Error("expected ide_port to be omitted when IDE not active")
	}
}
