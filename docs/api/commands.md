# Commands

## `dscd status`

Show daemon status.

**Arguments:** none

**Response `data`:**
```json
{
  "dscd_version": "string",
  "state_file": "string",
  "state_file_exists": true,
  "state_file_size_bytes": 1234,
  "workspace_count": 3,
  "workspace_summary": { "ready": 2, "error": 1, "pending": 0, "provisioning": 0 },
  "last_synced_at": "2026-05-20T12:00:00Z" | null
}
```

**Errors:** `STATE_CORRUPT`

---

## `dscd workspace provision '<spec-json>'`

Provision a workspace from an inline JSON spec. Uses bare-clone + worktree layout.

**Arguments:** exactly 1 — the JSON spec string

**Input schema (`WorkspaceSpec`):**
```json
{
  "name": "string (required — e.g. 'infra' or 'infra/feature-vpc')",
  "vcs": {
    "host": "string",
    "auth_user": "string",
    "repo": "string (org/repo)",
    "branch": "string",
    "clone_url": "string (required)"
  },
  "pat_name": "string",
  "project_root": "string (required — final worktree path)",
  "repo_root": "string (container dir holding .bare/ and worktrees)",
  "bare_root": "string (path to .bare/ directory)",
  "worktree_name": "string ('default' or branch-derived)",
  "is_default": true | false,
  "owner": "string (required)"
}
```

