# Connecting personal accounts

Employees sign in to NMC3 first, open **Tools**, and choose **Connect Google Drive**, **Connect Notion**, or **Connect with token** under GitHub. Provider account linking is separate from the Supabase login used to access NMC3. The server's OAuth client credentials identify this application; each employee authorizes their own account.

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

New OpenCode sessions use an isolated configuration location per employee. Their tools process receives the corresponding credential directory, without secrets in prompts or configuration. Threads allow read-only GitHub/Drive tools and catalog search; direct file reads, shell, editing, and subagent calls remain disabled. Opening or sending to another employee's thread is refused. Previously unowned threads must be recreated to use personal connections.

Custom MCP servers now emit newline-delimited JSON; they continue accepting old framed input for compatibility. Catalog specialists use `catalog_search`, rather than direct catalog file reads.

## Verification

Automated tests cover connection ownership, missing identity, OAuth state surviving API recreation, callback cookie binding, single-use callbacks, expiry, disconnection, account testing, MCP newline messages, per-employee runtime configuration, and thread ownership. Provider calls in these tests use a fixture service.

Real Google consent still requires the project's callback/audience settings and the employee's approval. Passing local tests or enabling OAuth does not prove live access to a Google Drive document.
