# State Store — Developer Notes

> Module: `github.com/atlascloudops/go-dscd`

## Architecture

The daemon uses a clean 3-layer architecture with a port-based store abstraction:

```
CLI (cobra)  -->  Domain (Provisioner, Syncer)  -->  StateStore (interface)
                                                           |
                                                      FileStore (impl)
```

The domain layer defines the `StateStore` interface (`domain/store_iface.go`). The infrastructure layer provides the `FileStore` implementation (`store/filestore.go`). The CLI layer wires them together via a lazy factory.

## StateStore Interface

```go
// domain/store_iface.go
type StateStore interface {
    Load() (map[string]*WorkspaceInstance, error)
    Save(instances map[string]*WorkspaceInstance) error
    WithLock(fn func() error) error
}
```

Design choices:

- **All-or-nothing persistence** — `Load`/`Save` operate on the entire workspace map, not individual entries. This keeps the interface minimal and avoids partial-write concerns.
- **Explicit locking** — `WithLock` provides advisory file locking for read-modify-write atomicity. Callers compose their own critical sections.
- **Domain-owned interface** — the port lives in `domain/`, not `store/`, so domain logic has zero import dependency on infrastructure.

## FileStore Implementation

Single JSON file, default path `/opt/dsc/var/dscd/state.json`.

### File Schema

```json
{
  "version": "v1",
  "updated_at": "2026-05-27T10:00:00Z",
  "workspaces": {
    "my-repo": { "spec": {...}, "events": [...], "status": "ready", ... },
    "my-repo/feature-x": { ... }
  }
}
```

### Persistence Characteristics

| Aspect | Detail |
|--------|--------|
| Format | JSON with `MarshalIndent` (human-readable) |
| Versioning | `"version": "v1"` envelope field |
| Write strategy | `os.WriteFile` (direct overwrite) |
| Locking | `syscall.Flock(LOCK_EX)` on `state.json.lock` — process-level advisory lock |
| Missing file | Returns empty map (no error) |
| Scope | Single file per pod — all workspaces in one map |

### File Layout on Pod

```
/opt/dsc/var/dscd/
├── state.json          # All workspace instances
├── state.json.lock     # Advisory flock file
├── ports.json          # IDE port allocations (PortAllocator)
├── logs/
│   └── <workspace>.log # Per-workspace provisioning logs
└── ide/
    └── <owner>--<worktree>.env  # Per-IDE systemd env files
```

## Domain Models

### WorkspaceSpec (Input)

Immutable input definition — what the client (frontend CLI) asks for:

- `Name` — logical name: `"infra"` or `"infra/feature-vpc"`
- `VCS` — clone URL, host, repo slug, branch
- `PatName` — PAT credential reference
- `ProjectRoot` — final worktree path on disk
- `RepoRoot` / `BareRoot` — container and bare clone paths
- `WorktreeName` — `"default"` or branch-derived
- `IsDefault` — true for bare clone + first worktree
- `Owner` — Linux username for `su -` operations
- `IDE` — optional `IDESpecConfig` (adapter name)

### WorkspaceInstance (Realized State)

What actually exists on the pod. Embeds `Spec` and carries:

- `Events []WorkspaceEventRecord` — append-only provisioning event stream
- `Status` — projected from events (never written directly)
- `IDE *IDEInstance` — optional, with its own independent event stream and status resolver
- `HeadCommit` — latest git HEAD (refreshed on provision/sync)
- `CredentialHost` — VCS host for credential lookup
- `ProvisionedAt` / `LastSyncedAt` / `LastError` — lifecycle timestamps

## Event-Sourced Status Projection

Status is **never set directly**. It is always projected from the event stream by a resolver:

```
Events:  clone_started -> clone_completed -> worktree_creating -> worktree_created
Status:  provisioning  -> provisioning    -> provisioning      -> ready
```

### Workspace Events

Lifecycle-affecting events (determine status):

