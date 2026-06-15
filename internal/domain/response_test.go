package domain

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestOkResponse(t *testing.T) {
	resp := OkResponse("workspace.list", []string{"ws1"})
	if resp.Version != "v2" {
		t.Fatalf("expected version v2, got %s", resp.Version)
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
	if info.Status != "ready" {
		t.Errorf("expected Status=ready, got %q", info.Status)
	}
}

func TestIDEInfoFromInstance_Failed(t *testing.T) {
	ide := &IDEInstance{
		Adapter: "openvscode-server",
		Port:    9100,
		Status:  StatusFailed,
	}
	info := IDEInfoFromInstance(ide)
	if info.Status != "failed" {
		t.Errorf("expected Status=failed, got %q", info.Status)
	}
}

func TestIDEInfoFromInstance_Pending(t *testing.T) {
	ide := &IDEInstance{
		Adapter: "openvscode-server",
		Port:    9100,
		Status:  StatusPending,
	}
	info := IDEInfoFromInstance(ide)
	if info.Status != "pending" {
		t.Errorf("expected Status=pending, got %q", info.Status)
	}
}

func TestIDEInfo_JSON(t *testing.T) {
	info := IDEInfo{Adapter: "openvscode-server", Port: 9100, Status: "ready"}
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
	if !strings.Contains(s, `"status":"ready"`) {
		t.Error("missing status in JSON")
	}
	if strings.Contains(s, `"active"`) {
		t.Error("unexpected active field in JSON — should use status instead")
	}
}

func TestWorkspaceInspectData_IDEInfoOmittedWhenNil(t *testing.T) {
	inspect := WorkspaceInspectData{
		Workspace:     Workspace{Name: "test", Status: StatusReady},
		WorktreeCount: 1,
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
		Workspace:     Workspace{Name: "test", Status: StatusReady},
		WorktreeCount: 1,
		IDEInfo: map[string]*IDEInfo{
			"default": {Adapter: "openvscode-server", Port: 9100, Status: "ready"},
		},
	}
	data, err := json.Marshal(inspect)
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	if !strings.Contains(s, `"ide_info"`) {
		t.Error("expected ide_info in JSON")
	}
	if !strings.Contains(s, `"status":"ready"`) {
		t.Error("expected status:ready in ide_info")
	}
	if strings.Contains(s, `"active"`) {
		t.Error("unexpected active field in ide_info JSON")
	}
}

func TestIDEInfo_NoActiveField(t *testing.T) {
	// Verify the IDEInfo JSON shape has "status" and no "active" field
	info := IDEInfo{Adapter: "openvscode-server", Port: 9100, Status: "ready"}
	data, err := json.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	if strings.Contains(s, `"active"`) {
		t.Error("IDEInfo JSON must not contain 'active' field — use 'status' instead")
	}
	if !strings.Contains(s, `"status":"ready"`) {
		t.Error("IDEInfo JSON must contain 'status' field")
	}
}

func TestIDEInfoFromInstance_EmptyStatus(t *testing.T) {
	// An IDEInstance with zero-value Status should default to "pending"
	ide := &IDEInstance{
		Adapter: "openvscode-server",
		Port:    9100,
	}
	info := IDEInfoFromInstance(ide)
	if info.Status != "pending" {
		t.Errorf("expected default Status=pending, got %q", info.Status)
	}
}

func TestWorkspaceListItemFromInstance_Basic(t *testing.T) {
	inst := &Workspace{
		Name:   "myrepo",
		Repo:   RepoInfo{Host: "github.com", Slug: "org/myrepo"},
		Status: StatusReady,
		Worktrees: []Worktree{
			{Name: "default", IsDefault: true},
		},
	}
	item := WorkspaceListItemFromInstance(inst)
	if item.Name != "myrepo" {
		t.Errorf("expected name myrepo, got %q", item.Name)
	}
	if item.WorktreeCount != 1 {
		t.Errorf("expected worktree_count=1, got %d", item.WorktreeCount)
	}
	if item.Repo.Slug != "org/myrepo" {
		t.Errorf("expected repo slug org/myrepo, got %q", item.Repo.Slug)
	}
}

func TestWorkspaceListItemFromInstance_IDEPort(t *testing.T) {
	// IDE port is surfaced when the default worktree has a ready IDE.
	inst := &Workspace{
		Name:   "myrepo",
		Repo:   RepoInfo{Host: "github.com", Slug: "org/myrepo"},
		Status: StatusReady,
		Worktrees: []Worktree{
			{Name: "default", IsDefault: true},
		},
		IDE: map[string]*IDEInstance{
			"default": {Adapter: "openvscode-server", Port: 9100, Status: StatusReady},
		},
	}
	item := WorkspaceListItemFromInstance(inst)
	if item.IDEPort != 9100 {
		t.Errorf("expected ide_port=9100, got %d", item.IDEPort)
	}
}

func TestWorkspaceListItemFromInstance_IDEPortNotReady(t *testing.T) {
	// IDE port is blank when IDE is not ready.
	inst := &Workspace{
		Name:   "myrepo",
		Repo:   RepoInfo{Host: "github.com", Slug: "org/myrepo"},
		Status: StatusReady,
		Worktrees: []Worktree{
			{Name: "default", IsDefault: true},
		},
		IDE: map[string]*IDEInstance{
			"default": {Adapter: "openvscode-server", Port: 9100, Status: StatusPending},
		},
	}
	item := WorkspaceListItemFromInstance(inst)
	if item.IDEPort != 0 {
		t.Errorf("expected ide_port=0 for non-ready IDE, got %d", item.IDEPort)
	}
}

func TestWorkspaceListItemFromInstance_NoIDE(t *testing.T) {
	// IDE port is blank when no IDE exists.
	inst := &Workspace{
		Name:   "myrepo",
		Repo:   RepoInfo{Host: "github.com", Slug: "org/myrepo"},
		Status: StatusReady,
		Worktrees: []Worktree{
			{Name: "default", IsDefault: true},
		},
	}
	item := WorkspaceListItemFromInstance(inst)
	if item.IDEPort != 0 {
		t.Errorf("expected ide_port=0 when no IDE, got %d", item.IDEPort)
	}
}

func TestWorkspaceListItemFromInstance_JSON(t *testing.T) {
	inst := &Workspace{
		Name:   "myrepo",
		Repo:   RepoInfo{Host: "github.com", Slug: "org/myrepo"},
		Status: StatusReady,
		Worktrees: []Worktree{
			{Name: "default", IsDefault: true},
			{Name: "feature", IsDefault: false},
		},
	}
	item := WorkspaceListItemFromInstance(inst)
	data, _ := json.Marshal(item)
	s := string(data)
	if !strings.Contains(s, `"worktree_count":2`) {
		t.Error("expected worktree_count:2 in JSON")
	}
	if !strings.Contains(s, `"name":"myrepo"`) {
		t.Error("expected name:myrepo in JSON")
	}
}
