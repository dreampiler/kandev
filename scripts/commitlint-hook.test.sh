#!/usr/bin/env bash
# commitlint-hook.test.sh — the commit-msg hook delegates to commitlint when the
# workspace is installed, propagates its failure, and — when apps/node_modules
# is absent — runs a built-in conventional-commit check that still fails on a
# bad header rather than skipping. The fallback is what lets a
# front-end-untouched commit in a fresh fork worktree finish without an install.
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

printf 'feat: add a thing\n' > "$TMP_DIR/good"
printf 'not a conventional header\n' > "$TMP_DIR/bad-shape"
printf 'build: use an unlisted type\n' > "$TMP_DIR/bad-type"
printf 'feat: ends with a period.\n' > "$TMP_DIR/bad-period"
printf 'feat: %s\n' "$(printf 'x%.0s' {1..120})" > "$TMP_DIR/bad-long"

exit_status() {
	# Run the hook and print its exit status (never fails the test harness).
	local rc
	set +e
	(cd "$repo" && "$hook" "$1") >"$TMP_DIR/out" 2>&1
	rc=$?
	set -e
	printf '%s' "$rc"
}

# --- installed: delegate to commitlint -------------------------------------
mkdir -p "$repo/apps/node_modules/.bin"
: > "$repo/apps/node_modules/.bin/commitlint"

if [ "$(exit_status "$TMP_DIR/good")" = 0 ]; then
	pass "installed: hook exits 0"
else
	fail "installed: hook should exit 0: $(cat "$TMP_DIR/out")"
fi
if grep -q 'exec commitlint --edit' "$record"; then
	pass "installed: hook delegates to commitlint"
else
	fail "installed: hook did not delegate to commitlint: $(cat "$record")"
fi

: > "$record"
set +e
(cd "$repo" && COMMITLINT_HOOK_PNPM_EXIT=3 "$hook" "$TMP_DIR/good") >/dev/null 2>&1
rc=$?
set -e
if [ "$rc" -eq 3 ]; then
	pass "installed: hook propagates commitlint failure"
else
	fail "installed: hook did not preserve exit status 3 (got $rc)"
fi

# --- missing: built-in fallback still enforces ------------------------------
rm -rf "$repo/apps/node_modules"
: > "$record"

if [ "$(exit_status "$TMP_DIR/good")" = 0 ]; then
	pass "fallback: valid header passes"
else
	fail "fallback: valid header should pass: $(cat "$TMP_DIR/out")"
fi
if grep -q 'built-in check' "$TMP_DIR/out"; then
	pass "fallback: reports the built-in check"
else
	fail "fallback: did not report the built-in check: $(cat "$TMP_DIR/out")"
fi

if [ "$(exit_status "$TMP_DIR/bad-shape")" != 0 ]; then
	pass "fallback: non-conventional header fails"
else
	fail "fallback: non-conventional header must fail"
fi
if [ "$(exit_status "$TMP_DIR/bad-type")" != 0 ]; then
	pass "fallback: unlisted type fails"
else
	fail "fallback: unlisted type must fail"
fi
if [ "$(exit_status "$TMP_DIR/bad-period")" != 0 ]; then
	pass "fallback: trailing period fails"
else
	fail "fallback: trailing period must fail"
fi
if [ "$(exit_status "$TMP_DIR/bad-long")" != 0 ]; then
	pass "fallback: over-long header fails"
else
	fail "fallback: over-long header must fail"
fi

if [ -s "$record" ]; then
	fail "fallback: hook invoked pnpm without commitlint installed: $(cat "$record")"
else
	pass "fallback: hook does not invoke pnpm"
fi

if [ "$status" -eq 0 ]; then
	echo 'All commitlint hook checks passed.'
fi
exit "$status"
