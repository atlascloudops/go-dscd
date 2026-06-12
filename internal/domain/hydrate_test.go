package domain

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHydrate_DiscoversBareClone(t *testing.T) {
	dir := t.TempDir()
	// Create a workspace structure: code/github.com/org/myrepo/.bare/
	repoRoot := filepath.Join(dir, "github.com", "org", "myrepo")
	bareRoot := filepath.Join(repoRoot, ".bare")
	os.MkdirAll(bareRoot, 0755)

	// Create a default worktree directory with .git file (simulates a worktree checkout)
	defaultWT := filepath.Join(repoRoot, "default")
	os.MkdirAll(defaultWT, 0755)
	os.WriteFile(filepath.Join(defaultWT, ".git"), []byte("gitdir: ../.bare/worktrees/default\n"), 0644)

	store := newMemStore()
	syncer := NewSyncer(store, nil)

	report, err := syncer.Hydrate(dir)
	if err != nil {
		t.Fatal(err)
	}

	// Since we can't run git commands in a test temp dir (no actual git repo),
	// the hydration should report an error for failing to resolve repo info
	if report.WorkspacesDiscovered == 0 && len(report.Errors) == 0 {
		t.Fatal("expected either discovered workspaces or errors")
	}
}

func TestHydrate_SkipsAlreadyKnown(t *testing.T) {
	dir := t.TempDir()
	// Create workspace structure
	repoRoot := filepath.Join(dir, "myrepo")
	bareRoot := filepath.Join(repoRoot, ".bare")
	os.MkdirAll(bareRoot, 0755)

	// Pre-populate state with this workspace
	store := newMemStore()
	store.instances["myrepo"] = &Workspace{
		Name:     "myrepo",
		RepoRoot: repoRoot,
		BareRoot: bareRoot,
		Status:   StatusReady,
		Events: []EventRecord{
			{Scope: EventScope{Kind: ScopeKindWorkspace, Name: "myrepo"}, Event: string(EventCloneCompleted)},
		},
	}

	syncer := NewSyncer(store, nil)
	report, err := syncer.Hydrate(dir)
	if err != nil {
		t.Fatal(err)
	}

	if report.WorkspacesAlreadyKnown != 1 {
		t.Fatalf("expected 1 already known, got %d", report.WorkspacesAlreadyKnown)
	}
	if report.WorkspacesDiscovered != 0 {
		t.Fatalf("expected 0 discovered, got %d", report.WorkspacesDiscovered)
	}

	// Verify existing workspace was not overwritten
	ws := store.instances["myrepo"]
	if ws.Status != StatusReady {
		t.Fatalf("expected status to remain ready, got %s", ws.Status)
	}
	if len(ws.Events) != 1 {
		t.Fatalf("expected 1 event (original), got %d", len(ws.Events))
	}
}

func TestHydrate_Idempotent(t *testing.T) {
	dir := t.TempDir()
	repoRoot := filepath.Join(dir, "myrepo")
	bareRoot := filepath.Join(repoRoot, ".bare")
	os.MkdirAll(bareRoot, 0755)

	store := newMemStore()
	syncer := NewSyncer(store, nil)

	// First hydrate — will fail to resolve repo info (no git repo)
	report1, err := syncer.Hydrate(dir)
	if err != nil {
		t.Fatal(err)
	}

	// Second hydrate — should produce same result
	report2, err := syncer.Hydrate(dir)
	if err != nil {
		t.Fatal(err)
	}

	// Both should have the same error count (can't resolve repo info)
	if len(report1.Errors) != len(report2.Errors) {
		t.Fatalf("idempotency: error count changed from %d to %d", len(report1.Errors), len(report2.Errors))
	}
}

func TestHydrate_ToleratesCorruptBareClone(t *testing.T) {
	dir := t.TempDir()
	// Create a directory with .bare/ but no git repository inside
	repoRoot := filepath.Join(dir, "corrupt-repo")
	bareRoot := filepath.Join(repoRoot, ".bare")
	os.MkdirAll(bareRoot, 0755)

	store := newMemStore()
	syncer := NewSyncer(store, nil)

	report, err := syncer.Hydrate(dir)
	if err != nil {
		t.Fatal(err)
	}

	// Should not crash — should report an error for the corrupt clone
	if len(report.Errors) == 0 {
		t.Fatal("expected errors for corrupt bare clone")
	}
	if report.WorkspacesDiscovered != 0 {
		t.Fatalf("expected 0 discovered for corrupt clone, got %d", report.WorkspacesDiscovered)
	}
}

