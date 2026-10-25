# Workspace tools plan

Status: planning only. Researched October 25, 2026. Branch: `codex/tools-call`.

## Recommendation

Build a broad connector catalog with a small set of dependable actions per service. Start with **GitHub, Google Drive, read-only database queries, and web search**. Add **Notion and Linear** next if credentials and useful sample data are available. Add **Sheets and Calendar** through the Google account connection. Schedule Slack and email after those integrations work.

Keep Muse Spark Contributor as the model. Connectors run through our backend/OpenCode/MCP layer; adopting OpenAI's plugin design does not require switching model providers or purchasing an OpenAI model API subscription.

Aim for roughly 20–30 useful read-only actions across the first four or five integrations, subject to the milestone checks below. This is a proposed scope, not implemented functionality. A large catalog should not mean sending every tool schema and every document to the model on each question.

## Current repository inventory

| Capability | Implemented surface | Verified boundary |
| --- | --- | --- |
| GitHub | Six actions: repository/code search, get file, list/get issues, list PRs | Token or OAuth connection code exists; this planning pass did not verify live GitHub access. |
| Google Drive | Three actions: search, list folder, read file | OAuth and token refresh code exists. Native Docs/Slides export as text; Sheets export as CSV. No general PDF extraction or structured multi-tab Sheets reader. |
| Connection settings | Tool-pack list/connect/disconnect and OAuth callbacks | Credentials are stored locally under `.nmc3`, keyed by pack, shared by the API and MCP process. Not yet a per-employee connection model. |
| Agent permissions | Tool IDs in definitions and OpenCode allowlists | Adding a backend action alone will not make it available to an agent. Both catalogs and permission mappings must agree. |
| PostgreSQL | Business and agent persistence already run locally | Persistence is not a model-callable database integration. No general workspace database pack exists. |
| Web research | Earlier implementation on `codex/serpapi-research`, commit `cc5844b` | Not on this branch. Review and selectively reuse after transport compatibility checks; do not blindly merge its older runtime changes. |

Relevant code: `agents/internal/toolpacks/{packs,runner,oauth,creds,github,gdrive,mcp}.go`, `agents/internal/opencode/{coordinator,project}.go`, and `agents/opencode.jsonc`.

## Fix the shared tool path first

The latest vendor comparison screenshot is unrelated to Supabase connectivity. Specialists attempted prohibited file reads while the intended `catalog_search` tool was unavailable. Investigation found:

