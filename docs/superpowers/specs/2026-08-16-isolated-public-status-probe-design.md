# Isolated Public Status Probe Design

## 1. Decision and supersession

This design replaces `2026-08-05-group-probe-status-design.md` for all future
status-probe work. The previous design reused the channel-test path, added an
administrator surface, and exposed administrator APIs. Those choices conflict
with the current requirements.

The replacement is an in-process but independently owned public status probe.
It has its own configuration, scheduler, HTTP clients, lease records, result
records, public API, and `/status` UI. It must not call or modify the existing
channel test, automatic disable/recovery, routing, quota, logging, or response
time paths.

The implementation removes the old group-probe administrator UI and routes,
stops the old scheduler, and restores probe-only changes made to the existing
channel-test implementation. Existing `group_probe_results` rows may remain in
the database as inert rollback data; deployment does not drop or rewrite them.
No unrelated administrator page or API changes.

## 2. Goals

- Probe configured public group/model targets every 60 seconds.
- Measure endpoint HTTP ping and real model conversation latency separately.
- Store and publish the latest 60 raw observations per target.
- Render a public `/status` experience inspired by check-cx while retaining the
  New API public layout, tokens, components, dark mode, and typography.
- Show time, state, ping latency, and conversation latency for every history
  point on mouse hover, keyboard focus, and touch interaction.
- Keep the feature isolated from all existing channel health and business
  behavior, including multi-key rotation state.
- Deploy to qiniu first and wait for explicit user approval before any GitHub
  push or pull request.

## 3. Non-goals

- No administrator settings, menu, page, manual-run action, or protected result
  browser.
- No channel enable/disable, recovery, response-time update, test-time update,
  quota deduction, consume log, performance metric, or billing side effect.
- No SLA claim, incident management, alert delivery, or real-traffic metric.
- No public channel id, channel name, upstream URL, credential, user id, raw
  response, stack trace, SQL error, or unbounded provider error.
- No ICMP ping. "Ping" follows the check-cx endpoint transport measurement.

## 4. Configuration

Configuration is deployment-owned and is supplied through environment
variables or a Compose override. It is never persisted through an administrator
form.

Required settings:

| Setting | Default | Rule |
| --- | --- | --- |
| `PUBLIC_STATUS_PROBE_ENABLED` | `false` | Master kill switch |
| `PUBLIC_STATUS_PROBE_INTERVAL_SECONDS` | `60` | Fixed to 60 for qiniu acceptance |
| `PUBLIC_STATUS_PROBE_PING_TIMEOUT_SECONDS` | `8` | Range 1-15 |
| `PUBLIC_STATUS_PROBE_CHAT_TIMEOUT_SECONDS` | `45` | Range 5-60 |
| `PUBLIC_STATUS_PROBE_DEGRADED_MS` | `6000` | Successful conversations above this are degraded |
| `PUBLIC_STATUS_PROBE_CONCURRENCY` | `5` | Range 1-20 |
| `PUBLIC_STATUS_PROBE_RETENTION_DAYS` | `7` | Range 1-30 |
| `PUBLIC_STATUS_PROBE_TARGETS` | `[]` | Size-bounded JSON target array |

Each private target contains:

```json
{
  "key": "codex-gpt-5-5",
  "group": "codex",
  "display_name": "Codex",
  "model": "gpt-5.5",
  "channel_id": 123,
  "key_index": 0
}
```

`key` is a stable opaque public identifier and must be unique. `channel_id` and
`key_index` are private deployment selectors and are never returned publicly.
Explicit representative channels make observations reproducible and avoid
calling production routing or mutating round-robin key selection. Missing,
disabled, deleted, or unsupported targets produce a sanitized failed point;
the probe does not fall back to a different channel.

At most 20 targets are accepted. Strings are trimmed, UTF-8 validated, and
length bounded. Invalid configuration disables the probe and logs one bounded
configuration error without logging the JSON value.

## 5. Isolation boundary

The module must not import or call these behavior paths:

- `testChannel` or `testChannelWithOptions`;
- `SetupContextForSelectedChannel`;
- `CacheGetRandomSatisfiedChannel` or retry selection;
- `GetNextEnabledKey` or `SaveChannelInfo`;
- channel status, test time, response time, or auto-disable/recovery updates;
- quota, subscription, consume-log, quota-data, or performance-metric writes.

The target loader performs a read-only database query and copies only the
fields required to construct a request. A configured key is selected from the
copy by index without changing `channel_info`. The probe client owns a dedicated
bounded `http.Client` and minimal provider adapters. Pure URL and protocol
helpers may be reused only when tests prove that they perform no writes and do
not depend on mutable request context.