func TestHydrate_EmptyRootNoError(t *testing.T) {
	dir := t.TempDir()

	store := newMemStore()
	syncer := NewSyncer(store, nil)

	report, err := syncer.Hydrate(dir)
	if err != nil {
		t.Fatal(err)
	}

	if report.WorkspacesDiscovered != 0 {
		t.Fatalf("expected 0 discovered, got %d", report.WorkspacesDiscovered)
	}
	if report.WorkspacesAlreadyKnown != 0 {
		t.Fatalf("expected 0 already known, got %d", report.WorkspacesAlreadyKnown)
	}
	if len(report.Errors) != 0 {
		t.Fatalf("expected 0 errors, got %d: %v", len(report.Errors), report.Errors)
	}
}

func TestHydrate_ReportStructure(t *testing.T) {
	dir := t.TempDir()

	store := newMemStore()
	syncer := NewSyncer(store, nil)

	report, err := syncer.Hydrate(dir)
	if err != nil {
		t.Fatal(err)
	}

	// Verify HydrateReport has all expected fields
	if report == nil {
		t.Fatal("expected non-nil report")
	}
	// These should be zero for empty directory
	if report.WorkspacesDiscovered != 0 {
		t.Fatalf("expected 0, got %d", report.WorkspacesDiscovered)
	}
	if report.WorkspacesAlreadyKnown != 0 {
		t.Fatalf("expected 0, got %d", report.WorkspacesAlreadyKnown)
	}
}

func TestScanForBareClones_FindsNestedBareClones(t *testing.T) {
	dir := t.TempDir()

	// Create structure: github.com/org/repo1/.bare and github.com/org/repo2/.bare
	os.MkdirAll(filepath.Join(dir, "github.com", "org", "repo1", ".bare"), 0755)
	os.MkdirAll(filepath.Join(dir, "github.com", "org", "repo2", ".bare"), 0755)

	entries, errors := scanForBareClones(dir, 4)
	if len(errors) != 0 {
		t.Fatalf("unexpected errors: %v", errors)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}

	// Verify entries point to correct paths
	names := map[string]bool{}
	for _, e := range entries {
		names[filepath.Base(e.repoRoot)] = true
		expectedBare := filepath.Join(e.repoRoot, ".bare")
		if e.bareRoot != expectedBare {
			t.Fatalf("expected bareRoot %s, got %s", expectedBare, e.bareRoot)
		}
	}
	if !names["repo1"] || !names["repo2"] {
		t.Fatalf("expected repo1 and repo2, got %v", names)
	}
}

func TestScanForBareClones_RespectsMaxDepth(t *testing.T) {
	dir := t.TempDir()

	// Create deeply nested structure beyond depth limit
	os.MkdirAll(filepath.Join(dir, "a", "b", "c", "d", "e", ".bare"), 0755)

	// Depth 4 should not find it (depth 0=a, 1=b, 2=c, 3=d, 4=e — but .bare is at depth 5)
	entries, _ := scanForBareClones(dir, 3)
	if len(entries) != 0 {
		t.Fatalf("expected 0 entries at depth 3, got %d", len(entries))
	}

	// Depth 5 should find it
	entries, _ = scanForBareClones(dir, 5)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry at depth 5, got %d", len(entries))
	}
}

func TestScanForBareClones_SkipsHiddenDirs(t *testing.T) {
	dir := t.TempDir()

	// Create .hidden/repo/.bare — should be skipped
	os.MkdirAll(filepath.Join(dir, ".hidden", "repo", ".bare"), 0755)
	// Create visible/repo/.bare — should be found
	os.MkdirAll(filepath.Join(dir, "visible", "repo", ".bare"), 0755)

	entries, _ := scanForBareClones(dir, 4)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry (hidden skipped), got %d", len(entries))
	}
	if filepath.Base(entries[0].repoRoot) != "repo" {
		t.Fatalf("expected repo, got %s", filepath.Base(entries[0].repoRoot))
	}
}

