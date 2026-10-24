# NMC3 product and implementation documentation

Conversation-derived planning pack, prepared 24 October 2026. **This folder documents decisions, requirements, reviews, and proposed work. It does not mean the described features have been implemented.**

NMC3 is NMC2's internal agent lifecycle and cross-team coordination platform. NMC2 has Hardware and Software divisions; Software contains the AI team. Developers will use a CLI/SDK; BAs, PMs, and other less technical employees will use the website. All clients share the same Go application API and permissions. Supabase remains the identity, database, and private-file platform.

The current execution direction follows main's OpenCode adoption. A scripted dummy execution adapter is the interim implementation; **do not rebuild the removed custom model/delegation/tool engine**. The user explicitly selected this direction after the change on main was discovered. The all-Go requirement applies to NMC3-owned application/backend code; OpenCode is the approved external execution dependency.

## Read these first

1. [Current decisions and precedence](decisions.md)
2. [Product context and whiteboard requirements](product-context.md)
3. [Scope, removal list, and dashboard freeze](scope-and-ownership.md)
4. [Go, Supabase, and OpenCode architecture](architecture.md)
5. [Second Cursor prompt: CLI/SDK/website follow-up](prompts/02-cli-sdk-website-follow-up.md)

## Implementation references

| Document | Purpose |
| --- | --- |
| [Domain and lifecycle](domain-and-workflows.md) | Roles, request-to-release workflow, transitions and business invariants |
| [Pages and interactions](pages-and-interactions.md) | Nontechnical workflows, actual commands, deferred dashboard work |
| [Core capability requirements](core-capabilities.md) | Evaluation, triggers, budgets, guardrails, prompts and observation |
| [API, CLI and SDK contract](api-cli-sdk.md) | Shared service behavior and developer-client requirements |
| [OpenCode and interim adapter](agent-execution.md) | Adapter seam, session/event mapping, deterministic testing and limits |
| [Data, migrations and files](data-and-storage.md) | Ownership, migration reconciliation, private objects and recovery |
| [Security requirements](security.md) | Identity, authorization, approval integrity, tools and isolation |
| [Delivery and acceptance](delivery-and-testing.md) | Phases, negative tests, browser/API verification and cutover |
| [Repository status and evidence](repository-status.md) | Pinned observations, review limits and main's changed runtime direction |
| [Open decisions](open-decisions.md) | Items to resolve without silently inventing product facts |
| [Source register](sources.md) | User clarifications, whiteboards, repository and external references |
| [Current implementation handoff](prompts/01-current-implementation-handoff.md) | Self-contained high-level instructions with links to detailed requirements |

## Historical material

The [archive index](archive/README.md) preserves the earlier comprehensive audit, earlier Go prompt, user-supplied Cursor prompt, and branch security review. These are historical evidence. They contain superseded FastAPI, Convex, partner-portal, custom-runtime, and dashboard instructions. **Do not execute archived prompts as current instructions.**

The current documents in this folder supersede the archived plans for this workstream. Actual repository behavior must still be checked against the current branch. This documentation-only publication does not modify any dashboard, code, runtime, migration, or other team's files.

## Working rules

- Preserve all dashboard code and behavior for now, including product-area dashboard routes and dashboard-assistance work.
- Distinguish confirmed user decisions, proposed implementation details, observed code, and unverified runtime capabilities.
- Protect the CLI/SDK and website equally. A different client never grants greater authority.
- Keep dummy implementation names/provenance in code and developer records, without website dummy badges. Do not invent operational results or usage.
- Read current main before implementation. Incorporate concurrent work without deleting or duplicating the agent team's OpenCode integration.
- Keep implementation status and verification evidence updated as separate facts from desired behavior.
