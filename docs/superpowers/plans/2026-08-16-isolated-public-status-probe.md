# Isolated Public Status Probe Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a 60-second public status probe that reports independent endpoint Ping and real conversation latency on `/status` without touching New API's existing channel-test, routing, multi-key, quota, billing, or logging behavior.

**Architecture:** A new `public_status_probe` service owns environment configuration, read-only channel snapshots, minimal provider clients, leases, scheduling, and result writes. The controller exposes one allowlisted public DTO; the React feature renders exactly 60 raw points in the existing public layout. The previous `group_probe` scheduler, administrator surfaces, and channel-test adaptations are disconnected while its historical database table remains inert for rollback.

**Tech Stack:** Go 1.22, Gin, GORM (SQLite/MySQL/PostgreSQL), React 19, TypeScript, TanStack Query/Router, Base UI, Tailwind CSS, Vitest/happy-dom, Playwright, Docker Compose.

---

## File Map

**New backend ownership**

- `setting/public_status_probe_setting/setting.go`: parse and validate environment-only settings and private target JSON.
- `setting/public_status_probe_setting/setting_test.go`: defaults, bounds, duplicate keys, invalid UTF-8, and secret-safe errors.
- `model/public_status_probe.go`: result/lease schemas and cross-database persistence methods.
- `model/public_status_probe_test.go`: uniqueness, latest-60 ordering, availability inputs, retention, and lease races.
- `service/public_status_probe/types.go`: private target snapshots, normalized states, adapter interfaces, and public service result types.
- `service/public_status_probe/client.go`: bounded HTTP transport, origin Ping, nonce challenge, body limits, and sanitized errors.
- `service/public_status_probe/openai.go`: independent Chat Completions and Responses adapters.
- `service/public_status_probe/anthropic.go`: independent Messages adapter.
- `service/public_status_probe/gemini.go`: independent `generateContent` adapter.
- `service/public_status_probe/client_test.go`: protocol requests/responses, challenge validation, Ping fallback, timeouts, and body limits.
- `service/public_status_probe/scheduler.go`: target loading, explicit key selection, slot loop, concurrency, leases, non-reentrancy, and retention cadence.
- `service/public_status_probe/scheduler_test.go`: duplicate suppression, skipped overlap, lease recovery, concurrency, unsupported providers, and isolation snapshots.
- `controller/public_status_probe.go`: public cache, ETag/304, explicit serializer, and API handler.
- `controller/public_status_probe_test.go`: public response contract, caching, ETag, empty/error states, and private-field exclusion.

**Backend integration and legacy disconnection**

- `main.go`: start the independent scheduler after database initialization.
- `model/main.go`: migrate the two new tables; retain legacy `GroupProbeResult` migration only as inert rollback compatibility.
- `router/api-router.go`: keep only public `GET /api/status/probes`; remove administrator group-probe routes.
- `router/api_router_test.go`: assert the public route exists and old administrator routes do not.
- `controller/system_task_handlers.go`: unregister the old `groupProbeHandler`.
- `model/system_task.go`: remove the old `SystemTaskTypeGroupProbe` constant.
- `controller/channel-test.go`: restore the pre-group-probe `testChannel` implementation and remove probe-only options.
- `controller/channel_test_internal_test.go`: remove tests for probe-only options and preserve ordinary channel-test regression coverage.
- Delete `controller/group_probe.go`, `controller/group_probe_test.go`, `controller/group_probe_task.go`, and `controller/group_probe_task_test.go`.
- Delete `setting/group_probe_setting/` and remove its handling from `model/option.go` and option tests.
- Keep `model/group_probe_result.go` and its table migration inert; remove no rows and issue no drop migration.

**Frontend ownership**