The initial adapter registry contains independent minimal clients for OpenAI-
compatible Chat Completions, OpenAI Responses, Anthropic Messages, and Gemini
`generateContent`. Each adapter uses the same bounded nonce challenge contract.
Unsupported channel types produce `unsupported_provider` rather than entering
the normal relay path. Adding another adapter cannot require a scheduler,
persistence, public API, or existing relay-path change.

## 6. Probe semantics

### 6.1 Endpoint ping

Ping matches check-cx transport semantics:

1. Resolve the configured endpoint to `scheme://host[:port]`.
2. Send `HEAD /` with redirects disabled and an 8-second timeout.
3. If transport fails, retry once with `GET /`.
4. Any received HTTP response proves transport reachability, even when its
   status is 401, 403, 404, or 5xx.
5. Record elapsed milliseconds or `null` on transport failure.

Ping runs concurrently with the conversation request and is diagnostic only.
It does not determine the target health state.

### 6.2 Conversation latency

Conversation latency follows check-cx's actual implementation, not its TTFT
comment: it measures from sending a minimal real model request until the full
bounded response has been consumed.

Each request contains a random nonce challenge and asks the model to return one
exact short token. Output is capped at 24 tokens. A response is successful only
when the normalized bounded output contains the expected nonce token. Provider
response bodies and generated content are never persisted.

State rules:

| State | Rule |
| --- | --- |
| `operational` | Challenge validated and conversation latency <= 6000 ms |
| `degraded` | Challenge validated and conversation latency > 6000 ms |
| `validation_failed` | A bounded response arrived but did not validate |
| `failed` | Empty response, timeout, network failure, or provider rejection |
| `unknown` | No completed observation |

There is no general retry for 401, 429, or 5xx. Cancellation releases response
bodies, connections, and goroutines. Errors are mapped to stable public codes;
raw provider bodies and credentials are discarded.

## 7. Scheduling and multi-instance behavior

The scheduler starts only when the master switch is enabled and validated
targets exist. Its first run begins on the next complete 60-second UTC slot.
It does not backfill missed slots.

Each target has an independent database lease. Acquisition is an atomic
compare-and-swap on `lease_until`; the lease duration is longer than the maximum
probe timeout and is renewed only while a probe is active. A process-local
non-reentrant guard skips a new cycle while its previous cycle is still active.
Expired leases are recoverable after process death.

A unique `(target_key, slot_started_at)` constraint is the final duplicate
barrier. When two instances observe the same slot, at most one can own the
target lease and at most one completed point can be stored. Work is bounded by
`PUBLIC_STATUS_PROBE_CONCURRENCY`. One target failure never cancels other
targets.

## 8. Persistence

### 8.1 `public_status_probe_results`

| Field | Purpose |
| --- | --- |
| `id` | Primary key |
| `target_key` | Stable public target identifier |
| `group_name` | Group snapshot |
| `display_name` | Display-name snapshot |
| `model_name` | Model snapshot |
| `channel_id` | Private audit field; never public |
| `slot_started_at` | UTC minute slot; unique with target key |
| `checked_at` | Completion Unix timestamp |
| `state` | Normalized public state |
| `ping_latency_ms` | Nullable endpoint latency |
| `chat_latency_ms` | Nullable full conversation latency |
| `error_code` | Bounded sanitized code only |

Indexes cover `(target_key, checked_at DESC)` and `checked_at`. Latencies cannot
be negative. The table has no foreign key to channels so deleting a channel
cannot break history queries.

### 8.2 `public_status_probe_leases`

One row per target stores owner id, lease expiry, and update time. It contains no
credential or request data. Rollback may leave both new tables in place because
older application images do not read them.

Retention cleanup runs at most once every 12 hours and deletes only rows older
than the configured retention. Public queries use an indexed per-target latest
60 query and never scan unbounded history.

## 9. Public API

`GET /api/status/probes` remains the only probe API. It is public, read-only,
rate limited, cached for 15 seconds, and supports ETag/304.

Response shape:

```json
{
  "success": true,
  "data": {
    "generated_at": 1786852274,
    "interval_seconds": 60,
    "targets": [
      {
        "key": "codex-gpt-5-5",
        "group": "codex",
        "display_name": "Codex",
        "model": "gpt-5.5",
        "state": "operational",
        "availability": 0.9833,
        "ping_latency_ms": 257,
        "chat_latency_ms": 5061,
        "latest_checked_at": 1786852214,
        "next_check_at": 1786852274,
        "history": [
          {
            "checked_at": 1786852214,
            "state": "operational",
            "ping_latency_ms": 257,
            "chat_latency_ms": 5061,
            "error_code": null
          }
        ]
      }
    ]
  }
}
```

History is oldest to newest and contains at most 60 real points. Availability is
successful (`operational` or `degraded`) points divided by completed points in
those 60 observations. The frontend pads missing points; the API never invents
timestamps or latency values.

The serializer is an explicit allowlist and never serializes database models.
Only localized public error labels are shown. Internal logs use sanitized codes
and target keys, not raw provider errors.

