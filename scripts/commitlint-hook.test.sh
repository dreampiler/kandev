#!/usr/bin/env bash
# commitlint-hook.test.sh — the commit-msg hook must delegate to commitlint when
# the workspace is installed, propagate its failure, and skip cleanly (exit 0,
# one-line notice, no pnpm call) when apps/node_modules is absent. The skip is
# what keeps a front-end-untouched commit in a fresh fork worktree from waiting
# on an install.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT

status=0
fail() { printf 'FAIL  %s\n' "$*"; status=1; }
pass() { printf 'ok    %s\n' "$*"; }

repo="$TMP_DIR/repo"
mkdir -p "$repo/scripts" "$repo/apps"
cp "$ROOT_DIR/scripts/commitlint-hook" "$repo/scripts/commitlint-hook"
hook="$repo/scripts/commitlint-hook"
printf 'feat: example\n' > "$TMP_DIR/msg"

# A stub pnpm earlier on PATH records its arguments, so the "installed" path is
# observable without a real dependency tree.
stub_bin="$TMP_DIR/bin"
mkdir -p "$stub_bin"
cat > "$stub_bin/pnpm" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' "$*" >>"$COMMITLINT_HOOK_RECORD"
exit "${COMMITLINT_HOOK_PNPM_EXIT:-0}"
EOF
chmod +x "$stub_bin/pnpm"
record="$TMP_DIR/record"
: > "$record"
export COMMITLINT_HOOK_RECORD="$record"
export PATH="$stub_bin:$PATH"

mkdir -p "$repo/apps/node_modules/.bin"
: > "$repo/apps/node_modules/.bin/commitlint"

if out="$(cd "$repo" && "$hook" "$TMP_DIR/msg" 2>&1)"; then
	pass "installed: hook exits 0"
else
	fail "installed: hook should exit 0: $out"
fi
if grep -q 'exec commitlint --edit' "$record"; then
	pass "installed: hook delegates to commitlint"
else
	fail "installed: hook did not delegate to commitlint: $(cat "$record")"
fi

: > "$record"
set +e
out="$(cd "$repo" && COMMITLINT_HOOK_PNPM_EXIT=3 "$hook" "$TMP_DIR/msg" 2>&1)"
rc=$?
set -e
if [ "$rc" -eq 3 ]; then
	pass "installed: hook propagates commitlint failure"
else
	fail "installed: hook did not preserve exit status 3 (got $rc): $out"
fi

rm -rf "$repo/apps/node_modules"
: > "$record"
if out="$(cd "$repo" && "$hook" "$TMP_DIR/msg" 2>&1)"; then
	pass "missing: hook exits 0"
else
	fail "missing: hook should exit 0: $out"
fi
if printf '%s' "$out" | grep -q 'commitlint skipped'; then
	pass "missing: hook prints a skip notice"
else
	fail "missing: hook did not print a skip notice: $out"
fi
if [ -s "$record" ]; then
	fail "missing: hook invoked pnpm without commitlint installed: $(cat "$record")"
else
	pass "missing: hook does not invoke pnpm"
fi

if [ "$status" -eq 0 ]; then
	echo 'All commitlint hook checks passed.'
fi
exit "$status"