| Event | Projected Status |
|-------|-----------------|
| `clone_started` | provisioning |
| `clone_completed` | provisioning |
| `worktree_creating` | provisioning |
| `worktree_created` | ready |
| `clone_detected` | ready |
| `provision_failed` | failed |

Informational events (skipped by resolver):

| Event | Purpose |
|-------|---------|
| `git_credentials_exist` | Credential file contains the VCS host |
| `hydrate_started` / `hydrate_completed` / `hydrate_skipped` | Fast-forward pull status |

`WorkspaceStatusResolver` walks events **backwards** to find the latest status-affecting event.

### IDE Events

IDE has its own event stream and resolver, enforced at compile time by distinct types:

| Event | Projected Status |
|-------|-----------------|
| `ide_started` | provisioning |
| `ide_ready` | ready |
| `ide_failed` | failed |
| `ide_stopped` | pending |

`IDEStatusResolver` uses **latest event wins** (simpler linear lifecycle).

### Status Values

Both resolvers project into the same `Status` type: `pending`, `provisioning`, `ready`, `failed`.

## State Mutation Pattern

All mutations follow load-under-lock, mutate, save-under-lock:

```go
store.WithLock(func() error {
    instances, err := store.Load()
    if err != nil {
        return err
    }
    // mutate instances[name]
    return store.Save(instances)
})
```

The `Provisioner` uses a `persistState` helper that encapsulates this for single-instance updates.

## Consumers

### Provisioner (`domain/provisioner.go`)

Owns the full workspace lifecycle:

- **Provision** — bare clone + default worktree, or add worktree from existing bare. Idempotent: if worktree already exists, refreshes state and returns.
- **Deprovision** — remove single non-default worktree (dirty guards) or remove all worktrees + bare clone.
- **Prune** — remove all clean non-default worktrees; skip dirty ones with reasons.
- **IDE management** — start/stop/health-check via `IDEAdapter` interface. IDE failures are non-fatal (events emitted, workspace status stays ready).

Each operation reads and writes state at lifecycle boundaries, appending events to build the status projection.

### WorkspaceSyncer (`domain/syncer.go`)

Periodic reconciliation that detects drift between persisted state and disk reality:

- Detects worktrees that appeared on disk (emits synthetic `clone_detected`)
- Detects worktrees that disappeared (emits `provision_failed`)
- Refreshes `HeadCommit` from git
- Health-checks IDE instances
- Updates `LastSyncedAt` timestamp

Runs as a single locked read-modify-write across all workspaces.

### CLI inspect/list

Read-only `Load()` for query responses. No locking needed for reads.

## CLI Composition

The CLI uses a lazy store factory so the `--state-path` flag is resolved before the store is instantiated:

```go
// root.go
storeFactory := func() *store.FileStore {
    return store.NewFileStore(statePath)
}
fs := &lazyStore{factory: storeFactory}
```

`lazyStore` itself implements `StateStore`, delegating all calls to the underlying `FileStore` on first access. This avoids constructing the store at command registration time when flags haven't been parsed yet.

## Contrast with Frontend Persistence

The frontend CLI has two separate stores for different concerns:

| Aspect | go-dscd FileStore | Frontend RuntimeStateStore | Frontend ConfigStore |
|--------|-------------------|---------------------------|---------------------|
| Format | JSON | JSON | TOML |
| Scope | Single file, all workspaces | Per-entity files (`<name>.json`) | Per-scope files (global, account) |
| Locking | `flock` advisory lock | None | None |
| Write strategy | `os.WriteFile` | temp + rename | temp + rename |
| Port abstraction | `StateStore` interface | Concrete class | Concrete class |
| Status model | Event-sourced projection | N/A (pods use EC2 state) | N/A (config, not runtime) |

The daemon store is intentionally simpler — one file, one lock, one map. The event-sourced status projection is the sophisticated part, living entirely in the domain layer with no store awareness.
