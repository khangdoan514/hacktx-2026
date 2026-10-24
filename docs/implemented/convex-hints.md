# Implemented Convex hints

Convex was a per-user hint bus. It was not the source of truth. Product pages polled the API. This checkout did not mount a Convex browser subscription. The Go outbox and server-sent events replace it, and the Convex package has been removed.

## Queries and mutations

`listMine` requires `ctx.auth.getUserIdentity()`. With no identity it returns an empty list. It reads `liveHints` for `identity.subject` and drops rows older than seven days.

`apply` is an internal mutation. It records `eventId` in `hintDeliveries` and does nothing if that id was already seen. For each recipient it deletes that user's hints older than seven days, then inserts or patches the resource hint. An existing hint with a higher revision is left in place.

`revoke` is an internal mutation. It dedupes on `eventId`, then deletes the user's hints in the named workspace. `scope` `project` also requires the project id to match.

## Signed delivery

The worker signs `timestamp.body` with HMAC-SHA256 and `CONVEX_HINT_SECRET`. It posts to `CONVEX_HTTP_URL/hints` or `/hints/revoke` with `X-Hint-Timestamp` and `X-Hint-Signature`. The HTTP action rejects a missing secret, a skew beyond five minutes, or a bad signature. If the URL or secret is empty, dispatch skips the post and does not roll back the business transaction.

`push_convex` only includes recipients who still have an active project membership, or an active workspace membership when the event has no project. `access.revoked` calls revoke instead.

`CONVEX_DEPLOY_KEY` is not part of the example environment. Hints are not a public list.

## Auth limit

`convex/auth.config.ts` registers a custom JWT provider only when issuer and JWKS are configured, and only for RS256 or ES256. The local HS256 issuer is not a Convex provider. Hint revocation does not, by itself, remove Postgres project membership. That gap is the membership defect in `access.md`.
