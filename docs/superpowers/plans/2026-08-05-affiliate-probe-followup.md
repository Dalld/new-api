# Affiliate and Probe Follow-up Implementation Plan

## Objective

Implement the approved follow-up specification in `docs/superpowers/specs/2026-08-05-affiliate-probe-followup-design.md`, preserve the existing black-and-gold referral UI, and deploy an immutable image to the test server.

## Workstream 1: Commission Snapshot and Direct Ownership

Files:

- `model/topup.go`
- `model/main.go`
- `controller/topup_commission.go`
- provider order-creation controllers under `controller/topup*.go`
- settlement and provider tests

Steps:

1. Add additive top-up snapshot fields for eligibility, direct inviter ID, and commission rate; retain `commission_base_quota`.
2. Build one helper that validates and freezes the snapshot from the order-creation user, configured rate, and computed base.
3. Apply the helper to every supported payment provider's order creation.
4. Make settlement consume only the persisted snapshot and never traverse an inviter chain or read the current rate/relationship.
5. Preserve the existing locked database transaction and unique top-up commission index.
6. Add historical/ineligible, A-B-C direct-only, changed relationship/rate, repeated callback, concurrent callback, and rollback-on-failure tests.

## Workstream 2: Dynamic 24-hour Probe Buckets

Files:

- `model/group_probe_result.go`
- `model/group_probe_result_test.go`
- `web/src/features/group-probe-status/types.ts`
- `web/src/features/group-probe-status/api.ts`
- `web/src/features/group-probe-status/components/status-timeline.tsx`
- public status tests

Steps:

1. Replace fixed 30-minute/48-bucket constants with bucket width derived from each target's effective interval.
2. Generate `ceil(1440 / interval_minutes)` buckets while keeping a precise 24-hour result window and a 288-bucket maximum.
3. Aggregate results into interval-aligned buckets and return the effective interval already used.
4. Normalize a variable bucket count in the frontend without inventing a second interval default.
5. Render variable grid columns with stable bucket widths and horizontal scrolling.
6. Keep 30-second API polling independent from the probe interval.
7. Cover 5/10/30/60-minute intervals, malformed values, boundaries, empty buckets, and scroll preservation.

## Workstream 3: Referral UI Migration

Files:

- `web/src/features/wallet/index.tsx`
- Wallet affiliate hook/component/dialog files where they become unused or shared
- `web/src/features/my-affiliate/index.tsx`
- `web/src/features/my-affiliate/api.ts`
- My Referrals schemas, tests, and locale resources

Steps:

1. Remove the referral card and transfer ownership from Wallet.
2. Reuse or relocate the transfer dialog without duplicating implementation.
3. Add the transfer command to the pending amount area in the existing black-and-gold card.
4. Use the existing `/api/user/aff_transfer` contract and internal quota units.
5. Refetch the authenticated summary after success and localize success/failure states.
6. Preserve mobile layout, copy actions, four statistics, search, pagination, and black-and-gold styling.

## Workstream 4: Affiliate Admin Failure

Files:

- `web/src/features/affiliate/api.ts`
- `web/src/features/affiliate/schemas.ts`
- `web/src/features/affiliate/index.tsx`
- `controller/affiliate.go`
- `model/commission.go`
- relevant router and tests only if the live response proves they are involved

Steps:

1. Capture the two live administrator endpoint status codes and response shapes without exposing credentials.
2. Reproduce the failure with a focused test.
3. Fix the actual backend query, authorization, field contract, or frontend schema mismatch.
4. Separate invitation-summary and commission-detail request states.
5. Treat successful empty responses as empty data, retain localized retry states, and avoid displaying backend internals.

## Integration and Verification

1. Run focused Go and Bun tests for each workstream.
2. Run all affiliate/probe frontend tests, Go package tests, TypeScript, oxlint, oxfmt, and the production web build.
3. Review schema migration and source archive contents for secrets.
4. Commit the implementation as a coherent feature commit.
5. Build a checksummed `git archive` and deploy with the hardened release script.
6. Verify MySQL/Redis identity preservation, application health, data counts, migrations, zero duplicate/orphan commissions, repaired admin endpoints, dynamic probe bucket count, and a real three-group probe run.
7. Run desktop/mobile Playwright acceptance for Wallet, My Referrals, Affiliate Admin, and public Status in system languages.
8. Update production rollout documentation with the final commit, archive checksum, image ID, rollback directory, migration details, and rollback constraints.