- `web/src/features/group-probe-status/types.ts`: replace bucket DTOs with target/history DTOs containing both latencies.
- `web/src/features/group-probe-status/api.ts`: typed fetch with 15-second query freshness and conditional refresh support.
- `web/src/features/group-probe-status/history.ts`: deterministic oldest-to-newest 60-slot padding and identity helpers.
- `web/src/features/group-probe-status/components/status-timeline.tsx`: 60 stable focusable segments and responsive hover/focus/tap details.
- `web/src/features/group-probe-status/components/group-status-row.tsx`: target card with dual current metrics and availability.
- `web/src/features/group-probe-status/content.tsx`: aggregate header, refresh state, responsive grid, fixed loading/error/empty states.
- `web/src/features/group-probe-status/index.tsx`: retain `PublicLayout` integration only.
- `web/src/features/group-probe-status/__tests__/api.test.ts`: response validation and cache semantics.
- `web/src/features/group-probe-status/__tests__/status-page.test.tsx`: dual latency, 60 points, pointer/keyboard/touch behavior, refresh preservation, and error states.
- Delete `web/src/features/system-settings/operations/group-probe/` and unregister it from `web/src/features/system-settings/operations/section-registry.tsx`.
- Update flat locale JSON files for user-visible public status strings and remove old management-only strings when no longer referenced.

### Task 1: Disconnect the Previous Probe and Restore Channel-Test

- [ ] **Step 1: Add route and registration regression tests**

In `router/api_router_test.go`, assert `GET /api/status/probes` is registered without administrator middleware and all four `/api/group-probe/*` routes return 404. In `controller/channel_test_internal_test.go`, remove the option-structure tests and retain a direct ordinary channel-test test that asserts consume logging remains enabled by the unchanged path.

- [ ] **Step 2: Run tests and verify the old surface still fails the new contract**

Run:

```powershell
go test ./router ./controller -run "PublicStatusProbe|GroupProbeAdminRoutes|ChannelTest" -count=1
```

Expected: FAIL because administrator routes and the old system-task handler still exist.

- [ ] **Step 3: Restore the original channel-test boundary and remove registrations**

Restore `controller/channel-test.go` probe-only hunks to the `151fc1c^` behavior:

```go
func testChannel(ctx context.Context, channel *model.Channel, testUserID int, testModel string, endpointType string, isStream bool) testResult {
    // Existing implementation body remains unchanged.
}
```

Remove `channelTestOptions`, `defaultChannelTestOptions`, `groupProbeChannelTestOptions`, and `testChannelWithOptions`. Remove `groupProbeHandler` registration and system-task type, delete the legacy controller/task/settings files, remove administrator routes/settings UI, and remove `group_probe_setting` from option validation/application. Do not delete or rewrite `group_probe_results`.

- [ ] **Step 4: Prove no old executable entry point remains**

Run:

```powershell
git grep -n -E "groupProbeHandler|testChannelWithOptions|groupProbeChannelTestOptions|/group-probe|GroupProbeSettingsSection"
```

Expected: no executable-code matches; only superseded documentation and inert legacy model references may remain.

- [ ] **Step 5: Run focused regressions and commit**

```powershell
go test ./controller ./router ./model -count=1
git add controller model router setting web/src/features/system-settings
git commit -m "refactor: isolate public probes from channel testing"
```

### Task 2: Add Environment-Only Configuration

- [ ] **Step 1: Write table-driven configuration tests**

Cover exact defaults, all numeric boundaries, maximum 20 targets, stable-key uniqueness, required strings, `channel_id > 0`, `key_index >= 0`, UTF-8 validity, per-field rune limits, JSON size limit, and an error string that never includes the supplied JSON or key material.

```go
func TestLoadRejectsDuplicateTargetKeysWithoutEchoingInput(t *testing.T) {
    t.Setenv("PUBLIC_STATUS_PROBE_TARGETS", `[...duplicate keys...]`)
    _, err := Load()
    require.Error(t, err)
    assert.NotContains(t, err.Error(), "secret-marker")
}
```

- [ ] **Step 2: Run the new package test and verify it fails**

```powershell
go test ./setting/public_status_probe_setting -count=1
```

Expected: FAIL because `Load` and types do not exist.

- [ ] **Step 3: Implement immutable settings**

Define:

