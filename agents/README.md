# agents/ — canonical agentic-ops role-skill definitions

Version-controlled source of truth for the Agentic Operating Model role-skills
(design: [`agentic-ops-design.md`](../_bmad-output/implementation-artifacts/agentic-ops-design.md)).

The Claude Code harness discovers skills under `.claude/skills/`, but `.claude/` is gitignored — so the
canonical `SKILL.md` files live **here**, in-repo, and are installed into `.claude/skills/` with:

```
make install-skills
```

**Edit the copies under `agents/`**, then re-run `make install-skills`. Do not edit
`.claude/skills/<role>/` directly — those are install artifacts and get overwritten.

## Current role-skills

| Role | What it does |
|---|---|
| `lamplighter/` | Observability watch — read Health KV → classify anomalies → surface remediation candidates (L0/L1). |
| `steward/` | Winston's self-driving dispatch loop — sense board + signals → activate the owning role (L1) → admit/commit (L2). |

The bmad tooling skills stay local (under `.claude/skills/` and `.agents/`) and are intentionally not
tracked here — this directory is only the agentic-ops roles.
