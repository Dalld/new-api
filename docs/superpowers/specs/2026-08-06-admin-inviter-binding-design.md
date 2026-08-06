# Administrator Inviter Binding Design

## 1. Purpose

Allow a root administrator to bind an existing user to an inviter from the user management
table when that user has never had an inviter. The operation repairs missing referral
relationships without replaying registration rewards or rewriting financial history.

## 2. Scope

### Included

- Add a root-only action to an unbound user's row in the administrator user table.
- Search for and select an existing user as the inviter.
- Persist the relationship once, update the inviter's relationship count, and audit the action.
- Apply the relationship to recharge commission snapshots created after the binding commits.
- Localize the user interface in every locale currently shipped by the web application.

### Excluded

- Rebinding a user who already has an inviter.
- Unbinding or editing an inviter relationship.
- Binding users from the end-user interface.
- Adding a free-text reason field.
- Paying or replaying fixed registration invitation rewards.
- Recalculating old top-ups, commission records, balances, or frozen commission snapshots.
- Adding database columns, tables, or migrations.

## 3. Authorization and Visibility

The write endpoint uses `middleware.RootAuth()` directly. `AdminAuth` is insufficient. A
regular administrator keeps read access to the existing affiliate administration pages but
cannot call the binding endpoint.

The row action is rendered only when both conditions hold:

1. the signed-in operator has the root role; and
2. the target row has `inviter_id` equal to `0`, `null`, or absent in the decoded API object.

Frontend visibility is an ergonomic guard only. Backend authorization and validation remain
authoritative. A stale page, crafted request, or regular administrator request cannot bypass
the rules.

## 4. API Contract

Add a dedicated endpoint:

```text
POST /api/affiliate/bind
```

Request body:

```json
{
  "invitee_id": 123,
  "inviter_id": 456
}
```

Both values must be positive integer user IDs. The endpoint follows the project's standard
JSON response envelope. A successful response identifies the resulting invitee and inviter
IDs so the client can update or invalidate cached data. Errors return a stable, user-safe
message and do not expose SQL details.

Calling the endpoint again after the first successful binding, including with the same inviter,
is rejected as already bound. It is not reported as an idempotent success because the operation
contract permits one state transition only: unbound to bound.

The endpoint distinguishes these failure classes:

- malformed or non-positive IDs;
- target user not found;
- inviter not found;
- target user already bound;
- target and inviter are the same user;
- the relationship would create an invitation cycle;
- a concurrent write or database transaction conflict prevented the operation;
- the operator is not root.

No failure may partially increment `aff_count` or persist an inviter relationship.

## 5. Transaction and Integrity Rules

Implement the model operation as a focused transaction-aware function rather than embedding
relationship writes in the HTTP handler. The operation uses `lockForUpdate(tx)` so SQLite,
MySQL, and PostgreSQL retain their existing dialect behavior.

Within one database transaction:

1. Validate both IDs before querying.
2. Load only non-deleted users and lock the target and proposed inviter rows in ascending
   user-ID order. Canonical lock order prevents two opposite or overlapping manual bindings
   from silently creating a cycle. A disabled but non-deleted inviter remains eligible, which
   matches the existing persisted relationship semantics.
3. Verify both users still exist and the target's current `inviter_id` is zero while locked.
4. Reject self-binding.
5. Walk the proposed inviter's `inviter_id` chain. Track visited IDs, reject a repeated node,
   and reject the relationship if the target appears anywhere in that chain. Missing users in
   an already-corrupt chain also fail closed and emit a server warning.
6. Update the target with a conditional write equivalent to
   `WHERE id = invitee_id AND inviter_id = 0`. Exactly one affected row is required.
7. Atomically increment the selected inviter's `aff_count` by one.
8. Commit both writes together.

The conditional update is the final defense against stale reads and concurrent requests. A
zero-row result is reported as an already-bound or concurrent-conflict error after rechecking
the target state where practical. Database deadlocks or serialization failures are not treated
as success and do not trigger an automatic second financial write.

The function does not call registration insertion or invitation reward helpers. It changes
only the target's `inviter_id` and the selected inviter's `aff_count`.

## 6. Financial Semantics

The successful commit establishes the relationship at that moment. Existing top-ups and
commission records remain byte-for-byte unchanged. In particular, the operation does not
modify these frozen top-up fields:

- `commission_inviter_id`;
- `commission_rate`;
- `commission_eligible`;
- commission base or paid-money snapshots.

When the invitee creates a new top-up after the binding commits, the existing top-up creation
flow reads the new `users.inviter_id` and freezes the selected inviter into that order. Normal
settlement then creates commission under the existing eligibility, rate, idempotency, and
balance rules. A top-up created before the binding remains associated with its original frozen
state even if it settles afterward.

Binding and top-up creation are linearized by their database commits: when binding commits
first, the later order freezes the new relationship; when order creation commits first, that
order retains its pre-binding snapshot permanently. Tests coordinate transaction boundaries
explicitly rather than inferring order from timestamps or sleeps.

The manual operation grants no fixed invitation quota to either user and makes no direct change
to `quota`, `aff_quota`, or `aff_history`.

## 7. Audit Trail

After a successful transaction, record one management audit entry through
`recordManageAuditFor`:

```text
action: affiliate.inviter_bind
target user: invitee_id
params:
  invitee_id
  inviter_id
  previous_inviter_id: 0
```

