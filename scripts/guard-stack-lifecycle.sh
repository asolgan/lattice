#!/usr/bin/env bash
#
# PreToolUse(Bash) backstop: refuse `make up*` / `make down` when the effective
# working directory is a git worktree rather than the main checkout.
#
# Why this exists alongside the Makefile's assert-main-checkout guard: the
# Makefile guard only protects worktrees whose checkout already contains the
# guarded Makefile. A worktree cut before the guard landed (or a stale one) still
# has an un-guarded Makefile and can recreate the pinned lattice-nats container —
# docker-compose.yml mounts deploy/nats-server.conf by a RELATIVE path, so a
# worktree's differently-pathed mount source diverges from the running container
# and Compose destroys + recreates it, wiping the ephemeral JetStream (all of
# Core KV). This hook fires session-wide regardless of which Makefile is on disk.
#
# Reads the PreToolUse hook payload on stdin; emits a deny decision (or nothing).
# Fail-open: any ambiguity (not a make up/down, not a git tree, unresolvable dir)
# exits 0 with no output so normal commands are never blocked.

input=$(cat)
cmd=$(printf '%s' "$input" | jq -r '.tool_input.command // ""' 2>/dev/null) || exit 0
[ -n "$cmd" ] || exit 0

# Explicit operator override, mirroring the Makefile's LATTICE_ALLOW_ANYWHERE=1.
case "$cmd" in *LATTICE_ALLOW_ANYWHERE=1*) exit 0 ;; esac

# Only `make` invoking an `up`/`up-*`/`down` target is dangerous. Fast-exit
# everything else (this runs on every Bash call).
printf '%s' "$cmd" | grep -Eq '(^|[^[:alnum:]_-])make[[:space:]]+(up|down)([[:space:]]|-|$|;|&)' || exit 0

# Effective directory: a leading `cd <path>` (the `cd <worktree> && make ...`
# pattern that caused the 2026-07-13 wipe) wins; otherwise the session cwd.
sess_cwd=$(printf '%s' "$input" | jq -r '.cwd // ""' 2>/dev/null)
[ -n "$sess_cwd" ] || sess_cwd=$(pwd)
eff=$(printf '%s' "$cmd" | sed -n 's/^[[:space:]]*cd[[:space:]]\{1,\}\([^;&|]*\).*/\1/p' | head -1 | sed 's/[[:space:]]*$//')
[ -n "$eff" ] || eff="$sess_cwd"
# Strip surrounding quotes a `cd "path"` may carry.
eff=${eff%\"}; eff=${eff#\"}; eff=${eff%\'}; eff=${eff#\'}

# Resolve the main checkout root from the effective dir (fail-open on anything odd).
eff_abs=$(cd "$eff" 2>/dev/null && pwd -P) || exit 0
gitcommon=$(cd "$eff_abs" && git rev-parse --git-common-dir 2>/dev/null) || exit 0
[ -n "$gitcommon" ] || exit 0
common=$(cd "$eff_abs" && cd "$gitcommon" 2>/dev/null && pwd -P) || exit 0
main_root=$(dirname "$common")

[ "$eff_abs" = "$main_root" ] && exit 0

reason="Refusing \`make up/down\` from $eff_abs — that is a git worktree, not the main checkout ($main_root). docker compose up/down from a worktree recreates the pinned lattice-nats container and WIPES all JetStream / Core-KV data (root cause of the 2026-07-13 data loss). Run stack lifecycle from $main_root: reuse the already-running stack, or use make refresh-<vertical> / start the app from main. Deliberate override: prefix LATTICE_ALLOW_ANYWHERE=1."

jq -cn --arg r "$reason" '{hookSpecificOutput:{hookEventName:"PreToolUse",permissionDecision:"deny",permissionDecisionReason:$r}}'
exit 0
