# Account Linking (名寄せ) Design

## Goal
Whichever linked Google account a person logs in with, they operate as one account. The base account (first registered) can link up to 5 additional accounts. Only the links are stored; no data is migrated.

## Behavior
1. Base account opens `/settings/accounts`, enters an email, and submits (CSRF-protected POST).
2. App creates `account_invites/{Hash(token)}` (`base_id`, lowercased `email`, `expires_at` = 24h), grants IAP access to that email (see IAP below), and sends a mail via the Gmail API with `BASE_URL/accounts/link?token=...`.
3. The invitee opens the link while logged in (IAP/OIDC) as the invited email. `GET` shows a confirmation page (mail scanners prefetch links, so GET never links). A CSRF-protected `POST` completes the link in one Firestore transaction.
4. After linking, every request by the linked login resolves to the base `User.ID`, so all existing ownership checks, quotas and API tokens use the base account's data.
5. Base can unlink from the same page; unlink deletes `account_links/{linkedUID}`, revokes the IAP grant (unless config.yaml allows that email), and takes effect on the next request.

## Data model
- `account_invites/{Hash(token)}`: `base_id`, `email`, `expires_at`. Single use.
- `account_links/{linkedUID}`: `base_id`, `email`, `created_at`. Listing/counting is a query on `base_id`.
- `Session` gains `LoginID` (the login's own hashed subject) and keeps `Email` as the login email. `User.ID` becomes the resolved base ID.

## Confirm transaction rules
- Invite exists, unexpired, deleted on use.
- Clicker's verified email equals the invited email; clicker is not the base; clicker is not already linked; clicker is not a base with links (no chains).
- Base has at most 5 links, counting pending invites.

## Identity resolution
`resolve(loginUID) -> baseUID` reads `account_links/{loginUID}`; missing means the user is their own base. Called from `withSession` (IAP) and `callback` (OAuth). Cookie reuse compares `Session.LoginID` to the login UID, and re-resolves the link on every request (one small read), so link and unlink take effect immediately. `PutSession` must not overwrite the base user's display name with a linked login's.

## Allowlist
Allowed = config.yaml allowlist OR an active link for the email. A pending invite is allowed only on the confirm routes. This applies to `withSession`, `issueToken` and the API token check.

## IAP grants (decided: conditional binding)
IAP blocks unlisted accounts before the app sees the link, so the grant must happen at invite time. `deploy.sh` currently rewrites every `roles/iap.httpsResourceAccessor` binding from config.yaml and would revoke all runtime grants.

Chosen: the app manages a separate conditional binding titled `shiori-linked-accounts` (policy version 3, read-modify-write with etag); `ReconcilePolicy` preserves that binding. Requires the runtime service account to have IAP policy admin on this one resource (a real privilege increase), and IAM propagation can take a few minutes (stated in the mail and UI).

Alternative: `cmd/iap-policy` also reads linked/pending emails from Firestore at deploy time. Simpler policy shape, but grants made at runtime would still be overwritten by a deploy unless reconciled the same way.

## Mail (Gmail API with ADC)
A service account has no mailbox. Send via domain-wide delegation: impersonate the sender (`MAIL_SENDER`) with scope `gmail.send` using the runtime service account's keyless token (TokenCreator on itself). Manual steps: enable `gmail.googleapis.com`, authorize the client in the Workspace admin console. In development the link is logged instead of sent.

## OAuth mode
Add the `email` scope and require `email_verified` so the clicker's email can be compared to the invite.

## Tests
`Mailer` and `AccessGranter` interfaces are stubbed. Emulator tests cover cap of 5, chains, self-link, email mismatch, expired/reused token, unlink, plus a regression test for session reuse by `LoginID`.
