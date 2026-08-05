# Group Probe Status Design

## 1. Purpose

Add a maintainable group-level synthetic probe and public status page to the same feature branch
as the affiliate commission work. The feature reuses new-api's existing channel test request
path, but selects a channel through normal group/model routing and stores purpose-built probe
history. It must not expose channel credentials, deduct user quota, alter channel state, or mix
synthetic results into real traffic metrics.

The historical server initially publishes these groups:

| Group | Initial probe model |
| --- | --- |
| `codex` | `gpt-5.5` |
| `codex纯血PRO池` | `gpt-5.5` |
| `provip` | `gpt-5.5` |

The mapping is deployment configuration stored through the normal option system, not hardcoded
in source. All three groups currently expose `gpt-5.5`; administrators may change the mapping
without rebuilding.

## 2. Scope

### Included

- Typed settings for enablement, interval, retention, public visibility, and group/model mappings.
- Scheduled and manual probe runs using the existing cross-instance system task framework.
- Normal production channel selection for each configured group/model pair.
- Persistent individual results plus server-side 24-hour aggregation.
- A public read-only API and public `/status` frontend route.
- An administrator settings surface and immediate-run action.
- SQLite, MySQL 5.7.8+, and PostgreSQL 9.6+ compatibility.

### Excluded

- External provider status feeds, incident authoring, alert delivery, SLA guarantees, automatic
  channel enable/disable, testing every model, and exposing physical channel details publicly.
- Treating synthetic availability as real-user success rate.

## 3. Configuration

Create a `group_probe_setting` package following existing typed setting patterns. Persist one JSON
option with this validated structure:

```json
{
  "enabled": false,
  "interval_minutes": 10,
  "retention_days": 7,
  "timeout_seconds": 45,
  "groups": [
    {"group": "codex", "display_name": "Codex", "model": "gpt-5.5", "public": true}
  ]
}
```

Validation rules:

- interval: 5 through 1440 minutes;
- retention: 1 through 90 days;
- timeout: 5 through 120 seconds;
- at most 50 unique groups;
- group, display name, and model are trimmed, nonempty, and length-bounded to database columns;
- duplicate group entries are rejected;
- each mapping must reference an enabled ability for the selected group and model when saved;
- public output includes only entries with `public=true`.

The feature defaults to disabled on generic installations. Deployment acceptance enables it and
seeds the three mappings above. Configuration changes do not delete history immediately.

## 4. Probe Execution

Add `SystemTaskTypeGroupProbe` and a scheduled handler. The existing `SystemTask` active key and
lease provide cross-instance deduplication. A manual administrator request enqueues the same task
type; it does not start an untracked goroutine.

For every configured group mapping:

1. Validate the group/model still has at least one enabled ability.
2. Determine the request path and endpoint type with the same model rules used by channel tests.
3. Select one channel with the normal group/model selection path at retry zero.
4. Run the existing test request machinery with explicit probe options:
   - `UsingGroup` set to the configured group;
   - per-probe context timeout;
   - no channel response-time update;
   - no automatic channel enable/disable;
   - no consume log, quota-data export, or real-traffic performance metric;
   - no full response body or credential-bearing context log.
5. Persist one result even for local failures such as no eligible channel or timeout.

Runs are sequential by default because the expected list is small and upstream calls cost real
tokens. Cancellation and lost leases stop remaining probes. One group failure does not prevent
other configured groups from running. The task result contains counts only; detailed errors stay
in protected result rows and administrator APIs.

The existing channel test behavior remains unchanged for its current callers. Refactoring uses
an options struct or wrapper so group probes can suppress side effects without duplicating the
provider request/adaptor implementation.

## 5. Data Model

Add `GroupProbeResult` through the normal GORM migration paths:

| Field | Meaning |
| --- | --- |
| `id` | Primary key |
| `task_id` | System task that produced this sample |
| `group_name` | Configured new-api group |
| `display_name` | Display-name snapshot |
| `model_name` | Requested model snapshot |
| `channel_id` | Selected channel, administrator-only |
| `success` | Whether the complete test response validated |
| `latency_ms` | End-to-end probe duration |
| `error_code` | Stable internal error category |
| `error_message` | Sanitized administrator-only diagnostic |
| `checked_at` | Unix timestamp |

Indexes cover `(group_name, checked_at)` and `checked_at`. Error persistence strips upstream
bodies, credentials, authorization headers, URLs containing secrets, and unbounded text. Cleanup
deletes rows older than configured retention after successful scheduled runs.

Probe history is independent of `perf_metrics`, consume logs, channel `response_time`, and user
quota tables. That separation keeps public labels accurate: this is synthetic probing, not real
traffic availability.

## 6. State and Aggregation

The public response uses a rolling 24-hour window with 30-minute buckets.

Current group state uses the latest three non-stale samples:

