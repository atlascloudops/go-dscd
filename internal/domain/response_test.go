package domain

import (
	"encoding/json"
	"testing"
)

func TestOkResponse(t *testing.T) {
	resp := OkResponse("workspace.list", []string{"ws1"})
	if resp.Version != "v1" {
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
	if m["version"] != "v1" || m["command"] != "workspace.list" || m["status"] != "ok" {
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
