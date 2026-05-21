# Quick Reference

```bash
# Status
dscd status
dscd --json status

# Provision — default worktree (bare clone + first worktree)
dscd workspace provision '{"name":"infra","vcs":{"host":"github.com","repo":"atlasops/infra","clone_url":"https://github.com/atlasops/infra.git","branch":"main"},"project_root":"/home/dev/code/github.com/atlasops/infra/default","repo_root":"/home/dev/code/github.com/atlasops/infra","bare_root":"/home/dev/code/github.com/atlasops/infra/.bare","worktree_name":"default","is_default":true,"owner":"dev"}'

# Provision — additional worktree on existing bare clone
dscd workspace provision '{"name":"infra/feature-vpc","vcs":{"host":"github.com","repo":"atlasops/infra","clone_url":"https://github.com/atlasops/infra.git","branch":"feature-vpc"},"project_root":"/home/dev/code/github.com/atlasops/infra/.worktrees/feature-vpc","repo_root":"/home/dev/code/github.com/atlasops/infra","bare_root":"/home/dev/code/github.com/atlasops/infra/.bare","worktree_name":"feature-vpc","is_default":false,"owner":"dev"}'

# List
dscd workspace list
dscd --json workspace list

# Inspect
dscd workspace inspect infra
dscd --json workspace inspect infra/feature-vpc

# Sync
dscd workspace sync
dscd --json workspace sync

# Deprovision — single worktree
dscd workspace deprovision infra/feature-vpc
dscd workspace deprovision infra/feature-vpc --force   # ignore dirty state

# Deprovision — entire workspace (all worktrees + bare clone)
dscd workspace deprovision infra --all

# Prune — remove all clean non-default worktrees
dscd workspace prune infra
dscd --json workspace prune infra

# Logs
dscd workspace logs infra                       # last 50 lines
dscd workspace logs --lines 100 infra           # last 100 lines
dscd workspace logs --follow infra              # stream mode (Ctrl+C to exit)
dscd workspace logs --lines 10 --follow infra   # last 10 + stream
```
