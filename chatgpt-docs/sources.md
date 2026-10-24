# Source register and provenance

## Human requirements and clarifications

- Original request: comprehensive repository pages/interactions/backend audit, excluding landing/front-page and visual design work.
- Original pipeline notes: management, BA/transformation, engineering, partner context and structured handoffs; parts superseded by internal-only scope.
- Supabase clarification: selected platform with future agent integration.
- Four whiteboard photos (`IMG_3147.jpg` through `IMG_3150.jpg`): overlapping capability headings and internal Hardware/Software versus external GPUaaS diagram. Relevant contents summarized in [product context](product-context.md); source images are not included.
- User explicitly confirmed PR means pull request/code review, and Hardware/Software are the two divisions with AI in Software.
- User requested the entire NMC3 custom backend in Go, interim dummy agents labeled in code rather than website badges, and explicit removal instructions.
- User clarified no dashboard work for now; the stronger freeze supersedes permissive dashboard text in earlier prompts.
- Latest vision: developer CLI/SDK and less technical website clients; evaluation, triggers, budget, guardrails, prompt and observation are core.
- On discovering main's OpenCode change, the user selected: follow OpenCode; use a scripted dummy execution adapter meanwhile.
- User authorized creating `chatgpt-docs` and pushing that documentation directly to main.

## Repository evidence

- [Original audit baseline](https://github.com/khangdoan514/hacktx-2026/tree/f0f0b1fd6467914e5b2424c49093f02283183e8b)
- [Initial main agent roadmap](https://github.com/khangdoan514/hacktx-2026/tree/df5575f13f86b790c872406a24fe77174832ea58/agents)
- [Pinned Cursor implementation](https://github.com/khangdoan514/hacktx-2026/tree/12beedc14d00053947d8bb9f1c6501c76c3e2f02)
- [Cursor PR #3](https://github.com/khangdoan514/hacktx-2026/pull/3)
- [Main runtime-removal commit](https://github.com/khangdoan514/hacktx-2026/commit/c1a4fb603a73d84f46d07c3fcc8a20045377cb43)
- [OpenCode adoption document at preparation](https://github.com/khangdoan514/hacktx-2026/blob/c1a4fb603a73d84f46d07c3fcc8a20045377cb43/agents/docs/opencode-adoption.md)
- [History-only entrypoint at preparation](https://github.com/khangdoan514/hacktx-2026/blob/c1a4fb603a73d84f46d07c3fcc8a20045377cb43/agents/cmd/nmc3-agent/main.go)

Relevant existing agent documents include product-spec, target-architecture, first-pass, architecture, contracts-v1, storage, testing, pass-specs, plan, parallel-work and opencode-adoption. Preserve their canonical ownership; resolve contradictory status prose against actual code and current user decisions.

## External implementation references

- [Supabase JWT documentation](https://supabase.com/docs/guides/auth/jwts): signing schemes and verified token handling.
- [Supabase Storage access control](https://supabase.com/docs/guides/storage/security/access-control): object policies and private access.
- [OWASP agent security guidance](https://cheatsheetseries.owasp.org/cheatsheets/AI_Agent_Security_Cheat_Sheet.html): review reference for tool permissions, untrusted input, human decisions and adversarial testing.

These references were consulted during review/planning. Recheck current official documentation before implementing version-dependent integrations. This pack does not independently validate OpenCode model IDs, price claims or API details quoted by upstream repository documents.

## Evidence boundaries

The photos/user messages establish intent; source code establishes observed implementation; tests establish only the behaviors exercised. Architecture diagrams and proposed state machines establish desired design. Do not conflate any of them. Archived prompts are retained for traceability and are explicitly not active instructions.