func TestBoot_ComposesHydrateThenSync(t *testing.T) {
	dir := t.TempDir()
	projectRoot := filepath.Join(dir, "ws-root", "repo")
	os.MkdirAll(filepath.Join(projectRoot, "default", ".git"), 0755)

	wsRoot := filepath.Join(dir, "ws-root")

	store := newMemStore()
	// Pre-populate with a workspace that has a default worktree
	store.instances["repo"] = &Workspace{
		Name:     "repo",
		RepoRoot: projectRoot,
		BareRoot: filepath.Join(projectRoot, ".bare"),
		Owner:    "user",
		Status:   StatusPending,
		Worktrees: []Worktree{
			{Name: "default", ProjectRoot: filepath.Join(projectRoot, "default"), IsDefault: true},
		},
	}

	syncer := NewSyncer(store, nil)
	report, err := syncer.Boot(wsRoot)
	if err != nil {
		t.Fatal(err)
	}

	if report.Hydrate == nil {
		t.Fatal("expected hydrate report")
	}
	if report.Sync == nil {
		t.Fatal("expected sync report")
	}

	// Sync should have checked the pre-existing workspace
	if report.Sync.WorkspacesChecked != 1 {
		t.Fatalf("expected 1 workspace checked in sync, got %d", report.Sync.WorkspacesChecked)
	}

	// The sync should have detected the clone on disk and moved to ready
	if store.instances["repo"].Status != StatusReady {
		t.Fatalf("expected ready after boot, got %s", store.instances["repo"].Status)
	}
}

func TestBoot_ReportStructure(t *testing.T) {
	dir := t.TempDir()

	store := newMemStore()
	syncer := NewSyncer(store, nil)

	report, err := syncer.Boot(dir)
	if err != nil {
		t.Fatal(err)
	}

	if report == nil {
		t.Fatal("expected non-nil boot report")
	}
	if report.Hydrate == nil {
		t.Fatal("expected non-nil hydrate report in boot report")
	}
	if report.Sync == nil {
		t.Fatal("expected non-nil sync report in boot report")
	}
}

func TestParseOriginURL_HTTPS(t *testing.T) {
	tests := []struct {
		url      string
		host     string
		slug     string
		cloneURL string
	}{
		{
			url:      "https://github.com/org/repo.git",
			host:     "github.com",
			slug:     "org/repo",
			cloneURL: "https://github.com/org/repo.git",
		},
		{
			url:      "https://github.com/org/repo",
			host:     "github.com",
			slug:     "org/repo",
			cloneURL: "https://github.com/org/repo",
		},
		{
			url:      "https://gitlab.example.com/team/sub/project.git",
			host:     "gitlab.example.com",
			slug:     "team/sub/project",
			cloneURL: "https://gitlab.example.com/team/sub/project.git",
		},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			info, err := ParseOriginURL(tt.url)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if info.Host != tt.host {
				t.Fatalf("expected host %q, got %q", tt.host, info.Host)
			}
			if info.Slug != tt.slug {
				t.Fatalf("expected slug %q, got %q", tt.slug, info.Slug)
			}
			if info.CloneURL != tt.cloneURL {
				t.Fatalf("expected cloneURL %q, got %q", tt.cloneURL, info.CloneURL)
			}
		})
	}
}

func TestParseOriginURL_SSH(t *testing.T) {
	tests := []struct {
		url  string
		host string
		slug string
	}{
		{
			url:  "git@github.com:org/repo.git",
			host: "github.com",
			slug: "org/repo",
		},
		{
			url:  "git@gitlab.example.com:team/project.git",
			host: "gitlab.example.com",
			slug: "team/project",
		},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			info, err := ParseOriginURL(tt.url)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if info.Host != tt.host {
				t.Fatalf("expected host %q, got %q", tt.host, info.Host)
			}
			if info.Slug != tt.slug {
				t.Fatalf("expected slug %q, got %q", tt.slug, info.Slug)
			}
		})
	}
}

func TestParseOriginURL_InvalidURLs(t *testing.T) {
	invalidURLs := []string{
		"",
		"not-a-url",
		"://missing-scheme",
	}

	for _, u := range invalidURLs {
		t.Run(u, func(t *testing.T) {
			_, err := ParseOriginURL(u)
			if err == nil {
				t.Fatalf("expected error for invalid URL %q", u)
			}
		})
	}
}
