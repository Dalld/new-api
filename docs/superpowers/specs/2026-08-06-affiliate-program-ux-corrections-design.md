# Affiliate Program UX Corrections Design

## Context

The referral page currently reads `users.aff_count` from `/api/user/self` for
the total invite count. That counter can drift from the authoritative
`users.inviter_id` relationship table, especially after administrator binding
operations. The invitee list also returns full usernames to the browser, and
the transfer action is visually separated from the pending balance it acts on.

## Goals

- Make the total invite count equal to the current number of users whose
  `inviter_id` is the authenticated user.
- Show invitee identity as a stable user ID plus a masked username and
  registration time. The full username must not be returned by the user-facing
  invitee endpoint.
- Show the current public referral rules: the commission rate, inviter signup
  reward, invitee signup reward, and whether compliance currently enables those
  rewards.
- Make transferring pending rewards a primary action inside the pending reward
  statistic.
- Preserve the existing commission settlement and transfer semantics.

## Non-goals

- Do not change how commission amounts are calculated or settled.
- Do not retroactively repair `aff_count`; the read path will use the
  authoritative relationship count.
- Do not expose administrator-only configuration or payment credentials.
- Do not publish to GitHub or push any container image until the user approves
  after testing.

## Design

### Backend contract

Add a user-authenticated referral overview endpoint. It returns:

```json
{
  "invitee_count": 6,
  "commission_rate": 0.1,
  "inviter_signup_reward_quota": 100000,
  "invitee_signup_reward_quota": 50000,
  "payment_compliance_confirmed": true
}
```

`invitee_count` is calculated with a GORM count scoped to
`users.inviter_id = current_user_id`, using the existing cross-database query
patterns. The rule fields are read from the existing validated affiliate and
quota settings. Fixed signup rewards are reported as effective rewards only
when payment compliance is confirmed; otherwise they are zero and the response
still reports the disabled state.

The invitee endpoint changes its response DTO to include `id`, a server-side
`masked_username`, `display_name` only when it is non-sensitive, and
`created_at`. It does not select or serialize the full username. Search remains
server-side over username/display name, but the response never echoes the
unmasked value.

### Frontend

- Fetch the overview alongside the existing summary and use
  `invitee_count` for the total invite card.
- Render the pending reward card with the amount, eligibility description, and
  a prominent transfer button in the same card. Keep the existing dialog and
  compliance guard.
- Render a dynamic rules panel with translated labels:
  - `充值返利：受邀用户成功充值后，邀请人按当前返利规则获得实际充值对应的 10% 返利。`
  - `邀请注册赠送：邀请人 ...；受邀用户 ...。`
  The percentage is formatted from the server value, not hard-coded.
- Render invitees as `用户 ID` plus the returned masked username and time. Use
  the same masking contract on desktop and mobile.
- Keep all text in the seven locale files and preserve responsive layout,
  loading, error, empty, disabled, and refresh states.

### Accuracy wording

The implementation will describe the commission as the configured percentage
of the eligible recharge base used by the existing settlement engine. It will
not claim a different cash calculation than the backend actually performs.
Historical commission rows will continue to use their frozen historical rate.

## Testing

- Backend tests cover authoritative invite counting, public overview values,
  compliance-disabled rewards, and the absence of raw usernames in the
  invitee response DTO.
- Frontend tests cover count selection, dynamic rule text, pending-card
  transfer action, masked identity rendering, and error/loading states.
- Run Go tests for affected controller/model packages, frontend typecheck,
  lint for changed files, targeted frontend tests, and a production build.
- Deploy to `qiniu`, verify container health, `/api/status`, the generated
  asset checksums, and the authenticated referral page before requesting
  approval for GitHub and image synchronization.

## Rollout

Build and deploy the tested version to the existing New API Compose project on
`qiniu`, preserving MySQL and Redis volumes. Keep the current deployment
backup. Do not push GitHub commits or update GHCR until the user explicitly
approves both source and image synchronization.
