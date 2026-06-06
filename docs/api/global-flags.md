# Global Flags & Response Envelope

## Global Flags

Available on all commands:

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--json` | bool | `false` | Output structured JSON envelope |
| `--state-path` | string | `/opt/dsc/var/dscd/state.json` | Path to state file |
| `--log-level` | string | `info` | Log level (debug, info, warn, error) |

## Response Envelope (all commands, `--json` mode)

Every command returns the same top-level envelope when `--json` is set:

```json
{
  "version": "v2",
  "command": "<command.path>",
  "status": "ok" | "error",
  "error": null | { "code": "<ERROR_CODE>", "message": "...", "detail": "..." },
  "data": <command-specific> | null
}
```

- Exit code `0` on success, `1` on any error.
- In text mode (default), output is human-readable tables or field-by-field displays.