The normal audit metadata records the root operator ID, username, role, authentication method,
client IP, and time. Add a stable English fallback template to `controller/audit.go`; localized
rendering follows the existing frontend audit-log mechanism. Failed requests do not create a
success audit entry, but authorization failures, relationship conflicts, detected corrupt
chains, and unexpected database failures enter the existing security/application logs with a
safe reason. No operator-supplied reason is collected or stored.

The relationship and `aff_count` are the atomic business transaction. The audit helper runs
immediately after that commit, following existing management-audit architecture; an audit-write
failure is logged as an operational error and never causes the relationship mutation to run a
second time.

## 8. Administrator Interface

Add `Bind inviter` to the existing per-user action menu in
`web/src/features/users/components/data-table-row-actions.tsx`. Use the established row-level
dialog pattern and keep its state local to the action/dialog components.

The dialog:

- identifies the target user by ID and username;
- starts with no search results and no selected inviter;
- searches `GET /api/user/search` after one non-whitespace character, with a 300 ms debounce
  and a page size of 20;
- supports the endpoint's existing exact numeric ID and partial username, display-name, and
  email matching, while displaying only candidate ID, username, and display name;
- requests or filters out deleted users; disabled but non-deleted users remain eligible;
- shows candidate ID, username, and display name;
- excludes the target user from selectable results;
- shows explicit loading, empty-result, request-failure, and retry states;
- clears the selected candidate whenever the normalized search term changes;
- disables the continue action until a candidate is selected;
- opens a separate confirmation step that names both users by username and ID and states that
  the relationship cannot be changed through this operation;
- disables repeated submission while the mutation is pending;
- keeps the dialog open with the selection intact when the server rejects the request.

Canceling confirmation sends no request and returns to the selection state. Closing the main
dialog resets its search, candidate, errors, and confirmation state. On success, close and reset
both steps, show a success toast, refresh the users query without changing the current user-table
search, filters, sort, or page, and invalidate the affiliate relationship queries. If the
follow-up list refresh fails, report that binding succeeded but refresh failed; do not retry the
binding automatically. Client-side exclusion of the target and hiding already-bound rows do not
replace backend validation.

The new text is translated for `en`, `zh`, `zh-TW`, `fr`, `ru`, `ja`, and `vi`. The interface
continues to follow the selected system/application locale; it does not force Chinese.

## 9. Error Handling

Controllers bind and validate a dedicated request DTO, call the model transaction, map typed or
sentinel domain errors to stable API messages, and record the audit only after success. They do
not perform separate relationship writes.

Unexpected database errors are logged with the operation and involved IDs but without
credentials or full user records. The client displays the server's safe message. A failed
request leaves the dialog available for correction or retry.

## 10. Verification

Backend tests must prove:

- root can bind an unbound target exactly once;
- normal users and regular administrators cannot use the endpoint;
- an existing relationship cannot be changed or removed;
- nonexistent target and inviter IDs fail without writes;
- self-binding, a two-user cycle, and a longer-chain cycle are rejected;
- concurrent attempts to bind one target produce one relationship and one `aff_count` increase;
- overlapping concurrent bindings cannot create a cycle;
- a successful binding increments only the selected inviter's `aff_count` once;
- no fixed registration reward or user balance field changes;
- existing top-up and commission rows do not change;
- a new top-up freezes the newly bound inviter while a pre-binding top-up does not;
- the success audit includes the operator, target, inviter, prior ID, and action;
- SQLite tests pass and the locking/query implementation is reviewed for MySQL and PostgreSQL.

Frontend tests must prove:

- the action appears only for a root operator and an unbound target;
- regular administrators and already-bound rows do not expose the action;
- search, selection, confirmation, cancellation, and target-user exclusion work;
- search covers debounce, ID/name matching, loading, empty, failure, retry, and candidate reset;
- pending submission cannot be duplicated;
- success closes the dialog, shows feedback, refreshes affected queries, and preserves the
  current table controls;
- a post-success refresh failure is distinct from a binding failure and does not resubmit;
- a server failure preserves the dialog and selection;
- all seven locale resources parse and contain the required keys.

Final verification runs focused Go tests, relevant frontend tests, changed-file formatting and
lint, `bun run typecheck`, and the production web build. Broader `go test ./...` and `go build
./...` run after focused tests when the repository environment supports them.

## 11. Delivery and Branch Hygiene

Implementation commits remain logically separated while development and review are active.
Before publishing the requested final-only feature branch, squash the feature work into the
single intended final customization commit, regenerate derived frontend files only through
their repository commands, and verify the resulting tree. Do not commit credentials, database
dumps, server backups, `.env` files, or generated build output.

Deployment to the test server is a separate, explicit step. Before any deployment, create and
verify a logical database dump and configuration backup, build an immutable image tagged with
the Git revision, replace only the `new-api` service, and retain the previous image for rollback.

## 12. Relationship to the Existing Commission Design

This document intentionally amends the registration-only relationship rule in
`2026-08-05-affiliate-commission-design.md` by adding the single root-operated unbound-to-bound
transition. It also clarifies the implemented commission behavior: inviter, eligibility, rate,
and base are frozen when a top-up is created, not first derived at settlement. Where those two
points conflict with the earlier design document, this document and the current snapshot-based
implementation govern this feature.