- `healthy`: latest sample succeeds and at least two of the last three succeed;
- `degraded`: recent samples contain both success and failure;
- `down`: the last three samples all fail;
- `unknown`: no sample exists, fewer than three samples exist without a current success, or the
  newest sample is older than twice the configured interval plus one timeout.

Each 30-minute bucket is:

- `healthy` when all samples succeed;
- `degraded` when success and failure are mixed;
- `down` when samples exist and all fail;
- `unknown` when no sample exists.

Availability is successful samples divided by all completed samples in the window. Average
latency uses successful samples only. The API also returns sample count, latest observation time,
freshness, configured interval, and bucket timestamps. Aggregate state never uses error text.

## 7. APIs and Authorization

Public endpoint:

- `GET /api/status/probes`: public mappings and aggregated status only.

The endpoint uses a short public cache (15 seconds), returns a stable success envelope, and is
rate-limited with existing public safeguards. It never returns channel id/name, raw error,
system task id, private group mappings, or credentials.

Administrator endpoints:

- `GET /api/group-probe/settings`;
- `PUT /api/group-probe/settings`;
- `POST /api/group-probe/run`;
- `GET /api/group-probe/results` with bounded pagination and filters.

Manual-run responses return the created task id. The frontend polls the existing
`GET /api/system-task/:task_id` contract; no duplicate group-probe task-status route is added.

All administrator routes require `AdminAuth` plus the existing channel-operate permission where
the permission framework applies. Save and run actions use critical rate limits.

## 8. Frontend

### Public `/status`

Build the actual status experience as the route's first screen, using current new-api layout,
tokens, components, icons, dark mode, and i18n patterns. The page contains:

- last update and explicit "synthetic probe" label;
- one unframed list of configured groups;
- current state, model name, 24-hour availability, average latency, and 30-minute status bars;
- expandable recent context without exposing protected errors;
- loading, empty, stale, partial-data, and request-error states;
- 30-second query refresh without causing layout movement.

Use semantic buttons, tooltips for status meaning, text equivalents for color, and horizontal
overflow behavior that remains usable on mobile. Do not clone the reference site's branding or
claim that results represent real traffic.

### Administrator settings

Add a focused settings section using existing form controls. Administrators can toggle the
feature, edit numeric limits, add/remove group mappings from enabled abilities, choose public
visibility, save, and enqueue a manual run. Show task progress and recent protected results.

All user-facing text is translated through the existing flat locale files. Generate
`routeTree.gen.ts`; do not hand-edit generated routes.

## 9. Error Handling and Security

- Probe requests use the selected channel's credentials only inside existing relay setup.
- Context cancellation closes upstream requests; response bodies are bounded and closed.
- Panics in one probe are recovered at the task boundary and recorded as sanitized failures.
- Public responses are derived from allowlisted fields and never serialize result models directly.
- Disabling probing prevents future scheduled tasks but preserves history until retention cleanup.
- Deleting or disabling a channel cannot break status queries; historical channel ids are optional.
- A configuration with no public mappings returns a successful empty state.

## 10. Verification

Backend tests prove:

- setting validation, defaults, option round trips, and group/model ability validation;
- scheduler enablement, interval, active-task deduplication, manual enqueue, and cancellation;
- normal group selection and explicit `UsingGroup` propagation;
- success, provider failure, timeout, no-channel, and deleted-channel persistence;
- probe mode skips consume logs, quota-data export, perf metrics, channel response updates, and
  auto-disable behavior;
- retention cleanup and all state/bucket boundaries, including stale and sparse data;
- public API field allowlist and private-group filtering;
- administrator authentication, permission, rate limit integration, and pagination bounds;
- GORM migration and queries on SQLite, plus MySQL deployment migration and PostgreSQL SQL review.

Frontend tests cover state labels, text equivalents, stale/empty/error states, expansion,
responsive overflow behavior, configuration validation, save/run flows, and polling cleanup.
Run affected tests, `bun run typecheck`, changed-file lint, route generation, `bun run build`,
targeted Go tests, `go test ./...`, and `go build ./...`.

## 11. Deployment and Acceptance

Deployment shares the affiliate feature's snapshot and rollback procedure. Before startup, create
a logical MySQL dump and preserve the previous immutable image id and compose file. After migration:

1. enable the three historical-server mappings;
2. run one manual group-probe task;
3. verify exactly one sanitized result per configured group;
4. verify no user quota, consume log, quota-data export, channel status, or response time changed;
5. verify `/api/status/probes` exposes only allowlisted fields;
6. inspect `/status` at desktop and mobile widths and confirm 30-second refresh stability;
7. allow scheduled execution to produce a second run and confirm task deduplication;
8. restore the prior image/config and database dump if migration or runtime acceptance fails.

The feature is accepted only when all three groups can be configured and probed through normal
new-api routing, status history renders publicly, protected details remain administrator-only,
and the documented rollback restores the previous healthy `IP:3000` deployment.
