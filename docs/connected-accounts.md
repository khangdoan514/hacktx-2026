# Connecting personal accounts

Employees sign in to NMC3 first, open **Tools**, and choose **Connect Google Drive**, **Connect Notion**, **Connect Slack**, or **Connect with token** under GitHub. Provider account linking is separate from the Supabase login used to access NMC3. The server's OAuth client credentials identify this application; each employee authorizes their own account.

After consent, Tools shows the account email or workspace name and offers **Test connection**, **Reconnect**, and **Disconnect**. The test verifies account access without reading documents. Disconnect removes the app's local credentials; provider-side revocation remains available from the provider's account settings.

## Notion setup

Create a public OAuth connection in the Notion developer portal and enable **Read content**. Register the exact local redirect URI:

```text
http://localhost:8080/oauth/notion/callback
```

Set `NOTION_CLIENT_ID`, `NOTION_CLIENT_SECRET`, and `NOTION_PUBLIC_URL=http://localhost:8080` in the ignored `agents/.env`. Restart the agent API, sign in to NMC3, and use **Tools → Connect Notion**. Choose the workspace and pages to share in Notion's consent page. OAuth client credentials alone do not authorize workspace data. On a deployed system, set `NOTION_PUBLIC_URL` to the backend's HTTPS origin and register that callback instead.

Notion uses a separate origin setting so its `localhost` callback does not change the registered Google callback. The authorization launch and browser-binding cookie both use this origin. Token exchange/refresh use JSON with HTTP Basic authentication. Access and refresh tokens are saved only to the employee's credential store, and rotated token pairs replace the previous pair.

The initial pack has five read-only actions: `notion.search`, `notion.get_page`, `notion.list_blocks`, `notion.get_database`, and `notion.query_data_source`. Search matches **titles**, not full content. Page metadata and body blocks are separate reads; follow child-block IDs and pagination cursors for nested content. Data-source queries return bounded rows; they do not run SQL or modify data. The adapter pins API version `2025-09-03` and uses data-source queries rather than the deprecated database query endpoint.

Use a new **Thread** after connecting, or select **Workspace assistant 1.1.0**, which adds the Notion tool allowlist. Previously published Workspace assistant 1.0.0 is unchanged in the registry. If results are empty or Notion returns 404, check the selected page access and the integration's Read content capability. A 403 may also indicate missing capabilities. The API never returns upstream error bodies that could contain private information.

