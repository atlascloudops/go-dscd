package domain

import (
	"os"
	"testing"
)

func TestResolveWorkspaceRoot_Default(t *testing.T) {
	// Ensure env var is unset
	os.Unsetenv("DSCD_WORKSPACE_ROOT")

	got := ResolveWorkspaceRoot("ubuntu")
	want := "/home/ubuntu/code"
	if got != want {
		t.Errorf("ResolveWorkspaceRoot(ubuntu) = %q, want %q", got, want)
	}
}

func TestResolveWorkspaceRoot_CustomOwner(t *testing.T) {
	os.Unsetenv("DSCD_WORKSPACE_ROOT")

	got := ResolveWorkspaceRoot("jperez")
	want := "/home/jperez/code"
	if got != want {
		t.Errorf("ResolveWorkspaceRoot(jperez) = %q, want %q", got, want)
	}
}

func TestResolveWorkspaceRoot_EnvOverride(t *testing.T) {
	os.Setenv("DSCD_WORKSPACE_ROOT", "/opt/workspaces")
	defer os.Unsetenv("DSCD_WORKSPACE_ROOT")

	got := ResolveWorkspaceRoot("ubuntu")
	want := "/opt/workspaces"
	if got != want {
		t.Errorf("ResolveWorkspaceRoot(ubuntu) = %q, want %q", got, want)
	}
}

func TestDeriveRepoRoot(t *testing.T) {
	tests := []struct {
		name          string
		workspaceRoot string
		host          string
		slug          string
		want          string
	}{
		{
			name:          "github standard",
			workspaceRoot: "/home/ubuntu/code",
			host:          "github.com",
			slug:          "org/repo",
			want:          "/home/ubuntu/code/github.com/org/repo",
		},
		{
			name:          "gitlab nested",
			workspaceRoot: "/home/jperez/code",
			host:          "gitlab.com",
			slug:          "group/subgroup/repo",
			want:          "/home/jperez/code/gitlab.com/group/subgroup/repo",
		},
		{
			name:          "custom workspace root",
			workspaceRoot: "/opt/workspaces",
			host:          "github.com",
			slug:          "org/repo",
			want:          "/opt/workspaces/github.com/org/repo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DeriveRepoRoot(tt.workspaceRoot, tt.host, tt.slug)
			if got != tt.want {
				t.Errorf("DeriveRepoRoot() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDeriveBareRoot(t *testing.T) {
	got := DeriveBareRoot("/home/ubuntu/code/github.com/org/repo")
	want := "/home/ubuntu/code/github.com/org/repo/.bare"
	if got != want {
		t.Errorf("DeriveBareRoot() = %q, want %q", got, want)
	}
}

func TestDeriveProjectRoot(t *testing.T) {
	repoRoot := "/home/ubuntu/code/github.com/org/repo"

	tests := []struct {
		name         string
		worktreeName string
		want         string
	}{
		{
			name:         "default worktree",
			worktreeName: "default",
			want:         "/home/ubuntu/code/github.com/org/repo/default",
		},
		{
			name:         "named branch",
			worktreeName: "feat",
			want:         "/home/ubuntu/code/github.com/org/repo/.worktrees/feat",
		},
		{
			name:         "nested branch name",
			worktreeName: "feat/bar",
			want:         "/home/ubuntu/code/github.com/org/repo/.worktrees/feat/bar",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DeriveProjectRoot(repoRoot, tt.worktreeName)
			if got != tt.want {
				t.Errorf("DeriveProjectRoot() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDeriveLocalRepoRoot(t *testing.T) {
	got := DeriveLocalRepoRoot("/home/ubuntu/code", "my-project")
	want := "/home/ubuntu/code/local/my-project"
	if got != want {
		t.Errorf("DeriveLocalRepoRoot() = %q, want %q", got, want)
	}
}

func TestExpandHome(t *testing.T) {
	tests := []struct {
		name  string
		path  string
		owner string
		want  string
	}{
		{"tilde expansion", "~/code", "ubuntu", "/home/ubuntu/code"},
		{"tilde expansion custom user", "~/work", "jperez", "/home/jperez/work"},
		{"absolute path unchanged", "/opt/src", "ubuntu", "/opt/src"},
		{"empty string", "", "ubuntu", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := expandHome(tt.path, tt.owner)
			if got != tt.want {
				t.Errorf("expandHome(%q, %q) = %q, want %q", tt.path, tt.owner, got, tt.want)
			}
		})
	}
}
