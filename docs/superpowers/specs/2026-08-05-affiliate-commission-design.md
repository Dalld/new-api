# Affiliate Recharge Commission Design

## 1. Purpose

Create a maintainable `feature/affiliate-commission` branch on top of upstream commit
`0ab02020603d22e5613bc4cf46bfab06f8567769`. The branch adds referral relationships,
recharge commissions, user-facing referral details, and administrator audit views while
remaining suitable for later rebases or merges from upstream.

The supplied `new-api-deploy` archive is a behavioral reference only. Local credentials,
SQLite files, generated route output, development compose files, Windows scripts, and
unrelated source changes are not imported.

## 2. Scope

### Included

- Persist the inviter relationship for every supported registration flow.
- Keep the existing fixed invitation reward behavior.
- Calculate recharge commission from the invitee's actual paid amount.
- Credit commission to the inviter's pending `aff_quota`; users transfer it to `quota`
  with the existing wallet action.
- Expose paginated user views for invitees, commission records, and referred recharge totals.
- Expose administrator views for referral relationships and commission records.
- Add system settings for enabling commission and configuring its rate.
- Support SQLite, MySQL 5.7.8+, and PostgreSQL 9.6+.
- Build and deploy a pinned image to the historical backup server for acceptance testing.

### Excluded

- Multi-level commissions, withdrawals to cash, commission reversal/refunds, settlement
  periods, custom per-user rates, and retroactive commission for old successful orders.
- Changes to upstream project identity, unrelated pages, payment pricing, or production data.
- Importing `.env`, `one-api.db*`, database volumes, or credentials from the archive.

## 3. Business Rules

1. A user has at most one inviter. The inviter is fixed when registration succeeds.
2. Self-invitation and nonexistent inviters are rejected by the existing invitation lookup.
3. The existing fixed registration reward remains unchanged and independent of recharge
   commission. A valid inviter relationship is persisted even when the fixed reward is zero.
4. Commission is generated only when all conditions hold:
   - the recharge transitions from pending to successful;
   - commission is enabled and the configured rate is greater than zero;
   - the invitee has a valid inviter;
   - the order has a positive frozen commission base;
   - no commission record exists for that top-up.
5. `CommissionRate` is a finite number in `[0, 1]`. Invalid persisted or submitted values are
   rejected or normalized to a safe disabled value, never used in arithmetic.
6. The commission base represents purchased quota derived from money actually paid, excluding
   bonuses, promotional multipliers, gifted quota, subscription effects, and manual quota edits.
7. The rate used for an order is the rate at successful settlement. The paid-value conversion
   is frozen when the order is created so later price, group-ratio, or discount changes cannot
   alter the base.
8. Commission quota uses the project's checked quota conversion helpers and cannot overflow,
   become negative, NaN, or infinite. A rounded result of zero creates no record.
9. A successful commission increases `users.aff_quota` and `users.aff_history` by exactly the
   same amount. It does not directly increase `users.quota`.
10. The inviter transfers pending quota through the existing `aff_transfer` API. A successful
    transfer refreshes the user cache so subsequent requests see the new quota immediately.

## 4. Data Model

### `top_ups.commission_base_quota`

Add a signed integer column with a code-enforced default of zero. New online orders freeze the
paid-value base at creation:

```text
commission_base_quota = actual_paid_amount / channel_unit_price * QuotaPerUnit
```

Each payment channel derives the base with `shopspring/decimal` from the exact two-decimal money
snapshot sent to the provider and the channel unit price active at order creation. The frozen
integer is written in the same insert as the pending top-up. Existing rows retain zero and do
not receive retroactive commission. Callback handlers compare the provider-confirmed currency
and amount with the order snapshot whenever the provider supplies those fields; a mismatch does
not settle the order.

### `commission_records`

Add a model migrated by the normal GORM migration path:

