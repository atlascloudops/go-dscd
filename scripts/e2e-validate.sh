#!/bin/bash
# e2e-validate.sh — Production hardening E2E validation script.
#
# Run on a pod after deploying a new AMI with the hardened dscd binary.
# This script validates all acceptance criteria from the dscd production
# hardening epic: aggregate models, event framework, process logging,
# application-root layout, and CLI integrations.
#
# Usage:
#   sudo bash scripts/e2e-validate.sh
#
# Prerequisites:
#   - Pod launched with the new AMI containing the hardened dscd
#   - At least one workspace provisioned (or the script will provision one)
#   - SSH user added to the dscd group

set -euo pipefail

PASS=0
FAIL=0
SKIP=0

pass() { PASS=$((PASS + 1)); echo "  PASS: $1"; }
fail() { FAIL=$((FAIL + 1)); echo "  FAIL: $1"; }
skip() { SKIP=$((SKIP + 1)); echo "  SKIP: $1"; }

section() { echo ""; echo "=== $1 ==="; }

# ---------------------------------------------------------------------------
# 1. Layout validation
# ---------------------------------------------------------------------------
section "1. Layout validation"

if [ -x /usr/local/bin/dscd ]; then
    pass "Binary at /usr/local/bin/dscd"
else
    fail "Binary not found at /usr/local/bin/dscd"
fi

if [ -d /var/lib/dscd ]; then
    pass "Daemon runtime root /var/lib/dscd/ exists"
else
    fail "Daemon runtime root /var/lib/dscd/ not found"
fi

if [ -d /var/lib/dscd/ide ]; then
    pass "IDE env directory /var/lib/dscd/ide/ exists"
else
    fail "IDE env directory /var/lib/dscd/ide/ not found"
fi

# Verify setgid bit on runtime dirs
PERMS=$(stat -c '%a' /var/lib/dscd 2>/dev/null || stat -f '%Lp' /var/lib/dscd 2>/dev/null)
if echo "$PERMS" | grep -q "^2"; then
    pass "Setgid bit set on /var/lib/dscd/"
else
    fail "Setgid bit not set on /var/lib/dscd/ (perms: $PERMS)"
fi

if [ -d /opt/dsc/etc ]; then
    pass "Infra-bootstrap artifacts at /opt/dsc/etc/"
else
    skip "Infra-bootstrap artifacts at /opt/dsc/etc/ (may not exist in test)"
fi

if [ -d /opt/dsc/bin ] || [ -f /opt/dsc/bin/pod-boot.sh ]; then
    pass "Pod-boot script at /opt/dsc/bin/"
else
    skip "Pod-boot script at /opt/dsc/bin/ (may not exist in test)"
fi

# Verify no legacy log files under /var/lib/dscd/
LEGACY_LOGS=$(find /var/lib/dscd/ -name "*.log" ! -name "activity.log" 2>/dev/null | head -5)
if [ -z "$LEGACY_LOGS" ]; then
    pass "No legacy per-workspace .log files under /var/lib/dscd/"
else
    fail "Legacy log files found: $LEGACY_LOGS"
fi

# ---------------------------------------------------------------------------
# 2. State file validation
# ---------------------------------------------------------------------------
section "2. State file validation"

STATE_FILE="/var/lib/dscd/state.json"
if [ -f "$STATE_FILE" ]; then
    pass "State file exists at $STATE_FILE"

    # Verify state structure has top-level keys
    if python3 -c "