```go
type Target struct {
    Key         string `json:"key"`
    Group       string `json:"group"`
    DisplayName string `json:"display_name"`
    Model       string `json:"model"`
    ChannelID   int    `json:"channel_id"`
    KeyIndex    int    `json:"key_index"`
}

type Setting struct {
    Enabled          bool
    Interval         time.Duration
    PingTimeout      time.Duration
    ChatTimeout      time.Duration
    DegradedLatency  time.Duration
    Concurrency      int
    RetentionDays    int
    Targets          []Target
}
```

Use `common.Unmarshal` and existing environment helpers. Return one bounded generic configuration error; never log or include `PUBLIC_STATUS_PROBE_TARGETS` content.

- [ ] **Step 4: Run and commit**

```powershell
go test ./setting/public_status_probe_setting -count=1
git add setting/public_status_probe_setting
git commit -m "feat: configure isolated public status probes"
```

### Task 3: Add Cross-Database Result and Lease Persistence

- [ ] **Step 1: Write database contract tests**

Create isolated SQLite fixtures and assert:

```go
require.NoError(t, db.AutoMigrate(&PublicStatusProbeResult{}, &PublicStatusProbeLease{}))
require.NoError(t, CreatePublicStatusProbeResult(first))
assert.Error(t, CreatePublicStatusProbeResult(duplicateSlot))
rows, err := GetLatestPublicStatusProbeResults("target-a", 60)
require.NoError(t, err)
assert.Len(t, rows, 60)
assert.Less(t, rows[0].CheckedAt, rows[59].CheckedAt)
```

Also race two lease acquisitions, verify only one owner, verify expired-owner takeover, and verify retention deletes only old public probe rows.

- [ ] **Step 2: Implement schemas and GORM methods**

Use unique/index tags compatible with all databases:

```go
type PublicStatusProbeResult struct {
    ID             int64  `gorm:"primaryKey"`
    TargetKey      string `gorm:"size:96;uniqueIndex:idx_public_probe_slot;index:idx_public_probe_latest,priority:1"`
    SlotStartedAt  int64  `gorm:"uniqueIndex:idx_public_probe_slot"`
    CheckedAt      int64  `gorm:"index;index:idx_public_probe_latest,priority:2,sort:desc"`
    // Snapshot/state/nullable latency/sanitized code fields.
}
```

Lease acquisition must be one transaction using `lockForUpdate(tx)` for MySQL/PostgreSQL and SQLite-compatible transaction semantics. Result insertion treats a duplicate unique key as an already-completed slot, not a retry signal.

- [ ] **Step 3: Register migrations while preserving legacy data**

Add both new structs to normal and fast migration lists in `model/main.go`. Leave `GroupProbeResult` in place so rollback images and old data remain valid.

- [ ] **Step 4: Run all model tests and commit**

```powershell
go test ./model -count=1
git add model/public_status_probe.go model/public_status_probe_test.go model/main.go
git commit -m "feat: persist public status probe observations"
```

### Task 4: Build the Independent Probe Transport

- [ ] **Step 1: Write HTTP transport tests**

With `httptest.Server`, cover HEAD success, HEAD transport failure followed by GET, redirects disabled, any HTTP status counted as reachable, malformed base URL, 8-second cancellation via injected short timeout, 1 MiB response cap, and closure of response bodies.

Define the adapter contract in tests:

```go
type Adapter interface {
    Probe(context.Context, Snapshot, Challenge) (string, error)
}

type Challenge struct {
    Expected string
    Prompt   string
}
```

- [ ] **Step 2: Implement bounded client and sanitizer**

Create dedicated `http.Client` instances with bounded transport settings, redirects rejected, context timeouts, and `io.LimitReader`. Resolve Ping to only `scheme://host[:port]/`, run HEAD first and GET only on transport failure, and consider every received response reachable.

Generate the expected token with `crypto/rand`, require exact normalized containment, discard all provider response text, and map failures only to:

```go
const (
    ErrorUnsupportedProvider = "unsupported_provider"
    ErrorTimeout             = "timeout"
    ErrorNetwork             = "network_error"
    ErrorProviderRejected    = "provider_rejected"
    ErrorEmptyResponse       = "empty_response"
    ErrorValidationFailed    = "validation_failed"
    ErrorInvalidTarget       = "invalid_target"
)
```

