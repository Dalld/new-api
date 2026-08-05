# Affiliate and Probe Follow-up Design

## Scope

This follow-up changes four connected behaviors without redesigning the existing black-and-gold referral page:

1. Remove the referral reward card from Wallet and move its balance-transfer capability into My Referrals.
2. Make the public probe timeline use the configured probe interval as its bucket width while retaining a 24-hour window.
3. Diagnose and fix the administrator affiliate page that reports `Failed to load referral data`.
4. Prove that commissions begin only for eligible post-update top-ups and are paid only to the invitee's direct inviter.

## Referral UI

The Wallet page will no longer render `AffiliateRewardsCard` or own the affiliate transfer dialog. Wallet remains focused on balances, recharge, redemption, subscriptions, and billing history.

The existing `/my-affiliate` black-and-gold card remains the single referral summary surface. It retains the affiliate code, invite link, invite count, pending commission, total commission, and referred recharge total. The existing `Transfer to Balance` command moves into this card next to the pending transferable amount. It opens the same quota transfer workflow and observes the existing payment-compliance restriction.

Transfer success must invalidate and refetch the authenticated user summary so the pending amount and global balance cannot disagree. Transfer errors remain localized and must not expose backend internals. The light Wallet referral card is removed rather than duplicated.

## Probe Timeline

The public status page continues to represent the most recent 24 hours. The effective backend `interval_minutes` is authoritative for each group's bucket width:

```text
bucket width = interval_minutes
bucket count = ceil(1440 / interval_minutes)
```

Examples are 288 buckets at 5 minutes, 144 at 10 minutes, 48 at 30 minutes, and 24 at 60 minutes. The configured validation floor of 5 minutes caps the timeline at 288 buckets.

The backend public-status aggregation generates buckets with the effective interval and returns `interval_minutes` with them. Results are grouped into those buckets; missing scheduled periods remain `unknown`, and multiple results in one bucket retain the existing healthy/degraded/down aggregation rule. Availability and average latency continue to use all results from the 24-hour window.

The frontend renders a variable bucket count with stable minimum bucket width and horizontal scrolling. It continues polling the public API every 30 seconds because polling frequency is independent of probe execution frequency. On initial load it positions the timeline at the newest bucket. A later refresh preserves a user's historical scroll position; if the user was already at the newest edge, it may continue following the newest result.

The labels remain `24 hours ago` and `Now` because the window remains fixed at 24 hours.

## Affiliate Admin Failure

The screenshot showing `Failed to load referral data` is the administrator `/affiliate` page. Implementation starts by inspecting the live responses from:

- `GET /api/affiliate/relations`
- `GET /api/affiliate/commissions`

The fix must address the actual failing layer: route/authorization, backend query, response field names, or frontend schema parsing. The frontend schema remains an explicit allowlist matching the backend JSON contract.

Invitation summary and commission detail use independent request states. A failure in one tab does not erase successfully loaded data in the other tab. The visible error is localized and offers retry; development diagnostics retain the endpoint and HTTP/business error category without displaying protected backend details to users.

Empty data is a successful empty state, not an error.

## Commission Eligibility

The business rule called "two-level relationship" in the request is implemented as one-hop, direct-inviter commission ownership:

```text
A invites B
B invites C
C recharges
=> B receives the commission
=> A receives no commission from C
```

Settlement reads only the recharging user's `inviter_id`. It must not traverse the inviter's inviter, recurse through ancestors, or create more than one commission record for a top-up.

Commission eligibility is frozen and persisted when a top-up order is created. The additive top-up snapshot contains:

- whether the order is commission eligible;
- the direct inviter ID;
- the commission rate;
- the existing commission base quota.

Historical rows receive migration defaults of ineligible, zero inviter, zero rate, and their existing zero base. No migration backfills these fields. This persisted snapshot is the release boundary: an order created before the updated behavior remains ineligible even if its payment callback arrives after deployment or after an application restart. New eligible orders created after the update capture all four values and settle only after successful payment. A later change to the inviter relationship or administrator commission rate affects only future orders, not pending snapshots.

Settlement uses only the order snapshot and runs inside the existing database transaction that locks the top-up, changes the top-up status, credits the recharge quota, inserts the unique commission record, and credits the direct inviter. The unique index on `commission_records.top_up_id` remains the database-level idempotency guard. Repeated or concurrent payment callbacks must not credit either recharge quota or commission twice, and a failure in any write rolls back every write.

No migration will backfill old top-ups or old commission records. Existing valid commission records are retained as audit history.

## Error Handling

- Referral code, summary, recharge total, invitee list, and commission list fail independently.
- A failed financial statistic displays `-` and a localized status message rather than a misleading zero.
- Transfer commands show localized success/failure feedback and refetch authoritative data after success.
- Public probe responses continue to omit channel IDs, channel names, task IDs, and backend error messages.
- A malformed probe interval falls back to the validated default before bucket generation; the public response reports the effective value actually used.

## Verification

Frontend tests will cover:

- Wallet no longer renders the referral card or transfer workflow.
- My Referrals retains the black-and-gold card and gains the transfer command.
- Transfer success refreshes the summary; transfer failure is localized.
- Admin summary and detail failures are isolated, empty data is not treated as failure, and retry works.
- All supported locales contain every new or moved string.
- Probe timelines render 288/144/48/24 buckets for 5/10/30/60 minute intervals.
- Initial scroll shows current results and refresh does not override historical browsing.

Backend tests will cover:

- Dynamic bucket width/count and exact 24-hour boundaries for supported intervals.
- Missing, duplicate, mixed-success, stale, and malformed interval cases.
- A-B-C settlement credits B only and never A.
- Historical, ineligible, zero-inviter, zero-rate, and zero-base top-ups produce no commission.
- A new eligible top-up produces exactly one commission.
- Every supported payment provider writes the same immutable commission snapshot when creating a new order.
- Repeated and concurrent callbacks remain transactionally idempotent, including forced failures between record insertion and inviter credit.
- Normal authenticated users can access self affiliate endpoints; administrator endpoints require administrator authorization.

Runtime acceptance on the test server will verify the existing data counts, migrations, unique index, zero duplicate/orphan commissions, the repaired administrator endpoints, a real three-group probe run, the public timeline interval, and desktop/mobile referral and status layouts.

## Deployment and Rollback

Deployment follows the existing immutable-image process: checksum the source archive, create a consistent database dump, build in an isolated release directory, and recreate only `new-api`. MySQL and Redis must retain their container IDs and start times.

Normal rollback restores only the previous application image. Database restore remains a last resort because it would discard post-deployment users, top-ups, balances, configuration, and commission activity. The additive probe/commission schema remains compatible with application-image rollback. A rollback to an image that does not implement the new snapshot must pause new top-ups or explicitly accept that orders created in the rollback window are ineligible and will not be backfilled later.