**Response `data`:** full [`WorkspaceInstance`](types.md#workspaceinstance)

**Dual-mode provisioning:**

**Path A — default worktree (`is_default: true`, no existing bare clone):**
1. Creates `repo_root` directory
2. `git clone --bare <clone_url> <bare_root>`
3. Resolves default branch via `git -C <bare_root> symbolic-ref --short HEAD` (fallback: `main`)
4. `git -C <bare_root> worktree add ../default <default_branch>`
5. Resolves HEAD commit, state becomes `ready`

**Path B — non-default worktree, or bare clone already exists:**
1. If no bare clone exists, creates one via `git clone --bare` first
2. Fetches the requested branch: `git -C <bare_root> fetch origin <branch>`
3. Determines worktree path:
   - Default: `<repo_root>/default`
   - Non-default: `<repo_root>/.worktrees/<worktree_name>`
4. `git -C <bare_root> worktree add -f <path> <branch>`
5. Resolves HEAD commit, state becomes `ready`

**Common side effects:**
- All git commands run as `owner` via `su -` if different from current user
- Persists instance to state file (with exclusive file lock)
- Appends provisioning logs to `{log_dir}/{name}.log`
- **Idempotent:** if worktree already exists at `project_root`, returns existing instance as-is

**Errors:** `SPEC_INVALID`, `CLONE_FAILED`, `INTERNAL`

---

## `dscd workspace list`

List all workspaces with live-enriched status.

**Arguments:** none

**Response `data`:** array of [`WorkspaceInstance`](types.md#workspaceinstance) (sorted by name)

**Text output:** table with columns `NAME | STATE | REPO | BRANCH`

**Liveness enrichment (read-only, not persisted):**
- Checks `.git` (file or directory) exists at `{project_root}/.git`
- Checks credential file at `/home/{owner}/.config/dsc/credentials/git-credentials`
- Resolves `head_commit` via `git rev-parse HEAD`
- Derives `status` field (`SYNCED` / `MISSING` / `ERROR`)

**Errors:** `STATE_CORRUPT`

---

## `dscd workspace inspect <name>`

Inspect a single workspace with full diagnostics including worktree enumeration.

**Arguments:** exactly 1 — workspace name (e.g. `infra` or `infra/feature-vpc`)

**Response `data`:** [`WorkspaceInspectData`](types.md#workspaceinspectdata) — extends `WorkspaceInstance` with worktree stats

```json
{
  "spec": { "..." },
  "state": "ready",
  "status": "SYNCED",
  "head_commit": "a1b2c3d4e5f6",
  "credential_host": "github.com",
  "provisioned_at": "2026-05-18T14:00:00Z",
  "last_synced_at": "2026-05-20T10:30:00Z",
  "last_error": null,
  "bare_root": "/home/jperez/code/github.com/atlasops/infra/.bare",
  "worktree_count": 3,
  "worktrees": ["default", "feature-vpc", "bugfix-42"],
  "credential_fresh": true
}
```

**Text output:** field-by-field display including Name, Worktree, Repo, Branch, Project Root, Bare Root, State, Status, Head Commit, Credential status, Worktree Count, Worktrees list, Last Synced, Provisioned, Last Error.

**Worktree enumeration:** parsed from `git -C <bare_root> worktree list --porcelain` (run as owner). Returns basenames only.

**Errors:** `STATE_CORRUPT`, `NOT_FOUND`

---

## `dscd workspace deprovision <name>`

Remove a worktree (and optionally the entire bare clone).

**Arguments:** exactly 1 — workspace name

**Flags:**

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--force` | bool | `false` | Delete even if worktree has uncommitted changes |
| `--all` | bool | `false` | Remove all worktrees and the bare clone for this workspace |

**Response `data` (`DeprovisionResult`):**
```json
{
  "removed": ["infra/feature-vpc"],
  "message": "removed worktree feature-vpc"
}
```

**Side Effects:**
- Removes the worktree directory and its git metadata
- Removes the workspace entry from the state file (with lock)
- Appends log entry to `{log_dir}/{name}.log`

**Guards:**
- Cannot delete the `default` worktree without `--all` (returns `CANNOT_DELETE_DEFAULT`)
- Checks `git status --porcelain` for uncommitted changes (returns `WORKTREE_DIRTY` unless `--force`)
- With `--all`: removes every worktree, then the bare clone and `repo_root`

**Errors:** `NOT_FOUND`, `WORKTREE_DIRTY`, `CANNOT_DELETE_DEFAULT`, `INTERNAL`

---

## `dscd workspace prune <workspace>`

Batch-remove all clean non-default worktrees for a workspace.

**Arguments:** exactly 1 — workspace/repo name (e.g. `infra`)

**Response `data` (`PruneResult`):**
```json
{
  "pruned": ["infra/feature-vpc", "infra/bugfix-42"],
  "skipped": [
    { "name": "infra/wip-branch", "reason": "uncommitted changes" }
  ],
  "message": "pruned 2 worktrees, skipped 1"
}
```

**Side Effects:**
- Removes each clean non-default worktree directory and git metadata
- Removes pruned workspace entries from the state file (with lock)
- Appends log entries to `{log_dir}/{name}.log`

**Behavior:**
- Never prunes the `default` worktree
- Skips dirty worktrees (reason: `"uncommitted changes"`)
- Dirty check via `git status --porcelain`

**Errors:** `NOT_FOUND`, `STATE_CORRUPT`

---

## `dscd workspace sync`

Sync persisted state against filesystem reality. **This is the only read-heavy command that mutates state.**

**Arguments:** none

**Response `data` (`SyncReport`):**
```json
{
  "workspaces_checked": 5,
  "state_changes": ["ws1: pending -> ready", "ws3: ready -> error"],
  "errors": []
}
```

**State transition rules:**

| Current State | `.git` exists? | New State | `LastError` |
|---------------|----------------|-----------|-------------|
| `pending` / `error` | yes | `ready` | cleared |
| `ready` | no | `error` | `"clone directory missing after reboot"` |
| otherwise | — | unchanged | unchanged |

**Side Effects:**
- Sets `LastSyncedAt` on all instances
- Re-checks credentials and resolves HEAD commit
- Persists all changes atomically (with exclusive file lock)
- Writes per-workspace log entries

**Errors:** `STATE_CORRUPT`

---

## `dscd workspace logs <name>`

Tail provisioning logs for a workspace.

**Arguments:** exactly 1 — workspace name

**Flags:**

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--lines` | int | `50` | Number of lines to tail |
| `--follow` | bool | `false` | Stream new lines (polls every 1s) |

**Log format:** `[2006-01-02T15:04:05Z] [phase] message`

**Phases:** `provision`, `error`, `sync`, `deprovision`, `prune`

**Log path:** `{log_dir}/{name}.log`

If no log file exists, prints: `No log file for workspace 'X' (not yet provisioned)`

**Errors:** `NOT_FOUND`
