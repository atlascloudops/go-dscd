# Core Types

## WorkspaceInstance

The primary object returned by `workspace provision`, `workspace list`, and `workspace inspect`.

```json
{
  "spec": {
    "name": "string (e.g. 'infra' or 'infra/feature-vpc')",
    "vcs": {
      "host": "string",
      "auth_user": "string",
      "repo": "string (org/repo)",
      "branch": "string",
      "clone_url": "string"
    },
    "pat_name": "string",
    "project_root": "string (final worktree path)",
    "repo_root": "string (container dir for .bare/ and worktrees)",
    "bare_root": "string (path to .bare/ directory)",
    "worktree_name": "string ('default' or branch-derived)",
    "is_default": true | false,
    "owner": "string"
  },
  "state": "pending" | "provisioning" | "ready" | "error",
  "status": "SYNCED" | "MISSING" | "ERROR",
  "head_commit": "abc123...",
  "credential_host": "github.com",
  "provisioned_at": "2026-05-20T12:00:00Z",
  "last_error": "clone directory missing",
  "last_synced_at": "2026-05-20T12:05:00Z"
}
```

Internal fields `CloneExists` and `CredentialFresh` are excluded from JSON output (`json:"-"`).

## WorkspaceInspectData

Extended response returned by `workspace inspect`. Embeds `WorkspaceInstance` and adds worktree diagnostics.

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

Worktrees are enumerated via `git -C <bare_root> worktree list --porcelain` and returned as basenames only.

## WorkspaceState Enum

| Value | Meaning |
|-------|---------|
| `pending` | Initial state, awaiting provisioning |
| `provisioning` | Clone operation in progress |
| `ready` | Clone exists and is ready for use |
| `error` | Clone failed or missing after reboot |

## Status Field (derived at read time)

| Value | Meaning |
|-------|---------|
| `SYNCED` | Clone/worktree exists, state is `ready` |
| `MISSING` | Clone directory not found on disk |
| `ERROR` | State is `error` or liveness check failed |

## DeprovisionResult

Returned by `workspace deprovision`.

```json
{
  "removed": ["infra/feature-vpc"],
  "message": "removed worktree feature-vpc"
}
```

## PruneResult

Returned by `workspace prune`.

```json
{
  "pruned": ["infra/feature-vpc", "infra/bugfix-42"],
  "skipped": [
    { "name": "infra/wip-branch", "reason": "uncommitted changes" }
  ],
  "message": "pruned 2 worktrees, skipped 1"
}
```

### PruneSkipped

```json
{
  "name": "string",
  "reason": "string"
}
```

## SyncReport

Returned by `workspace sync`.

```json
{
  "workspaces_checked": 5,
  "state_changes": ["ws1: pending -> ready", "ws3: ready -> error"],
  "errors": []
}
```

## Filesystem Layout

```
~/code/github.com/atlasops/infra/       # repo_root
├── .bare/                               # bare_root (bare clone)
├── default/                             # default worktree (project_root for default)
│   └── .git                             # file: "gitdir: ../.bare/worktrees/default"
└── .worktrees/
    └── feature-vpc/                     # non-default worktree (project_root)
        └── .git                         # file: "gitdir: ../../.bare/worktrees/feature-vpc"
```

## Workspace Identity

- Logical name: `<repo-name>` for the default worktree, `<repo-name>/<worktree-name>` for branches
- `<repo-name>` alone is shorthand for `<repo-name>/default`
- Filesystem paths use `.worktrees/`; CLI identifiers use `/`

## State File (`state.json`)

Persisted at `--state-path` (default `/opt/dsc/var/dscd/state.json`).

```json
{
  "version": "v1",
  "updated_at": "2026-05-20T12:00:00Z",
  "workspaces": {
    "<name>": { /* WorkspaceInstance */ }
  }
}
```

- File permissions: `0664`
- Lock file: `{state_path}.lock` (POSIX `flock` exclusive)
- Missing state file is treated as empty (not an error)
- Lock wraps all load-modify-save operations atomically

## Process Logging

Operational diagnostics are emitted via `log/slog` as structured JSON to stdout, captured by journald through the systemd unit's `StandardOutput=journal`. Log level is configurable via `--log-level` (default: `info`).