- [ ] **Step 3: Run and commit**

```powershell
go test ./service/public_status_probe -run "Ping|Challenge|Sanitize" -count=1
git add service/public_status_probe
git commit -m "feat: add bounded public probe transport"
```

### Task 5: Implement Four Minimal Provider Adapters

- [ ] **Step 1: Write provider request and response tests**

For each adapter, assert URL path, authentication headers, model, nonce prompt, non-streaming mode, and output limit `24`. Test valid and malformed payloads, provider non-2xx responses, empty outputs, malicious large text, and nonce mismatch.

- [ ] **Step 2: Implement explicit adapter registry**

The registry must select only known channel types/protocols:

```go
func adapterFor(snapshot Snapshot) (Adapter, error) {
    switch snapshot.Protocol {
    case ProtocolOpenAIChat:
        return openAIChatAdapter{}, nil
    case ProtocolOpenAIResponses:
        return openAIResponsesAdapter{}, nil
    case ProtocolAnthropicMessages:
        return anthropicMessagesAdapter{}, nil
    case ProtocolGeminiGenerateContent:
        return geminiAdapter{}, nil
    default:
        return nil, codedError(ErrorUnsupportedProvider)
    }
}
```

Do not import relay packages and do not add fallback to channel test or normal routing. Use `common.Marshal`, `common.Unmarshal`, and `common.DecodeJson` for JSON operations.

- [ ] **Step 3: Prove forbidden calls are absent and commit**

```powershell
git grep -n -E "testChannel|SetupContextForSelectedChannel|CacheGetRandomSatisfiedChannel|GetNextEnabledKey|SaveChannelInfo|RecordConsumeLog" -- service/public_status_probe
go test ./service/public_status_probe -run "OpenAI|Responses|Anthropic|Gemini" -count=1
git add service/public_status_probe
git commit -m "feat: probe supported providers independently"
```

Expected grep output: empty.

### Task 6: Add Read-Only Target Loading and Slot Scheduler

- [ ] **Step 1: Write loader and scheduler tests**

Assert a configured channel is copied with only type, base URL, organization, model mapping/protocol fields, and explicitly indexed key. Missing/disabled/deleted channels, out-of-range key indexes, and unsupported types create sanitized failed points and never select another channel.

Test scheduler contracts with a fake clock/repository/client:

- first run starts at the next complete UTC interval slot;
- two schedulers racing one target/slot store at most one row;
- a still-running local cycle causes the next tick to be skipped;
- no missed slot is backfilled;
- maximum active work equals configured concurrency;
- one target failure does not cancel siblings;
- expired lease is recoverable after owner death;
- retention executes at most once per 12 hours.

- [ ] **Step 2: Implement immutable snapshot loading**

Query the configured channel ID directly with `model.DB.Select(...)`. Copy the channel and parse keys without calling `GetNextEnabledKey`; choose `keys[target.KeyIndex]` from the local copy. Never write the channel or cache.

- [ ] **Step 3: Implement the scheduler**

`Start(ctx, setting)` returns immediately when disabled/invalid/empty. Otherwise, wait until `now.UTC().Truncate(interval).Add(interval)`, guard the whole cycle with `atomic.Bool`, and process targets through a fixed semaphore. Run Ping and conversation concurrently under their own timeouts, derive health only from conversation validation, write one point, and release the target lease.

- [ ] **Step 4: Add process startup wiring and commit**

In `main.go`, load configuration once after DB migration, log only a bounded disabled/configuration status, and call the service start function. Never register it in the normal system-task framework.

```powershell
go test ./service/public_status_probe ./setting/public_status_probe_setting -count=1
git add main.go service/public_status_probe
git commit -m "feat: schedule isolated public status probes"
```

### Task 7: Expose the Allowlisted Public API

- [ ] **Step 1: Write controller contract tests**