- OpenCode reported both `catalog` and `tools` MCP connections as failed with request timeouts.
- Both custom Go MCP servers currently use `Content-Length` message framing. Modern MCP stdio uses newline-delimited JSON. This is a strong compatibility lead, not a completed live diagnosis or repair. See the [official MCP Go SDK transport documentation](https://go.sdk.modelcontextprotocol.io/protocol/).
- The orchestrator prompt still says specialists read `catalog/vendors.json`, while the specialist configuration intentionally permits `catalog_search` and denies catalog file reads. Align instructions with the actual tool policy.
- The screenshot shows `SUCCEEDED` with no usable recommendation and `tools 7/4`. Audit success semantics and whether parent/child tool counts and limits measure the same scope.

Before expanding: initialize MCP, list tools, execute catalog search, and execute one connected workspace tool through the actual application. Keep direct file reads denied where the catalog tool is the intended interface. Check Go executable discovery and the service's subprocess working directory as part of this verification.

## Ranked integration shortlist

Setup effort is our engineering estimate for a developer-owned demo with usable accounts; it excludes vendor reviews, enterprise approval, and production rollout. Existing implementations still require live validation.

| Order | Integration | Why it earns a slot | Initial actions | What the owner needs | Effort / decision |
| --- | --- | --- | --- | --- | --- |
| 1 | GitHub | Existing code; strong engineering workspace use cases | Existing six; later PR diff and CI status | Fine-grained PAT with read permissions for selected repositories; some operations may require a different token/auth mode | Low. Validate existing adapter first. |
| 2 | Google Drive / Docs | Broad document knowledge; existing code | Existing three, then bounded text extraction improvements | Google Cloud project, Drive API enabled, OAuth web client ID/secret, redirect URI, consent screen, test users | Medium. Start credential setup early because OAuth is a dependency. |
| 3 | PostgreSQL / Supabase data | Reporting and workspace records; local demo needs no new vendor signup | List exposed datasets, describe columns, run saved report, bounded query | A dedicated read-only database connection or existing authenticated business API | Medium. Prefer business views/report endpoints over platform administration. |
| 4 | Web search | Answers questions beyond private documents; one credential | Search, fetch selected public page, return cited excerpts | One SerpApi or Tavily key | Low–medium. Prefer reviewing existing SerpApi work before adding a second provider. |
| 5 | Notion | Wiki/pages plus structured workspace data | Search accessible pages, fetch content, query a data source | Internal connection token and explicit access to sample pages; confirm account permissions | Low–medium. Good next addition if a useful Notion workspace exists. |
| 6 | Linear | Project tracking without building an issue system | Search issues, get issue, list projects/cycles | Read-only API key, preferably team restricted; OAuth for multiple users later | Low–medium using official MCP, assuming client compatibility. Skip if the team only uses GitHub Issues. |
| 7 | Google Sheets | Useful reporting; reuse the Google OAuth client registration | List tabs, read bounded range, get spreadsheet metadata | Enable Sheets API and add `spreadsheets.readonly` scope, then re-consent | Medium. Same client registration does not automatically grant the new scope. |
| 8 | Google Calendar | Deadlines and availability | List calendars, list events, free/busy | Calendar API enabled; narrow read/freebusy scopes and consent | Medium. High value only if scheduling is in the demo. |
| 9 | Slack | Team decisions and discussions | Search authorized messages, read thread, get source permalink | Slack app installation/OAuth, correct user scopes, workspace approval where required | Medium–high. Valuable but access/setup can dominate the demo schedule. |
| 10 | Gmail | Email context and follow-ups | Search and read threads; no sending initially | Gmail API, user OAuth, suitable scopes; broader distribution may require verification/assessment | High. Defer unless email is central. |
| 11 | Microsoft 365 | One ecosystem can cover OneDrive/SharePoint, Outlook, Teams | File search/read first, then calendar/mail | Entra app registration, delegated Graph scopes, tenant consent as applicable | High under unknown tenant policy. Prioritize instead of Google only if target users already live in Microsoft 365. |
| 12 | Dropbox / Box / Jira / Airtable | Additional storage, tickets, or tables | Search/list/read the relevant entities | Provider account, app/token, access to realistic sample data | Backlog candidates; access and API details need a dedicated research pass before selection. |

Provider facts: [GitHub MCP configuration](https://github.com/github/github-mcp-server/blob/main/docs/server-configuration.md), [Drive scopes](https://developers.google.com/workspace/drive/api/guides/api-specific-auth), [Notion internal connections](https://developers.notion.com/guides/get-started/internal-connections), [Linear MCP](https://linear.app/docs/mcp), [Calendar scopes](https://developers.google.com/workspace/calendar/api/auth), [Slack MCP](https://docs.slack.dev/ai/slack-mcp-server/), [Gmail scopes](https://developers.google.com/workspace/gmail/api/auth/scopes), and [Microsoft tenant consent policies](https://learn.microsoft.com/en-us/entra/identity/enterprise-apps/manage-app-consent-policies).

## Credential acquisition order

1. Create the Google Cloud OAuth client now: enable Drive, add the demo account as a test user, and register the app's actual callback. Current code defaults to `http://127.0.0.1:8080/oauth/gdrive/callback`; verify `NMC3_PUBLIC_URL` before registering it. Calendar/Sheets can reuse the client registration, but require their APIs/scopes and additional consent.
2. Create a GitHub token scoped to the demo repositories. Test the required endpoints, especially code search: fine-grained token support varies by endpoint; do not promise all six actions before checking them.
3. Choose which database the agent should query. For fastest demo progress, use a separate sample database or approved application views with a read-only role. The business database and agent-history database are different targets.
4. Obtain one search-provider key. [SerpApi](https://serpapi.com/search-api) offers structured search results; [Tavily](https://docs.tavily.com/) also exposes search and content extraction. Choose one for the first release rather than paying for and maintaining both.
5. If time remains, create a Notion internal connection and share the demo pages with it, or obtain a read-only Linear key if those tickets are more relevant.

The Meta model key authenticates inference only. The Supabase publishable key configures client access and does not grant arbitrary database or Supabase Management API access. Google Drive needs user authorization, not an ordinary API key.

Google's current `drive.readonly` scope is restricted. A test-user demo and a broadly distributed app have different verification requirements. Testing-mode external OAuth refresh tokens normally expire after seven days for these scopes. See [Google OAuth token expiration](https://developers.google.com/identity/protocols/oauth2). A selected-file design using `drive.file` can reduce scope, but changes broad Drive search into a file-selection workflow and needs a deliberate product decision.

## What to copy from OpenAI plugins

Use OpenAI's architecture as a reference, not a dependency on its catalog credentials:

- Give tools clear action names, bounded input schemas, stable resource IDs, concise structured results, and source URLs. Use `readOnlyHint` and accurate side-effect metadata; enforce permissions on the server. [OpenAI MCP server guidance](https://developers.openai.com/plugins/build/mcp-server)
- Separate connection setup from execution. Identify the connected account, request only required scopes, and report expired/missing authorization clearly. For a future ChatGPT-facing plugin, use its documented OAuth 2.1 contract; our provider connections still follow each provider's OAuth requirements. [OpenAI plugin authentication](https://developers.openai.com/plugins/build/auth)
- Prefer standard MCP interfaces where maintained vendor servers exist. OpenAI documents discovery, tool filtering, remote HTTP transports, and authorization. Its API connectors are product-specific; an OpenAI `connector_id` is not a portable plugin we can hand to Muse Spark/OpenCode. [OpenAI MCP integration guide](https://developers.openai.com/api/docs/guides/tools-connectors-mcp)

An installed ChatGPT/Codex app connection does not establish credentials for this application. Our Tools page must manage our own connections. A later OpenAI-facing MCP endpoint could reuse the same internal adapters; building or publishing that plugin is outside this plan's first milestones.

## Proposed architecture

```text
Workspace question
  -> choose relevant connected packs and permitted actions
  -> OpenCode / Muse Spark tool selection
  -> MCP adapter -> shared executor
  -> resolve caller's connection + enforce resource permissions
  -> vendor API or constrained database query
  -> bounded structured result + source references + execution event
```

Retain the existing pack IDs and allowlist mapping. Extend the pack contract with auth requirements, required scopes, connection health, pagination, output limits, and source references. Introduce per-user or explicitly shared organization connections before exposing multiple employees' private accounts; the current pack-keyed credential file cannot distinguish them. Ensure a remote MCP integration still passes through equivalent application authorization and audit controls.

Use existing REST adapters when they are already small and useful. Consider official remote MCP for new services such as Linear; validate discovery, token authentication, result sizes, allowlisting, and lifecycle before adopting it. Do not expose an unrestricted URL/token forwarding tool to the model.

For discovery, load only the packs relevant to the task and user. Keep large file contents behind explicit fetch/read actions. Cache unchanged source content and fetch bounded ranges or excerpts. Page through results only when needed. Measure actual per-run model tokens, provider requests, latency, and estimated spend; no cost claims based solely on the number of installed tools.

## Database integration decision

**Recommended application path:** domain tools such as `database.list_datasets`, `database.describe_dataset`, `database.run_report`, and `database.query_rows` over approved views. Preserve employee/workspace access checks. Start with saved reports and bounded filters; arbitrary SQL is optional and later.

**Optional developer path:** official Supabase MCP for schema/log investigation, scoped to one project with `read_only=true` and only needed feature groups. This is platform/developer access, not a replacement for per-employee application authorization. Supabase documents these controls in its [MCP guide](https://supabase.com/docs/guides/ai-tools/mcp).

If SQL is later added, use a database-enforced read-only role, approved schemas/views, statement timeout, and row/byte caps. A prompt saying "SELECT only" is insufficient. Keep admin credentials and the agent-history database out of general employee analysis tools.

## Milestones and acceptance checks

| Milestone | Scope | Completion evidence |
| --- | --- | --- |
| M0: tool reliability | MCP transport, catalog instructions, shared execution telemetry | Both local MCP servers connect; catalog search succeeds from a real run; forbidden reads remain blocked; over-limit runs and partial results are represented accurately. |
| M1: useful workspace | Existing GitHub + Drive, minimal DB reports, one search provider | One successful source-cited question per pack; disconnect/expired-credential errors; read-only behavior; secrets absent from results/logs. |
| M2: broaden with low setup cost | Notion and/or Linear; structured Sheets if needed | Each selected integration passes its own contract checks and one realistic cross-source task. |
| M3: multi-user readiness | Connection ownership, account identity, resource checks, refresh/revocation | Two employees cannot use each other's private connection or access resources their account lacks. |
| M4: communications/actions | Slack, Calendar, then email; controlled writes if requested | Provider access proven; writes have explicit product behavior and approval handling; no writes hidden inside read tools. |

Suggested M1 demo: "Use the requirements document in Drive and the GitHub README to identify setup gaps; compare them with an approved database status report and cite each source." Suggested later demo: "Find a Linear issue, its related design document, and the matching pull request; summarize the current blocker."

Planning defaults: developer-owned sample accounts, read-only initial capabilities, OAuth verification and enterprise reviews outside any implementation estimate. Order may change if a required credential is blocked. No new integration, credential request, provider subscription, or external account change is executed by this plan.
