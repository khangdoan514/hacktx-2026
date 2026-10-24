# Current decisions and precedence

Status: current user-approved planning direction. Implementation status is tracked in [repository-status.md](repository-status.md).

## Confirmed decisions

| Decision | Consequence |
| --- | --- |
| Product name is NMC3; company is NMC2 | Do not present NMC3 as a separate customer company or generic multi-company SaaS |
| NMC2 offers GPUaaS | Hardware capacity is business context; this scope is not a customer GPU marketplace |
| Hardware and Software are the two divisions | Seed these divisions; AI team belongs to Software |
| PR means pull request/code review | Use engineering-review scenarios rather than public-relations scenarios |
| Employees can hold multiple business functions | PM/BA labels are not mutually exclusive and do not determine authorization alone |
| Internal users only | Remove public company/workspace signup and partner-account access |
| All NMC3 custom backend code should be Go | Replace the Python business API and Convex functions, retaining Next.js as frontend |
| Supabase remains selected | Auth, Postgres and private Storage remain managed services |
| Follow main's OpenCode direction | Do not restore the deleted custom execution engine |
| Scripted dummy execution adapter meanwhile | Real persisted application workflows can proceed without live provider calls |
| Dummy labels belong in code, not website badges | Internal provenance remains explicit; user-facing claims remain truthful |
| CLI/SDK for developers; website for less technical users | Both use the same API, definitions, permissions and history |
| Evaluation, triggers, budget, guardrails, prompts and observation are core | Treat them as versioned backend capabilities, not decoration on request forms |
| Dashboard work is frozen | No dashboard screen, adapter, proposal, wiring or redesign work in this phase |
| This publication goes directly to main | Only this documentation folder is authorized for this publication; no code merge/deploy is implied |

## Precedence within this pack

1. The latest direct user instruction controls.
2. Current decision/scope documents and current prompts describe this workstream.
3. Current main supplies code reality and the approved OpenCode execution direction.
4. Existing agent contracts remain canonical until deliberately migrated/versioned with their owners.
5. Earlier plans and pasted prompts in `archive/` explain history; conflicting instructions in them are superseded.

Repository documents are design inputs, not proof that capabilities exist. In particular, the current agent README and OpenCode adoption document contain older setup/revert passages inconsistent with the history-only entrypoint. Resolve those inconsistencies in future implementation work rather than inferring a hidden working engine.

## Decision history

The initial request was a comprehensive repository/product/backend gap audit excluding front-page and visual design work. It evolved to Supabase with future agent integration, initially without dummy agents. Later clarifications made the product exclusively internal to NMC2, requested an all-Go backend and code-labeled dummy agents, froze dashboard work, and introduced the CLI/SDK/website split. A subsequent check found that main had removed the custom runtime for OpenCode. The user then explicitly chose OpenCode with an interim scripted adapter.

This history explains why archived plans disagree. Do not average those plans together. For example, “no agent implementation” and “build a new custom Go model engine” are both obsolete instructions for this phase.

## Proposed details, not established company policy

The exact permission matrix, root-module migration layout, retention periods, approval thresholds, budget amounts, team names beyond the confirmed divisions/AI team, IdP/SSO configuration, and CLI/SDK release language are implementation/product decisions still to finalize. The docs suggest bounded defaults but do not invent NMC2 policies.