Assert exact response fields, oldest-to-newest history, at most 60 real observations, availability `(operational + degraded) / completed`, latest values, next UTC slot, 15-second cache, ETag/304, and rate limiting. Marshal the response and assert it never contains `channel_id`, `key_index`, `base_url`, `key`, credentials, raw response, or stack text.

- [ ] **Step 2: Implement controller DTOs and cache**

Use explicit DTO structs rather than model serialization:

```go
type publicProbePointDTO struct {
    CheckedAt      int64  `json:"checked_at"`
    State          string `json:"state"`
    PingLatencyMS  *int64 `json:"ping_latency_ms"`
    ChatLatencyMS  *int64 `json:"chat_latency_ms"`
    ErrorCode      *string `json:"error_code"`
}
```

Build a stable ETag from serialized public data, honor `If-None-Match`, set `Cache-Control: public, max-age=15`, and return an empty target array when probing is disabled or has no configured target.

- [ ] **Step 3: Register only the public route and commit**

```powershell
go test ./controller ./router -run "PublicStatusProbe|StatusProbeRoute" -count=1
git add controller/public_status_probe.go controller/public_status_probe_test.go router
git commit -m "feat: publish isolated status probe history"
```

### Task 8: Replace the Frontend Data Contract and History Padding

- [ ] **Step 1: Write TypeScript data tests**

Cover successful decoding, nullable dual latency, unknown state handling, oldest-to-newest ordering, fewer than 60 real points padded on the left, and stable point identities based on target key plus `checked_at`.

```ts
expect(padProbeHistory(history)).toHaveLength(60)
expect(padProbeHistory(history).at(-1)?.checked_at).toBe(latest.checked_at)
```

- [ ] **Step 2: Replace bucket types and API hook**

Define `PublicProbePoint`, `PublicProbeTarget`, and `PublicProbeData` matching the backend exactly. Set TanStack Query `staleTime` to 15 seconds and polling to 60 seconds; manual refresh invalidates the query without destroying current data.

- [ ] **Step 3: Run tests and commit**

```powershell
Set-Location web
bun test src/features/group-probe-status/__tests__/api.test.ts
bun run typecheck
git add src/features/group-probe-status
git commit -m "refactor: consume raw public probe observations"
```

### Task 9: Build the Responsive 60-Point Public Status UI

- [ ] **Step 1: Write interaction tests first**

Render cards and assert:

- current conversation and Ping latency are distinct;
- availability uses latest 60 completed points;
- exactly 60 segment buttons exist per target;
- hover opens details after 100 ms;
- focus/Enter opens and Escape closes;
- touch tap opens and outside tap closes;
- detail shows local time, state text, both latencies, and localized sanitized reason;
- refresh preserves active point, focus, and horizontal scroll when identity survives;
- loading, empty, partial, stale, and API error views retain fixed dimensions.

- [ ] **Step 2: Implement stable accessible components**

Use existing New API theme tokens, `PublicLayout`, Base UI tooltip/popover primitives, and Hugeicons/Lucide already in the app. Use a one/two/three-column responsive grid, fixed two-column metric tracks, and a horizontally scrollable 60-segment timeline with a stable minimum width. Each segment is a real focusable button with a complete translated `aria-label`; color is paired with state text and icon.

- [ ] **Step 3: Synchronize locales and remove management registration**

Add all public strings to every locale through the repository sync script, remove the operations-section import/entry, and delete `web/src/features/system-settings/operations/group-probe/`.

- [ ] **Step 4: Run frontend verification and commit**

```powershell
Set-Location web
bun test src/features/group-probe-status
bun run i18n:sync
bun run typecheck
bun run lint
bun run build
git add src
git commit -m "feat: show dual-latency public status history"
```

### Task 10: Add Isolation and Full Regression Gates

- [ ] **Step 1: Add before/after isolation integration tests**

Create fixtures with cache both enabled and disabled. Snapshot channel rows including status/test time/response time/key/channel info, multi-key polling index/order, users/subscriptions quotas, consume-log count/hash/sums, quota data, and original monitor task state. Execute successful, failed, validation-failed, unsupported, and timed-out probes; compare every snapshot byte-for-byte. The only changed tables must be `public_status_probe_results` and `public_status_probe_leases`.