| Field | Meaning |
| --- | --- |
| `id` | Primary key |
| `top_up_id` | Unique idempotency key referencing the recharge row |
| `order_no` | Provider/trade number for audit and search |
| `inviter_id` | Commission recipient |
| `invitee_id` | Recharging user |
| `payment_provider` | Stripe, Epay, Creem, Waffo, or other source |
| `paid_money` | Canonical decimal string of the actual paid amount captured for audit |
| `commission_rate` | Finite rate used at settlement |
| `commission_base_quota` | Frozen purchased-quota base |
| `commission_quota` | Pending quota credited to inviter |
| `created_at` | Settlement timestamp |

`top_up_id` has a database unique index. The implementation uses a portable GORM conflict
clause (`ON CONFLICT DO NOTHING` behavior emitted by GORM) and checks `RowsAffected`; only the
transaction that inserts the row may increment inviter balances. Order number is indexed for
search but is not the primary idempotency key.

`paid_money` is a fixed two-decimal string derived from the verified order snapshot, never from
binary floating-point formatting at read time. `commission_rate` is stored as its canonical
decimal string for audit; arithmetic uses `decimal.Decimal` and the final quota conversion uses
the project's checked quota helpers.

## 5. Transaction and Payment Flow

Payment controllers validate provider signatures and normalize callback input, then call a
model settlement function. Controllers do not update order status, user quota, or commission
balances independently.

Within one database transaction:

1. Lock the top-up row with `lockForUpdate(tx)` on MySQL/PostgreSQL; use the existing SQLite
   transaction behavior.
2. Verify provider ownership and order state.
3. If already successful, return an idempotent success without changing balances.
4. Mark the top-up successful and persist completion metadata.
5. Increase the invitee's purchased quota using the existing channel-specific settlement rule.
6. Load the inviter relationship and validate the current commission setting.
7. Attempt to insert `commission_records` by unique `top_up_id`.
8. If and only if insertion occurred, atomically increment the inviter's `aff_quota` and
   `aff_history`.
9. Commit all changes together. Any failure rolls back order status, invitee quota, commission
   record, and inviter balances.

Logs and cache synchronization occur after commit and must not repeat database increments.
Cache failure is logged and recovered by invalidation or reload; it does not rerun settlement.

Manual completion of an existing direct top-up uses the same settlement primitive and is marked
with the admin provider. Subscription-order compatibility top-up rows are excluded: they have a
zero commission base and never enter direct top-up settlement. All supported online providers
are migrated to this flow. Duplicate and concurrent callbacks must result in one recharge and
at most one commission record.

## 6. APIs and Authorization

Authenticated user routes:

- `GET /api/user/aff/invitees`: current user's invitees, paginated and searchable.
- `GET /api/user/aff/commissions`: current user's received commission records, paginated.
- `GET /api/user/aff/recharge_total`: aggregate commission base for the current inviter.
- Existing `POST /api/user/aff_transfer`: move pending `aff_quota` to spendable `quota`.

Administrator routes use `AdminAuth`:

- `GET /api/affiliate/relations`: inviters with relationships or commission activity.
- `GET /api/affiliate/commissions`: global commission audit list.

User queries always constrain by the authenticated user id. Request pagination uses existing
`common.PageInfo` conventions. Search input is parameterized through GORM. API responses follow
the project's standard success and error envelopes.

## 7. Registration Compatibility

Every registration path that accepts an invitation code, including password registration and
OAuth registration, must call one shared transaction-aware user insertion rule. It sets
`inviter_id` whenever the invitation is valid, regardless of whether `QuotaForInviter` is zero.
The fixed reward, invite count, and invitation history updates remain compatible with existing
behavior and occur once.

`aff_count` is treated as a relationship count rather than evidence that a fixed reward was
paid. Administrator relationship queries also include users discovered from actual inviter
relations or commission records so zero-reward deployments are visible.

## 8. Configuration

Add a system setting for recharge commission rate using the existing option storage and admin
settings patterns. The backend is authoritative:

- accept only finite values from `0` through `1`;
- `0` disables new recharge commissions;
- reject NaN, positive/negative infinity, negative values, and values above `1`;
- never expose secrets in public options.

The frontend presents the rate as a percentage while the API stores a decimal fraction. It
must preserve exact disabled state and not silently clamp invalid administrator input.

## 9. Frontend