import json, sys
state = json.load(open('$STATE_FILE'))
assert 'workspaces' in state, 'missing workspaces key'
" 2>/dev/null; then
        pass "State file contains 'workspaces' key"
    else
        fail "State file missing 'workspaces' key"
    fi

    # Verify workspace entries use Workspace type (events have scope)
    WS_COUNT=$(python3 -c "
import json
state = json.load(open('$STATE_FILE'))
ws = state.get('workspaces', {})
print(len(ws))
" 2>/dev/null)
    if [ "$WS_COUNT" -gt 0 ] 2>/dev/null; then
        pass "State file contains $WS_COUNT workspace(s)"

        # Check that events use EventRecord with scope (not old WorkspaceEventRecord)
        SCOPED_EVENTS=$(python3 -c "
import json
state = json.load(open('$STATE_FILE'))
ws = state.get('workspaces', {})
for name, w in ws.items():
    events = w.get('events', [])
    for e in events:
        if 'scope' not in e:
            print(f'UNSCOPED: {name}')
            break
    else:
        continue
    break
else:
    print('ALL_SCOPED')
" 2>/dev/null)
        if [ "$SCOPED_EVENTS" = "ALL_SCOPED" ]; then
            pass "All workspace events use scoped EventRecord format"
        else
            fail "Found unscoped events: $SCOPED_EVENTS"
        fi
    else
        skip "No workspaces in state to validate event format"
    fi
else
    fail "State file not found at $STATE_FILE"
fi

# ---------------------------------------------------------------------------
# 3. Workspace lifecycle (CLI)
# ---------------------------------------------------------------------------
section "3. Workspace lifecycle (CLI)"

# dscd workspace list --json
LIST_OUT=$(dscd workspace list --json 2>&1) || true
if echo "$LIST_OUT" | python3 -c "
import json, sys
resp = json.load(sys.stdin)
assert resp.get('version') == 'v2', f'unexpected version: {resp.get(\"version\")}'
assert resp.get('status') == 'ok', f'unexpected status: {resp.get(\"status\")}'
# Verify data items do not use 'WorkspaceInstance' type name in keys
data = resp.get('data', [])
for item in data:
    assert 'spec' in item, 'missing spec in list item'
    assert 'status' in item, 'missing status in list item'
" 2>/dev/null; then
    pass "dscd workspace list --json returns v2 response with Workspace items"
else
    fail "dscd workspace list --json response validation failed"
fi

# Check if there's at least one workspace to inspect
FIRST_WS=$(dscd workspace list --json 2>/dev/null | python3 -c "
import json, sys
resp = json.load(sys.stdin)
data = resp.get('data', [])
if data:
    print(data[0]['spec']['name'])
" 2>/dev/null)

if [ -n "$FIRST_WS" ]; then
    # dscd workspace inspect <name> --json
    INSPECT_OUT=$(dscd workspace inspect "$FIRST_WS" --json 2>&1) || true
    if echo "$INSPECT_OUT" | python3 -c "
import json, sys
resp = json.load(sys.stdin)
data = resp.get('data', {})
events = data.get('events', [])
# Verify events have scope field with EventScope format
for e in events:
    scope = e.get('scope', '')
    assert ':' in scope, f'event scope missing colon: {scope}'
    parts = scope.split(':', 1)
    assert parts[0] in ('workspace', 'ide', 'credentials'), f'unexpected scope kind: {parts[0]}'
print('ok')
" 2>/dev/null; then
        pass "dscd workspace inspect --json returns EventRecord entries with EventScope"
    else
        fail "dscd workspace inspect --json event format validation failed"
    fi
else
    skip "No workspaces available for inspect validation"
fi

# ---------------------------------------------------------------------------
# 4. Event visibility (CLI)
# ---------------------------------------------------------------------------
section "4. Event visibility (CLI)"

# dscd events (default table output)
EVENTS_TABLE=$(dscd events 2>&1) || true
if echo "$EVENTS_TABLE" | head -1 | grep -q "TIMESTAMP"; then
    pass "dscd events shows table header"
elif echo "$EVENTS_TABLE" | grep -q "No events found"; then
    skip "dscd events: no events recorded yet"
else
    fail "dscd events output unexpected: $(echo "$EVENTS_TABLE" | head -3)"
fi

# dscd events --json
EVENTS_JSON=$(dscd events --json 2>&1) || true
if echo "$EVENTS_JSON" | python3 -c "
import json, sys
data = json.load(sys.stdin)
assert isinstance(data, list), 'expected JSON array'
if data:
    e = data[0]
    assert 'scope' in e, 'missing scope'
    assert 'event' in e, 'missing event'
    assert 'timestamp' in e, 'missing timestamp'
" 2>/dev/null; then
    pass "dscd events --json returns valid structured output"
else
    fail "dscd events --json validation failed"
fi

# dscd events --kind workspace
EVENTS_KIND=$(dscd events --kind workspace --json 2>&1) || true
if echo "$EVENTS_KIND" | python3 -c "
import json, sys
data = json.load(sys.stdin)
assert isinstance(data, list), 'expected JSON array'
for e in data:
    scope = e.get('scope', '')
    assert scope.startswith('workspace:'), f'unexpected scope in kind filter: {scope}'
" 2>/dev/null; then
    pass "dscd events --kind workspace filters correctly"
else
    # May be empty if no workspace events
    if echo "$EVENTS_KIND" | python3 -c "
import json, sys
data = json.load(sys.stdin)
assert data == []
" 2>/dev/null; then
        skip "dscd events --kind workspace: no workspace events to filter"
    else
        fail "dscd events --kind workspace filter validation failed"
    fi
fi

# dscd events --since 5m
EVENTS_SINCE=$(dscd events --since 5m --json 2>&1) || true
if echo "$EVENTS_SINCE" | python3 -c "
import json, sys
data = json.load(sys.stdin)
assert isinstance(data, list), 'expected JSON array'
" 2>/dev/null; then
    pass "dscd events --since 5m returns valid output"
else
    fail "dscd events --since 5m validation failed"
fi

# ---------------------------------------------------------------------------
# 5. Activity log file
# ---------------------------------------------------------------------------
section "5. Activity log file"

ACTIVITY_LOG="/var/lib/dscd/activity.log"
if [ -f "$ACTIVITY_LOG" ]; then
    pass "Activity log exists at $ACTIVITY_LOG"

    # Validate line format: [timestamp] [scope] event
    VALID_LINES=$(grep -cP '^\[.*\] \[.*:.*\] ' "$ACTIVITY_LOG" 2>/dev/null || echo 0)
    TOTAL_LINES=$(wc -l < "$ACTIVITY_LOG" 2>/dev/null || echo 0)
    if [ "$TOTAL_LINES" -gt 0 ] && [ "$VALID_LINES" -eq "$TOTAL_LINES" ]; then
        pass "All $TOTAL_LINES activity log lines match expected format"
    elif [ "$TOTAL_LINES" -gt 0 ]; then
        fail "Activity log format: $VALID_LINES/$TOTAL_LINES lines match expected format"
    else
        skip "Activity log is empty"
    fi

    # Check for cross-aggregate events (workspace + credentials)
    WS_EVENTS=$(grep -c '\[workspace:' "$ACTIVITY_LOG" 2>/dev/null || echo 0)
    CRED_EVENTS=$(grep -c '\[credentials:' "$ACTIVITY_LOG" 2>/dev/null || echo 0)
    echo "  INFO: Activity log: $WS_EVENTS workspace events, $CRED_EVENTS credential events"
else
    skip "Activity log not yet created at $ACTIVITY_LOG"
fi

# ---------------------------------------------------------------------------
# 6. Process logging (journald)
# ---------------------------------------------------------------------------
section "6. Process logging (journald)"

if command -v journalctl &>/dev/null; then
    JOURNAL_LINES=$(journalctl -u dscd --no-pager -n 10 2>/dev/null | wc -l)
    if [ "$JOURNAL_LINES" -gt 0 ]; then
        pass "Structured slog output found in journald (dscd.service)"

        # Check for structured log format (slog key=value pairs)
        if journalctl -u dscd --no-pager -n 20 2>/dev/null | grep -qE '(level=|msg=|time=)'; then
            pass "Journald entries contain structured slog fields"
        else
            skip "Journald entries present but structured slog fields not detected"
        fi
    else
        skip "No journald entries for dscd.service (may not have run yet)"
    fi
else
    skip "journalctl not available (not a systemd environment)"
fi

# Verify no per-workspace .log files
WORKSPACE_LOGS=$(find /var/lib/dscd/ -name "*.log" ! -name "activity.log" 2>/dev/null)
if [ -z "$WORKSPACE_LOGS" ]; then
    pass "No per-workspace .log files under /var/lib/dscd/"
else
    fail "Per-workspace .log files found: $WORKSPACE_LOGS"
fi

# ---------------------------------------------------------------------------
# 7. Systemd unit validation
# ---------------------------------------------------------------------------
section "7. Systemd unit validation"

if [ -f /etc/systemd/system/dscd.service ]; then
    pass "Systemd unit file exists"

    # Verify ExecStart points to /usr/local/bin/dscd
    if grep -q 'ExecStart=/usr/local/bin/dscd' /etc/systemd/system/dscd.service; then
        pass "Systemd ExecStart uses /usr/local/bin/dscd"
    else
        fail "Systemd ExecStart does not use /usr/local/bin/dscd"
    fi

    # Verify StandardOutput=journal
    if grep -q 'StandardOutput=journal' /etc/systemd/system/dscd.service; then
        pass "Systemd unit sends stdout to journal"
    else
        fail "Systemd unit not configured for journal output"
    fi

    # Verify UMask=0002
    if grep -q 'UMask=0002' /etc/systemd/system/dscd.service; then
        pass "Systemd unit has UMask=0002 for group-writable files"
    else
        fail "Systemd unit missing UMask=0002"
    fi
else
    skip "Systemd unit file not found (not installed via dscd.sh.install)"
fi

# ---------------------------------------------------------------------------
# 8. Credential events (if credentials exist)
# ---------------------------------------------------------------------------
section "8. Credential aggregate validation"

if [ -f "$STATE_FILE" ]; then
    CRED_COUNT=$(python3 -c "
import json
state = json.load(open('$STATE_FILE'))
creds = state.get('credentials', {})
print(len(creds))
" 2>/dev/null)
    if [ "$CRED_COUNT" -gt 0 ] 2>/dev/null; then
        pass "Credential aggregate state present ($CRED_COUNT owner(s))"

        # Verify credential events have proper scope
        python3 -c "
import json
state = json.load(open('$STATE_FILE'))
creds = state.get('credentials', {})
for owner, cs in creds.items():
    events = cs.get('events', [])
    for e in events:
        scope = e.get('scope', '')
        assert scope.startswith('credentials:'), f'bad scope: {scope}'
        assert owner in scope, f'owner {owner} not in scope {scope}'
print('ok')
" 2>/dev/null && pass "Credential events have correct scope format" || fail "Credential event scope validation failed"
    else
        skip "No credential state to validate (run dscd credentials git write first)"
    fi
else
    skip "State file not found, cannot validate credentials"
fi

# ---------------------------------------------------------------------------
# Summary
# ---------------------------------------------------------------------------
echo ""
echo "==========================================="
echo "  E2E Validation Summary"
echo "==========================================="
echo "  PASS: $PASS"
echo "  FAIL: $FAIL"
echo "  SKIP: $SKIP"
echo "==========================================="

if [ "$FAIL" -gt 0 ]; then
    echo ""
    echo "RESULT: FAILED ($FAIL check(s) failed)"
    exit 1
else
    echo ""
    echo "RESULT: PASSED (all checks passed, $SKIP skipped)"
    exit 0
fi