- [ ] **Step 2: Run forbidden-call and write-surface scans**

```powershell
git grep -n -E "testChannel|testChannelWithOptions|SetupContextForSelectedChannel|CacheGetRandomSatisfiedChannel|GetNextEnabledKey|SaveChannelInfo|RecordConsumeLog|UpdateResponseTime" -- service/public_status_probe controller/public_status_probe.go
git grep -n -E "GroupProbeSettingsSection|/group-probe" -- web/src router controller
```

Expected: no matches.

- [ ] **Step 3: Run complete backend verification**

```powershell
go test ./... -count=1
go build ./...
Set-Location relaykit
$env:GOWORK='off'; go build ./...
```

- [ ] **Step 4: Run complete frontend and visual verification**

```powershell
Set-Location web
bun run typecheck
bun run lint
bun run format:check
bun run copyright:check
bun run build
```

Start the local application and use Playwright at widths 320, 375, 768, and 1440. Capture screenshots and assert no page overflow, clipped text, overlapping controls, timeline layout shift, blank content, or management-page probe UI. Exercise mouse, keyboard, and touch interaction.

- [ ] **Step 5: Commit verification fixes only after all gates pass**

```powershell
git status --short
git add <only-files-changed-for-verification>
git commit -m "test: verify public probe isolation"
```

### Task 11: Back Up and Deploy Only `new-api` to qiniu

- [ ] **Step 1: Reverify live topology read-only**

Through Xterminal, record the effective Compose files, container IDs/images/start times/restart counts/health, mounted paths, `.env` checksum, and current `/api/status`, `/api/status/probes`, and `/status` responses. Abort if the application directory or MySQL/Redis identities differ from the recorded topology.

- [ ] **Step 2: Create a fresh timestamped rollback directory**

Preserve `.env`, every effective Compose file, `docker compose config`, `docker inspect` for all three services, the exact current immutable New API image reference, and current container metadata. Create and verify a MySQL logical dump without restarting or recreating MySQL.

- [ ] **Step 3: Capture isolation baselines**

Run read-only SQL snapshots/hashes for channel runtime fields, multi-key `channel_info`, user/subscription quota totals, consume-log counts/hashes/quota sums, quota data, and original monitoring-task state. Store commands and output in the rollback directory with credentials redacted.

- [ ] **Step 4: Build and configure an immutable candidate**

Transfer the tested source archive or build context, build `local/new-api:<local-commit>`, and write a qiniu-only override containing only the candidate image and private `PUBLIC_STATUS_PROBE_*` variables. Validate `docker compose config` before deployment and ensure target JSON never appears in logs or returned API data.

- [ ] **Step 5: Recreate only the application service**

Use exactly:

```bash
docker compose -f docker-compose.yml -f docker-compose.qiniu-probe.yml up -d --no-deps --no-build --pull never new-api
```

Do not run a project-wide `up`, `restart`, or `down`. Do not restart or recreate MySQL or Redis.

- [ ] **Step 6: Verify and rollback on any gate failure**

Require healthy New API, HTTP 200 from `/api/status`, `/api/status/probes`, and `/status`, no panic/migration/unknown-column/credential logs, unchanged MySQL and Redis container identity/start/restart metadata, and stable channel/quota/log snapshots after at least three 60-second cycles. On failure, point the override back to the preserved image and run the same `up -d --no-deps --no-build --pull never new-api` command.

### Task 12: User Acceptance and GitHub Hold

- [ ] **Step 1: Present the qiniu status page for acceptance**

Provide `http://111.62.241.77:3000/status`, deployment commit/image, backup directory, container identity comparison, three-cycle probe evidence, and rollback command.

- [ ] **Step 2: Keep GitHub untouched**

Run:

```powershell
git status --short --branch
git log origin/feature/affiliate-commission..HEAD --oneline
```

Report local commits only. Do not push, force-push, create a PR, or alter the GitHub branch until the user explicitly approves the qiniu result.