- Preserve the wallet invitation reward card and existing transfer modal.
- Add a user referral page with summary values, invitation link/code, invitee list, and received
  commission list using existing feature, table, query, pagination, and i18n patterns.
- Add an administrator commission page with relationship and commission audit tabs.
- Add sidebar entries through the existing role/sidebar configuration model.
- Add translations for every supported locale, with English source keys and Chinese text where
  available; locale fallback must remain valid.
- Generate `web/src/routeTree.gen.ts` using the repository router generator. Do not copy the
  archive's generated file.
- Preserve `web/src/features/usage-logs/data/schema.ts` and all unrelated baseline files.

## 10. Migration and Compatibility

- Register `CommissionRecord` in both normal and batched migration paths if both are used by the
  application.
- Use GORM tags and clauses that work on SQLite, MySQL, and PostgreSQL.
- Do not use MySQL-only DDL or assume `AUTO_INCREMENT`.
- Existing users and top-ups remain valid. New columns use zero/empty defaults enforced by code.
- Re-running migration and restarting the application is safe.
- The first deployment requires a verified logical MySQL dump before application startup runs
  migration.

## 11. Error Handling and Observability

- Signature errors, provider mismatches, missing orders, and invalid state transitions do not
  modify data.
- Duplicate callbacks return the provider-required success response after confirming the order
  is already complete.
- Settlement failures return an error that causes the provider to retry where supported.
- Logs include top-up id, order number, provider, invitee id, inviter id, base quota, rate, and
  commission quota, but never credentials or complete payment secrets.
- Administrator records provide a durable audit trail independent of application logs.

## 12. Verification

Backend tests must prove:

- registration persists inviter relationships when the fixed inviter reward is zero;
- rate validation rejects out-of-range, NaN, and infinity values;
- each payment channel freezes the correct paid-value commission base;
- successful settlement updates order, invitee quota, commission record, and inviter pending
  balances together;
- an injected failure rolls all settlement changes back;
- repeated and concurrent callbacks create one commission and one quota credit;
- no inviter, disabled rate, zero base, and zero rounded commission create no record;
- transfer moves quota once and leaves database/cache-observable user state consistent;
- user/admin endpoints enforce ownership and role permissions;
- migrations and core model tests pass on SQLite, with MySQL deployment migration verified on
  the historical server; SQL is reviewed for PostgreSQL compatibility.

Frontend verification includes affected unit tests, `bun run typecheck`, lint on changed files,
route generation, and `bun run build`. Backend verification includes targeted Go tests followed
by `go test ./...` and `go build ./...`.

## 13. Historical Server Deployment and Rollback

1. Record the current container image id, compose configuration, health state, and mounted paths.
2. Create a timestamped `mysqldump` with routines, triggers, events, and a consistency-safe
   transaction; verify the dump is nonempty and can be parsed.
3. Archive deployment configuration and non-database persistent files without copying a live
   MySQL data directory.
4. Build the feature branch into an immutable image tag containing the Git short SHA.
5. Update only the test new-api service, retaining the previous image tag and backup paths.
6. Start the stack, wait for health, and verify `http://TEST_SERVER_IP:3000`, login, migration,
   referral APIs, duplicate settlement behavior, transfer, admin audit view, and container logs.
7. On failure, stop the feature container, restore the previous image/config, and restore the
   logical dump only when migration or test data changed the database. Verify the old health and
   HTTP endpoint after rollback.

The deployment is complete only when the fixed image is running, the database migration is
verified, user and administrator workflows work through the browser/API, duplicate processing
does not change balances, and a documented rollback artifact exists.

## 14. Branch Hygiene

- Keep commits separated by concern: design, model/migration, settlement, APIs, frontend,
  tests, and deployment documentation.
- Do not commit credentials, database files, build output, local IDE state, or server backups.
- Strengthen `.dockerignore` and `.gitignore` for `.env*`, database files, backup archives, and
  local tooling state without hiding required example configuration.
- Before later upstream integration, fetch upstream, merge or rebase deliberately, regenerate
  routes, rerun the full verification suite, and resolve payment-flow conflicts by preserving
  the transaction and idempotency invariants in this document.
