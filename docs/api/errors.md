# Error Codes & Handling

## Error Code Reference

| Code | Meaning | Commands |
|------|---------|----------|
| `SPEC_INVALID` | Missing/invalid required fields in spec JSON | `provision` |
| `CLONE_FAILED` | `git clone --bare` or `git worktree add` failed | `provision` |
| `STATE_CORRUPT` | State file exists but contains invalid JSON | `status`, `list`, `inspect`, `sync`, `prune` |
| `NOT_FOUND` | Workspace name not in state | `inspect`, `logs`, `deprovision`, `prune` |
| `ALREADY_EXISTS` | Workspace name collision (reserved, not actively used) | — |
| `LOCK_FAILED` | Could not acquire state file lock | any mutating command |
| `WORKTREE_DIRTY` | Worktree has uncommitted changes | `deprovision` |
| `CANNOT_DELETE_DEFAULT` | Attempt to delete default worktree without `--all` | `deprovision` |
| `INTERNAL` | Unexpected / unclassified error | any |

## Error Response Structure

When `--json` is set and an error occurs:

```json
{
  "version": "v2",
  "command": "workspace.provision",
  "status": "error",
  "error": {
    "code": "SPEC_INVALID",
    "message": "missing required field: project_root",
    "detail": ""
  },
  "data": null
}
```

Exit code is always `1` for errors.

## Handling Patterns

### Provision errors
- Spec validation failures return `SPEC_INVALID` before any side effects occur.
- Clone failures return `CLONE_FAILED` with git stderr captured in `detail`. The workspace is still persisted to state with `state: "error"` and `last_error` populated.

### Deprovision guards
- Attempting to delete the `default` worktree without `--all` returns `CANNOT_DELETE_DEFAULT`.
- Attempting to delete a worktree with uncommitted changes returns `WORKTREE_DIRTY` unless `--force` is set. Dirty state is checked via `git status --porcelain`.

### Prune skips
- `workspace prune` does **not** error on dirty worktrees — it skips them and reports in the `skipped` array with a reason. It only errors on `NOT_FOUND` or `STATE_CORRUPT`.

### State corruption
- If the state file exists but contains invalid JSON, commands return `STATE_CORRUPT` with the parse error in `detail`.
- No auto-repair is attempted; manual investigation is required.

### Missing state file
- A missing state file is **not** an error — it is treated as an empty workspace map. Commands like `list` return an empty array, `status` reports `workspace_count: 0`.

### Missing clone directory
- Detected during liveness enrichment (`list`, `inspect`) and sync (`sync`).
- On `sync`, a `ready` workspace with a missing `.git` directory transitions to `error` with `last_error: "clone directory missing after reboot"`.
- On `list`/`inspect`, the `status` field is derived as `MISSING` but state is **not** persisted.

### Lock failures
- State file locking uses POSIX `flock` with `LOCK_EX` (exclusive).
- Lock blocks indefinitely (no timeout). If the lock cannot be acquired due to a system error, `LOCK_FAILED` is returned.

### Missing credential file
- Detected as `CredentialFresh: false` during enrichment. Not treated as fatal — the `status` field still derives from clone existence.