References: [Notion OAuth](https://developers.notion.com/guides/get-started/authorization), [title search](https://developers.notion.com/reference/post-search), and [data-source queries](https://developers.notion.com/reference/query-a-data-source).

## Slack setup

In the Slack app's **OAuth & Permissions** settings, add these **User Token Scopes** for the default public-channel integration:

```text
channels:read
channels:history
search:read.public
```

The app requests a personal user token, not a bot token. No posting, editing, channel membership changes, private-channel search, or direct-message scopes are requested by default.

For local development, enable **PKCE** in the Slack app and register this exact redirect URL:

```text
http://localhost:8080/oauth/slack/callback
```

Slack documents enabling PKCE as a one-way change that marks an app as a public client; it cannot be disabled without Slack support. If that is unsuitable for an existing app, use a separate development Slack app, or deploy the backend behind HTTPS and choose the standard server flow instead. The app does not change Slack dashboard settings automatically. See [Slack's PKCE requirements](https://docs.slack.dev/authentication/using-pkce/).

Local configuration in ignored `agents/.env`:

```text
SLACK_CLIENT_ID=<your client id>
SLACK_CLIENT_SECRET=<your client secret>
SLACK_PUBLIC_URL=http://localhost:8080
SLACK_OAUTH_PKCE=true
SLACK_USER_SCOPES=channels:read,channels:history,search:read.public
```

For the standard server flow, set `SLACK_OAUTH_PKCE=false`, set `SLACK_PUBLIC_URL` to the backend's HTTPS origin, and register that origin plus `/oauth/slack/callback`. Local HTTP without PKCE is refused at sign-in start. Each OAuth attempt persists a unique PKCE verifier with the employee's state; only the SHA-256 challenge goes to Slack in the authorization URL. The localhost PKCE exchange omits the client secret.

After restarting the API, use **Tools → Connect Slack**, authorize your workspace, and return to **Test connection**. Workspace installation policy may require an administrator's approval. The callback saves only the `authed_user` token, and rejects bot-only authorizations. Refresh and expiry are handled, including replacement of rotating refresh tokens. The account test calls `auth.test` without reading messages.

The five read-only actions are `slack.search_messages`, `slack.list_channels`, `slack.get_channel_history`, `slack.get_thread`, and `slack.get_permalink`. Search uses `assistant.search.context`, restricted to public-channel messages. Returned search results have source permalinks. History/thread results can be cited by calling `get_permalink` with the channel ID and timestamp. Search is limited to 20 results per call; history and thread reads to 15, with explicit pagination. Rate-limit errors return a retry instruction without automatic request loops. No Slack content is read just by connecting.

Search depends on Slack's Real-time Search availability and the app's AI/search permissions. Basic channel tools are separate from search; they still require their granted scopes. This implementation does not silently fall back to legacy `search:read` APIs or ask for additional scopes on a failed read. See [Slack Real-time Search](https://docs.slack.dev/apis/web-api/real-time-search-api/) and [Slack OAuth](https://docs.slack.dev/authentication/installing-with-oauth/).

Start a new Thread after connecting or use **Workspace assistant 1.2.0**, which adds Slack to its tool allowlist. The previously published 1.0.0 and 1.1.0 definitions remain in the registry unchanged. Live message access is unverified until the employee completes real Slack consent.

## Google Cloud setup

In the Cloud project containing the web OAuth client:

1. Enable Google Drive API.
2. Configure Google Auth Platform branding and a support/contact email.
3. For development, set the audience to External with publishing status **Testing**, and explicitly add each tester's Google email under **Test users**.
4. Add this exact authorized redirect URI to the web client for the default local setup:

   ```text
   http://127.0.0.1:8080/oauth/gdrive/callback
   ```

5. Set `GOOGLE_CLIENT_ID` and `GOOGLE_CLIENT_SECRET` in the ignored `agents/.env`, then restart the agent API. `NMC3_PUBLIC_URL` determines the callback origin; `NMC3_APP_URL` determines the return to Tools. Register any changed callback exactly.
6. Start a fresh connection from Tools. A prior callback URL is single-use and is not a reusable sign-in link.

`redirect_uri_mismatch` means the requested callback differs from the client's registered callback, including host, port, path, or trailing slash. `localhost` and `127.0.0.1` are different hosts.

An access-blocked verification error may mean the account is not an allowed test user. Test users can still see an unverified-app warning. Workspace policies and Advanced Protection can impose additional restrictions. Development/testing apps have verification exceptions; public distribution and restricted Drive scopes need separate review. See [Google audience settings](https://support.google.com/cloud/answer/15549945?hl=en) and [verification exceptions](https://support.google.com/cloud/answer/13464323?hl=en).

The current Drive connection requests `drive.readonly`. Google documents this as a restricted scope. Testing-mode authorizations normally expire after seven days. Sheets and Calendar are separate APIs/scopes and are not added by this change. An ordinary Google API key does not authorize access to a person's private Drive.

## Connection ownership and execution

The guarded application proxy supplies the employee and organization IDs. Account APIs reject requests without both. Credentials are stored under `.nmc3/users/<owner hash>/` with private file permissions; the API never returns tokens. Legacy shared credentials are not adopted into an employee's account automatically.

Pending OAuth attempts are persisted for ten minutes, tied to the initiating employee and a callback-browser cookie, and consumed once. This lets a sign-in survive an agent API restart. Older links created before this implementation must be replaced by a fresh connection attempt.

New OpenCode sessions use an isolated configuration location per employee. Their tools process receives the corresponding credential directory, without secrets in prompts or configuration. Threads support connected-account tools, catalog search, file reads, and web tools. Shell commands, edits, and grep require the per-call permission decision in the Threads UI. Subagent calls and reads of environment files, connection credentials, OAuth state, and OpenCode service registration remain denied. Opening or sending to another employee's thread is refused. Previously unowned threads must be recreated to use personal connections.

Custom MCP servers now emit newline-delimited JSON; they continue accepting old framed input for compatibility. Catalog specialists use `catalog_search`, rather than direct catalog file reads.

## Verification

Automated tests cover connection ownership, missing identity, OAuth state surviving API recreation, callback cookie binding, single-use callbacks, expiry, disconnection, account testing, MCP newline messages, per-employee runtime configuration, and thread ownership. Provider calls in these tests use a fixture service.

Real Google consent still requires the project's callback/audience settings and the employee's approval. Passing local tests or enabling OAuth does not prove live access to a Google Drive document.