## 10. Public `/status` UI

The route keeps `PublicLayout` and New API's existing theme, spacing, shadcn
components, icons, and dark mode. It borrows the information architecture from
check-cx without copying its brand, filters, drag controls, or detail links.

The header contains the page title, aggregate status badge, last update time,
and an icon refresh button with an accessible label. The body is a responsive
grid: one column on mobile, two on tablet, and three on wide desktop.

Each target card contains:

1. display name, model name, and text/icon/color state badge;
2. current conversation latency and endpoint ping in stable two-column metrics;
3. availability over the latest 60 completed observations;
4. exactly 60 stable history segments, padded with unknown placeholders;
5. latest observation and next scheduled update time.

Segments run from past to now. Colors are green for operational, amber for
degraded, red for failed or validation failure, and gray for unknown. Color is
never the only state signal.

Desktop mouse hover and keyboard focus open a tooltip after 100 ms. Touch uses
tap-to-open and outside-tap/Escape to close. The detail contains local time,
state text, conversation latency, ping latency, and a localized sanitized error
label when present. Every segment is keyboard focusable with a complete
`aria-label`.

On narrow screens, the timeline has a stable minimum width, scrolls
horizontally, and starts at the newest edge. Auto-refresh preserves scroll,
focus, and the active point when that point still exists. It must not announce
every refresh or resize cards. Loading, empty, stale, partial, API error, and
manual refresh states have fixed dimensions and text equivalents.

## 11. Removal of the previous implementation

The implementation deletes or disconnects only prior group-probe additions:

- old group-probe scheduler and settings registration;
- administrator group-probe API routes;
- administrator group-probe settings components;
- 24-hour bucket public DTO and UI;
- probe-only options and branches added to the existing channel-test path.

Existing channel-test behavior is compared with the parent implementation from
before the group-probe feature. Affiliate commission and unrelated branch work
must remain intact. Old result tables and rows are left in place for rollback.

## 12. Verification gates

### 12.1 Isolation

Tests snapshot and compare, before and after successful, failed, and timed-out
probes:

- channel `status`, `test_time`, `response_time`, `channel_info`, and all other
  runtime state;
- multi-key order and current round-robin index with channel cache both enabled
  and disabled;
- user and subscription quota fields;
- consume-log count, hashes, and quota sums;
- original channel test and automatic monitoring task behavior.

All values must remain byte-for-byte equal. Public probing may write only its
result and lease tables.

### 12.2 Scheduling and data

- Two instances racing for one slot produce at most one point per target.
- A probe longer than one interval does not overlap or create backlog.
- Active requests never exceed the configured concurrency.
- Process death allows lease recovery without duplicate results.
- Ping and chat timeouts cancel at their configured boundaries.
- Queries return stable newest 60 results and correct availability.
- Public output contains no private fields or unbounded text.
- Malicious provider text cannot execute in the browser.

### 12.3 Frontend

Component and Playwright tests cover mouse hover, keyboard focus/Enter/Escape,
touch tap/outside close, refresh while a point is active, and loading/error/
empty/partial states. Screenshots at 320, 375, 768, and 1440 pixels must show no
overlap, clipping, unexpected page overflow, or layout shift. The management
pages are compared before and after and must have no probe UI.

### 12.4 Build and regression

Run focused Go model/client/scheduler/API tests, isolation integration tests,
`go test ./...`, and `go build ./...`. Run frontend unit tests, typecheck,
changed-file lint, route generation checks, and production build. Search the
final call graph to prove forbidden existing health and billing functions are
not referenced by the new module.

## 13. qiniu deployment and rollback

Before deployment:

1. preserve `.env`, all effective Compose files, application/image/container
   inspect data, and a verified MySQL logical dump;
2. record New API, MySQL, and Redis container ids, start times, and restart
   counts;
3. snapshot channel runtime fields, multi-key state, quota totals, consume-log
   totals, and original monitoring task state;
4. preserve the previous immutable New API image and exact rollback command.

Build an immutable candidate image labeled with the local commit. Add one
qiniu-only Compose override containing the candidate image and private probe
configuration. Deploy only with:

```bash
docker compose ... up -d --no-deps --no-build --pull never new-api
```

Acceptance requires a healthy container, HTTP 200 from `/api/status`,
`/api/status/probes`, and `/status`, zero panic/migration/unknown-column errors,
unchanged MySQL and Redis container identities, and unchanged isolation
snapshots after multiple probe cycles. Visual acceptance covers desktop and
mobile rendering plus tooltip interaction.

Rollback switches only New API to the preserved image and previous Compose
file set. The new tables remain inert; a database restore is required only when
the normal migration or data-integrity verification fails.

No GitHub push, force push, or pull request is allowed until the user confirms
the qiniu `/status` result.
