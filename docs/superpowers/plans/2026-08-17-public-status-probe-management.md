# Public Status Probe Management Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a Root-only public status probe configuration page with versioned target CRUD and 60-second runtime hot reload, then deploy the verified result only to qiniu.

**Architecture:** Store one strict, versioned `PublicStatusProbeConfig` JSON document in the existing Option table. Publish validated immutable snapshots to the scheduler and public controller, use database compare-and-swap for mutations, and retain the isolated probe transport, lease, result, and `/status` contracts. A focused React Operations section consumes sanitized Root APIs and never receives channel secrets.

**Tech Stack:** Go 1.22, Gin, GORM with SQLite/MySQL/PostgreSQL, React 19, TypeScript, TanStack Query/Router, React Hook Form, Zod, Base UI, Tailwind CSS, Vitest, Playwright, Docker Compose.

---

## File Map

**Backend configuration and runtime**

- Create `setting/public_status_probe_setting/config.go` for the canonical document, strict codec, validation, cloning, and runtime snapshot.
- Modify `setting/public_status_probe_setting/setting.go` to retain environment bootstrap parsing and add per-target enabled state.
- Create `model/public_status_probe_config.go` for compare-and-create import and exact-value CAS.
- Modify `model/option.go` and `main.go` for validation, synchronization, bootstrap, publish hooks, and startup.
- Modify `service/public_status_probe/scheduler.go` and `loader.go` for per-slot snapshots and read-only target validation.
- Modify `controller/public_status_probe.go` for canonical public reads and version-aware cache invalidation.
- Create `controller/public_status_probe_admin.go` and register Root routes in `router/api-router.go`.

**Frontend management module**

- Create `web/src/features/system-settings/operations/public-status-probe/` with `types.ts`, `api.ts`, `validation.ts`, `index.tsx`, focused components, and tests.
- Modify `web/src/features/system-settings/operations/section-registry.tsx` for the independent Operations entry.
- Update locale files through `bun run i18n:sync` and extend the existing affiliate/probe localization test.

**Deployment**

- Store local screenshots and redacted manifests under ignored `deploy-artifacts/` paths.
- Create a timestamped rollback directory and qiniu-only Compose override on the server; never commit server secrets.

## Task 1: Canonical Document And Runtime Snapshot

**Files:**
- Create: `setting/public_status_probe_setting/config.go`
- Modify: `setting/public_status_probe_setting/setting.go`
- Create: `setting/public_status_probe_setting/config_test.go`
- Test: `setting/public_status_probe_setting/setting_test.go`

- [ ] **Step 1: Write failing strict-document tests**

Cover defaults, every numeric boundary, unknown fields, unknown schema versions,
duplicate keys, 20-target limit, trimming, array order, and deep cloning.

```go
func TestDecodeDocumentRejectsUnknownField(t *testing.T) {
    _, err := DecodeDocument(`{"schema_version":1,"version":1,"enabled":true,"targets":[],"secret":"x"}`)
    require.Error(t, err)
    assert.NotContains(t, err.Error(), "secret")
}

func TestPublishDocumentReturnsIndependentSnapshots(t *testing.T) {
    doc := DefaultDocument()
    doc.Targets = []Target{{Key: "target-a", Enabled: true, Group: "codex", DisplayName: "Codex", Model: "gpt-5.5", Protocol: ProtocolOpenAIChat, ChannelID: 23}}
    require.NoError(t, PublishDocument(doc))
    first := CurrentDocument()
    first.Targets[0].Group = "changed"
    assert.Equal(t, "codex", CurrentDocument().Targets[0].Group)
}
```

- [ ] **Step 2: Verify the tests fail**

```powershell
go test ./setting/public_status_probe_setting -run "Document|Publish|Environment" -count=1
```

Expected: FAIL because the document and runtime functions do not exist.

- [ ] **Step 3: Implement the stable contracts**

```go
const (
    OptionKey     = "PublicStatusProbeConfig"
    SchemaVersion = 1
)

type Document struct {
    SchemaVersion      int      `json:"schema_version"`
    Version            int64    `json:"version"`
    Enabled            bool     `json:"enabled"`
    PingTimeoutSeconds int      `json:"ping_timeout_seconds"`
    ChatTimeoutSeconds int      `json:"chat_timeout_seconds"`
    DegradedLatencyMS  int      `json:"degraded_latency_ms"`
    Concurrency        int      `json:"concurrency"`
    RetentionDays      int      `json:"retention_days"`
    Targets            []Target `json:"targets"`
}

func DefaultDocument() Document
func DecodeDocument(raw string) (Document, error)
func EncodeDocument(Document) (string, error)
func ValidateAndNormalizeDocument(Document) (Document, error)
func PublishDocument(Document) error
func CurrentDocument() Document
func CurrentSetting() Setting
func LoadEnvironmentDocument() (Document, error)
```

Add `Enabled bool` to `Target`. Decode first to an allowlisted
`map[string]json.RawMessage` with `common.Unmarshal`, then decode the typed value.
Use one bounded generic error that never echoes JSON. Protect a deep-cloned runtime
document with `sync.RWMutex`. `CurrentSetting` filters disabled targets and always
sets `Interval: time.Minute`; imported environment targets default to enabled.

- [ ] **Step 4: Format, test, and commit**

```powershell
gofmt -w setting/public_status_probe_setting
go test ./setting/public_status_probe_setting -count=1
git add setting/public_status_probe_setting
git commit -m "feat: define managed public probe configuration"
```

Expected: PASS and one local commit.

## Task 2: Atomic Option Import, CAS, And Synchronization

**Files:**
- Create: `model/public_status_probe_config.go`
- Create: `model/public_status_probe_config_test.go`
- Modify: `model/option.go`
- Modify: `main.go`

- [ ] **Step 1: Write failing SQLite contracts**

Prove import happens only when the Option is absent, existing disabled/empty
documents are not re-imported, concurrent create has one winner, stale versions
return conflict, and failed writes do not publish.

```go
func TestCompareAndSwapPublicStatusProbeConfigRejectsStaleVersion(t *testing.T) {
    saved, _, err := EnsurePublicStatusProbeConfig(publicstatusprobesetting.DefaultDocument())
    require.NoError(t, err)
    _, err = CompareAndSwapPublicStatusProbeConfig(saved.Version+1, func(next *publicstatusprobesetting.Document) error {
        next.Enabled = true
        return nil
    })
    require.ErrorIs(t, err, ErrPublicStatusProbeConfigConflict)
}
```

- [ ] **Step 2: Verify model tests fail**

```powershell
go test ./model -run "PublicStatusProbeConfig" -count=1
```

Expected: FAIL with missing persistence functions.

- [ ] **Step 3: Implement portable persistence**

```go
var ErrPublicStatusProbeConfigConflict = errors.New("public status probe configuration conflict")

func EnsurePublicStatusProbeConfig(bootstrap publicstatusprobesetting.Document) (publicstatusprobesetting.Document, bool, error)
func GetPublicStatusProbeConfig() (publicstatusprobesetting.Document, error)
func CompareAndSwapPublicStatusProbeConfig(expectedVersion int64, mutate func(*publicstatusprobesetting.Document) error) (publicstatusprobesetting.Document, error)
func ApplyPublicStatusProbeConfigOption(raw string) error
```

Use `clause.OnConflict{DoNothing:true}` for first import. CAS reads and decodes the
exact current raw value, checks `version`, mutates a clone, increments once, then
updates with `WHERE key = ? AND value = ?`. Require `RowsAffected == 1`; otherwise
return conflict. Publish only after commit succeeds.

- [ ] **Step 4: Connect Option validation and remote publish**

In `model/option.go`, validate `PublicStatusProbeConfig` with `DecodeDocument` and
call `ApplyPublicStatusProbeConfigOption` from `updateOptionMap`. Add a setting-level
publish hook so model code does not import controllers:

```go
func SetPublishHook(hook func(version int64))
```

An invalid synchronized value leaves the previous snapshot active.

- [ ] **Step 5: Bootstrap after resources initialize, before scheduling**

```go
bootstrap, err := publicstatusprobesetting.LoadEnvironmentDocument()
if err != nil {
    common.SysLog("public status probe bootstrap disabled: invalid configuration")
    bootstrap = publicstatusprobesetting.DefaultDocument()
}
document, _, err := model.EnsurePublicStatusProbeConfig(bootstrap)
if err != nil {
    common.FatalLog("failed to initialize public status probe configuration")
    return
}
if err := publicstatusprobesetting.PublishDocument(document); err != nil {
    common.FatalLog("failed to publish public status probe configuration")
    return
}
```

Never log raw Option or environment values.

- [ ] **Step 6: Format, test, and commit**

```powershell
gofmt -w model/public_status_probe_config.go model/public_status_probe_config_test.go model/option.go main.go
go test ./model ./setting/public_status_probe_setting -run "PublicStatusProbeConfig|Document|Option" -count=1
go test ./model -count=1
git add model/public_status_probe_config.go model/public_status_probe_config_test.go model/option.go main.go
git commit -m "feat: persist public probe configuration atomically"
```

Expected: PASS.

The persistence tests use real SQLite plus GORM dry-run statements for MySQL and
PostgreSQL to verify that compare-and-create and exact-value CAS generate valid
portable SQL. Task 10 repeats the mutation contract against qiniu's real MySQL
before acceptance.

## Task 3: Per-Minute Scheduler Snapshots

**Files:**
- Modify: `service/public_status_probe/scheduler.go`
- Modify: `service/public_status_probe/scheduler_test.go`
- Modify: `main.go`

- [ ] **Step 1: Write failing dynamic-setting tests**

Test that one slot uses one snapshot, updates apply next slot, disabled targets are
skipped, global disable does not stop the scheduler, and retention runs while
disabled or empty.

```go
type fakeSettingProvider struct {
    mu      sync.Mutex
    setting publicstatusprobesetting.Setting
}

func (provider *fakeSettingProvider) CurrentSetting() publicstatusprobesetting.Setting {
    provider.mu.Lock()
    defer provider.mu.Unlock()
    return cloneTestSetting(provider.setting)
}
```

- [ ] **Step 2: Verify failure**

```powershell
go test ./service/public_status_probe -run "Reload|DisabledRetention|Snapshot" -count=1
```

Expected: FAIL because `Scheduler` owns one startup setting.

- [ ] **Step 3: Implement one lifetime scheduler**

```go
type SettingProvider interface {
    CurrentSetting() publicstatusprobesetting.Setting
}

type runtimeSettingProvider struct{}
func (runtimeSettingProvider) CurrentSetting() publicstatusprobesetting.Setting {
    return publicstatusprobesetting.CurrentSetting()
}
```

`NewScheduler` receives the provider. At each UTC minute, `runSlot` reads one
snapshot and uses it for targets, limits, and timeouts. Skip target work when
disabled, but run retention from the same snapshot. A save during a running slot
does not cancel leased work. Change `Start(ctx)` to keep one scheduler alive and
preserve the existing shutdown done channel.

- [ ] **Step 4: Format, test, and commit**

```powershell
gofmt -w service/public_status_probe/scheduler.go service/public_status_probe/scheduler_test.go main.go
go test ./service/public_status_probe -count=1
go test ./controller ./model -run "PublicStatusProbe" -count=1
git add service/public_status_probe/scheduler.go service/public_status_probe/scheduler_test.go main.go
git commit -m "feat: reload public probe targets each minute"
```

Expected: PASS.

## Task 4: Canonical Public Output And Versioned Cache

**Files:**
- Modify: `controller/public_status_probe.go`
- Modify: `controller/public_status_probe_test.go`
- Modify: `main.go`

- [ ] **Step 1: Write failing public behavior tests**

Require managed target order, enabled-only output, disabled/deleted historical
targets remaining hidden, edited target keys retaining history, and changed
configuration versions rebuilding body and ETag.

```go
func TestBuildPublicStatusProbeResponseUsesManagedTargetOrder(t *testing.T) {
    publishTestDocument(t, 7, []Target{target("second"), target("first")})
    response, err := buildPublicStatusProbeResponse(time.Unix(120, 0))
    require.NoError(t, err)
    assert.Equal(t, "second", response.Data.Targets[0].Key)
    assert.Equal(t, "first", response.Data.Targets[1].Key)
}
```

- [ ] **Step 2: Verify failure**

```powershell
go test ./controller -run "PublicStatusProbe.*Managed|ConfigVersion|Disabled" -count=1
```

Expected: FAIL because the controller calls environment `Load()`.

- [ ] **Step 3: Implement canonical reads and cache invalidation**

Read `CurrentDocument`/`CurrentSetting`, add `configVersion int64` to the cache
entry, and require a version match. Export:

```go
func InvalidatePublicStatusProbeCache(_ int64) {
    publicStatusProbeCacheMu.Lock()
    publicStatusProbeCache = publicStatusProbeCacheEntry{}
    publicStatusProbeCacheMu.Unlock()
}
```

Register it through `SetPublishHook` in `main.go`. Query history by immutable key
and remove the current channel/model equality filter so edits preserve the last
60 observations. Public DTOs remain unchanged and private selectors remain absent.

- [ ] **Step 4: Format, test, and commit**

```powershell
gofmt -w controller/public_status_probe.go controller/public_status_probe_test.go main.go
go test ./controller ./router -run "PublicStatusProbe|StatusProbeRoute" -count=1
git add controller/public_status_probe.go controller/public_status_probe_test.go main.go
git commit -m "refactor: serve managed public probe targets"
```

Expected: PASS, including ETag/304 and redaction tests.

## Task 5: Root Configuration And Target CRUD APIs

**Files:**
- Create: `controller/public_status_probe_admin.go`
- Create: `controller/public_status_probe_admin_test.go`
- Modify: `service/public_status_probe/loader.go`
- Modify: `service/public_status_probe/loader_test.go`
- Modify: `router/api-router.go`
- Modify: `router/api_router_test.go`

- [ ] **Step 1: Write failing read-only validation tests**

Cover channel existence/enabled status, protocol match, configured model,
multi-key index, proxies/overrides, and byte-equal channel/cache state before and
after validation.

```go
func TestValidateTargetDoesNotMutateChannel(t *testing.T) {
    before := loadChannelFixture(t, 23)
    require.NoError(t, validator.ValidateTarget(context.Background(), validTarget()))
    assert.Equal(t, before, loadChannelFixture(t, 23))
}
```

- [ ] **Step 2: Add the read-only validator**

```go
type TargetValidator interface {
    ValidateTarget(context.Context, publicstatusprobesetting.Target) error
}

func (loader *DBTargetLoader) ValidateTarget(ctx context.Context, target publicstatusprobesetting.Target) error {
    _, err := loader.Load(ctx, target)
    return err
}
```

Select only additional fields required to verify model availability. Never return
the loaded secret snapshot from the admin controller.

- [ ] **Step 3: Write failing exact API and auth tests**

Assert sanitized GET, 401/403, RootAuth on every method, body-size and unknown-field
rejection, 400 validation, 404 missing key, 409 stale version, generated immutable
keys, stable order, CRUD, and absence of channel secrets.

```go
func TestCreatePublicStatusProbeTargetIgnoresClientKey(t *testing.T) {
    response := performRootJSON(t, http.MethodPost, "/api/public-status-probe/targets", createBodyWithKey("chosen-by-client"))
    assert.Equal(t, http.StatusOK, response.Code)
    assert.NotContains(t, response.Body.String(), "chosen-by-client")
}
```

- [ ] **Step 4: Implement strict DTOs and CAS mutations**

The protected GET returns global fields, target fields, version, and safe channel
fields only: `id`, `name`, `type`, `status`, `models`, `is_multi_key`, and
`key_count`. Decode through `io.LimitReader` and a JSON field allowlist.

Implement:

```go
func GetPublicStatusProbeConfig(c *gin.Context)
func UpdatePublicStatusProbeConfig(c *gin.Context)
func CreatePublicStatusProbeTarget(c *gin.Context)
func UpdatePublicStatusProbeTarget(c *gin.Context)
func DeletePublicStatusProbeTarget(c *gin.Context)
```

Every write calls `CompareAndSwapPublicStatusProbeConfig`. Generate keys as
`probe-` plus `uuid.NewString()`, preserve path key on update, and require positive
`version` query on DELETE. Emit bounded audit logs containing operator ID, action,
target key, and version, never request bodies.

- [ ] **Step 5: Register Root-only, non-cacheable routes**

```go
admin := apiRouter.Group("/public-status-probe")
admin.Use(middleware.RootAuth(), middleware.DisableCache())
{
    admin.GET("/config", controller.GetPublicStatusProbeConfig)
    admin.PUT("/config", controller.UpdatePublicStatusProbeConfig)
    admin.POST("/targets", controller.CreatePublicStatusProbeTarget)
    admin.PUT("/targets/:key", controller.UpdatePublicStatusProbeTarget)
    admin.DELETE("/targets/:key", controller.DeletePublicStatusProbeTarget)
}
```

- [ ] **Step 6: Format, test, scan, and commit**

```powershell
gofmt -w controller/public_status_probe_admin.go controller/public_status_probe_admin_test.go service/public_status_probe/loader.go service/public_status_probe/loader_test.go router/api-router.go router/api_router_test.go
go test ./service/public_status_probe ./controller ./router -run "PublicStatusProbe|ValidateTarget|StatusProbeRoute" -count=1
git grep -n -E 'json:"(api_key|base_url|model_mapping|header_override|param_override)' -- controller/public_status_probe_admin.go
git add controller/public_status_probe_admin.go controller/public_status_probe_admin_test.go service/public_status_probe/loader.go service/public_status_probe/loader_test.go router/api-router.go router/api_router_test.go
git commit -m "feat: manage public probe targets through root API"
```

Expected: tests PASS; grep finds no forbidden response tags.

## Task 6: Frontend API, Types, And Validation

**Files:**
- Create: `web/src/features/system-settings/operations/public-status-probe/types.ts`
- Create: `web/src/features/system-settings/operations/public-status-probe/api.ts`
- Create: `web/src/features/system-settings/operations/public-status-probe/validation.ts`
- Create: `web/src/features/system-settings/operations/public-status-probe/__tests__/api.test.ts`
- Create: `web/src/features/system-settings/operations/public-status-probe/__tests__/validation.test.ts`

- [ ] **Step 1: Write failing API and schema tests**

Assert exact methods/paths/payloads, DELETE version query, all numeric bounds,
trimmed required text, four protocols, non-negative integer key index, and the
20-target limit.

```ts
it('deletes with the current version', async () => {
  await deletePublicStatusProbeTarget('probe-a', 7)
  expect(api.delete).toHaveBeenCalledWith(
    '/api/public-status-probe/targets/probe-a',
    { params: { version: 7 } }
  )
})
```

- [ ] **Step 2: Verify failure**

```powershell
Set-Location web
bun test src/features/system-settings/operations/public-status-probe/__tests__/api.test.ts src/features/system-settings/operations/public-status-probe/__tests__/validation.test.ts
```

Expected: FAIL because the module does not exist.

- [ ] **Step 3: Implement sanitized contracts**

Define `PublicStatusProbeConfig`, `PublicStatusProbeTarget`,
`PublicStatusProbeChannel`, `GlobalSettingsInput`, and `TargetInput`. Implement:

```ts
getPublicStatusProbeConfig()
updatePublicStatusProbeConfig(input: GlobalSettingsInput & { version: number })
createPublicStatusProbeTarget(version: number, target: TargetInput)
updatePublicStatusProbeTarget(key: string, version: number, target: TargetInput)
deletePublicStatusProbeTarget(key: string, version: number)
```

Use the shared `api` client. Export Zod `globalSettingsSchema` and
`targetFormSchema`, plus a mapper that distinguishes validation, HTTP 409, and
request failures without automatic retry.

- [ ] **Step 4: Test, typecheck, and commit**

```powershell
bun test src/features/system-settings/operations/public-status-probe/__tests__/api.test.ts src/features/system-settings/operations/public-status-probe/__tests__/validation.test.ts
bun run typecheck
git add src/features/system-settings/operations/public-status-probe
git commit -m "feat: add public probe management client"
```

Expected: PASS.

## Task 7: Root Management Interface

**Files:**
- Create: `web/src/features/system-settings/operations/public-status-probe/index.tsx`
- Create: `web/src/features/system-settings/operations/public-status-probe/components/global-settings-form.tsx`
- Create: `web/src/features/system-settings/operations/public-status-probe/components/targets-section.tsx`
- Create: `web/src/features/system-settings/operations/public-status-probe/components/target-dialog.tsx`
- Create: `web/src/features/system-settings/operations/public-status-probe/components/delete-target-dialog.tsx`
- Create: `web/src/features/system-settings/operations/public-status-probe/__tests__/section.test.tsx`

- [ ] **Step 1: Write failing interaction tests**

Cover loading/retry/empty, global save, create, edit preserving key,
enable/disable, named delete confirmation, duplicate-submit prevention, field
errors, dirty-close confirmation, and 409 refresh retaining unsaved values.

```tsx
it('keeps edited values after a version conflict refresh', async () => {
  renderManagementSection({ updateStatus: 409 })
  await user.click(screen.getByRole('button', { name: /edit codex/i }))
  await user.clear(screen.getByLabelText(/display name/i))
  await user.type(screen.getByLabelText(/display name/i), 'Codex New')
  await user.click(screen.getByRole('button', { name: /save target/i }))
  expect(await screen.findByDisplayValue('Codex New')).toBeVisible()
  expect(screen.getByText(/configuration changed/i)).toBeVisible()
})
```

- [ ] **Step 2: Verify failure**

```powershell
Set-Location web
bun test src/features/system-settings/operations/public-status-probe/__tests__/section.test.tsx
```

Expected: FAIL because components do not exist.

- [ ] **Step 3: Implement query/mutation ownership and page states**

```ts
export const publicStatusProbeConfigQueryKey = [
  'public-status-probe',
  'config',
] as const
```

Use `useQuery` for GET and `useMutation` per command. Replace query cache with the
complete successful response. On 409, retain the active draft, refetch current
config, and show a conflict alert. Disable duplicate submissions.

- [ ] **Step 4: Implement global controls and target workflows**

Use `SettingsSection`, `SettingsSwitchField`, `SettingsFormGrid`, stable bounded
numeric inputs, and a read-only fixed interval of 60 seconds. Group target rows by
configured `group`; keep disabled rows visible. Use Lucide `Plus`, `Pencil`, and
`Trash2` icon buttons with tooltips and accessible names.

Build create/edit with React Hook Form and `zodResolver(targetFormSchema)`. Fields:
enabled, group, display name, safe channel selector, model, protocol, and key
index. Show immutable key read-only only when editing. Require a named delete
dialog. Use unframed sections, not nested cards.

- [ ] **Step 5: Test, lint, typecheck, and commit**

```powershell
bun test src/features/system-settings/operations/public-status-probe
bun run typecheck
bunx oxlint -c .oxlintrc.json src/features/system-settings/operations/public-status-probe
git add src/features/system-settings/operations/public-status-probe
git commit -m "feat: add public probe management interface"
```

Expected: PASS.

## Task 8: Operations Registration And Localization

**Files:**
- Modify: `web/src/features/system-settings/operations/section-registry.tsx`
- Modify: `web/src/i18n/static-keys.ts`
- Modify: `web/src/i18n/locales/en.json`
- Modify: `web/src/i18n/locales/zh.json`
- Modify: `web/src/i18n/locales/zh-TW.json`
- Modify: `web/src/i18n/locales/fr.json`
- Modify: `web/src/i18n/locales/ja.json`
- Modify: `web/src/i18n/locales/ru.json`
- Modify: `web/src/i18n/locales/vi.json`
- Modify: `web/src/i18n/affiliate-probe-localization.test.ts`

- [ ] **Step 1: Write failing registration and locale tests**

Require the `public-status-probe` section exactly once and every visible/accessibility
string in all supported locales.

- [ ] **Step 2: Register the independent section**

```tsx
import { PublicStatusProbeSettingsSection } from './public-status-probe'

{
  id: 'public-status-probe',
  titleKey: 'Public Status Probe',
  build: () => <PublicStatusProbeSettingsSection />,
},
```

The existing Root route guard and generic Operations `$section` route provide
`/system-settings/operations/public-status-probe`.

- [ ] **Step 3: Synchronize locales and run frontend gates**

```powershell
Set-Location web
bun run i18n:sync
bun test src/features/system-settings/operations/public-status-probe src/i18n/affiliate-probe-localization.test.ts
bun run typecheck
bun run lint
bun run format:check
bun run copyright:check
bun run build
```

Fill accurate `zh` and `zh-TW`; require non-empty values in remaining locales.

- [ ] **Step 4: Commit registration and locales**

```powershell
git add src/features/system-settings/operations/section-registry.tsx src/i18n
git commit -m "feat: register public probe management settings"
```

## Task 9: Full Regression, Isolation, And Visual Gates

**Files:**
- Modify only files required by test failures.
- Store screenshots outside Git tracking under `deploy-artifacts/public-probe-management-visual/`.

- [ ] **Step 1: Scan forbidden calls and public private fields**

```powershell
git grep -n -E "testChannel|testChannelWithOptions|SetupContextForSelectedChannel|CacheGetRandomSatisfiedChannel|GetNextEnabledKey|SaveChannelInfo|RecordConsumeLog|UpdateResponseTime" -- service/public_status_probe controller/public_status_probe.go controller/public_status_probe_admin.go
git grep -n -E 'json:"(api_key|base_url|key_index|channel_id|model_mapping|header_override|param_override)' -- controller/public_status_probe.go
```

Expected: no forbidden behavior call and no private public-DTO tags.

- [ ] **Step 2: Run complete backend verification**

```powershell
go test ./... -count=1
go build ./...
Set-Location relaykit
$env:GOWORK='off'
go build ./...
Remove-Item Env:GOWORK
```

Expected: all commands exit 0.

- [ ] **Step 3: Run complete frontend verification**

```powershell
Set-Location web
bun test
bun run typecheck
bun run lint
bun run format:check
bun run copyright:check
bun run build
```

Expected: all commands exit 0.

- [ ] **Step 4: Run local Playwright interaction and responsive checks**

Start the app on an unused port with disposable SQLite and a seeded Root user.
Exercise global save, create, edit, disable, re-enable, delete, and conflict. Capture
management and public screenshots at 320, 375, 768, and 1440 pixels. Assert no page
overflow, clipped text, overlap, blank content, dialog escape, or status-timeline
regression. Stop the disposable server afterward.

- [ ] **Step 5: Re-run focused tests and commit only real fixes**

```powershell
git status --short
go test ./setting/public_status_probe_setting ./model ./service/public_status_probe ./controller ./router -count=1
Set-Location web
bun test src/features/system-settings/operations/public-status-probe src/features/group-probe-status
bun run typecheck
```

If fixes exist, stage the exact reported files and commit:

```powershell
git commit -m "test: verify managed public probe configuration"
```

Do not create an empty commit.

## Task 10: qiniu Backup, Deployment, And Acceptance

**Files:**
- Create on qiniu: timestamped rollback directory under `/www/dk_project/dk_app/newapi/backups/`.
- Create on qiniu: qiniu-only Compose override for the immutable image.
- Do not modify GitHub or any remote Git ref.

- [ ] **Step 1: Record local and remote hold evidence**

```powershell
git status --short --branch
git rev-parse HEAD
git rev-parse origin/feature/affiliate-commission
git log --oneline origin/feature/affiliate-commission..HEAD
```

Record hashes. Never run `git push`, `gh pr create`, or remote mutations.

- [ ] **Step 2: Reverify qiniu topology read-only through Xterminal MCP**

Confirm `/www/dk_project/dk_app/newapi/newapi_EWdt`, effective Compose files,
New API/MySQL/Redis IDs, image digests, start times, restart counts, health, `.env`
checksum, and current `/api/status`, `/api/status/probes`, and `/status`. Abort if
topology or dependency identity changed unexpectedly.

- [ ] **Step 3: Create verified rollback evidence before building**

Create a mode-0700 timestamped directory. Preserve `.env`, effective Compose files,
`docker compose config`, all three service inspect outputs, current app image and
digest, current `PublicStatusProbeConfig` if present, existing probe environment
values, and exact scoped rollback command. Store secret files at 0600 and a
separate redacted manifest. Produce and verify a non-empty MySQL logical dump
without stopping or recreating MySQL.

- [ ] **Step 4: Build an immutable candidate and validate Compose**

Derive deterministic names before transfer:

```powershell
$shortCommit = git rev-parse --short=12 HEAD
$utcStamp = (Get-Date).ToUniversalTime().ToString('yyyyMMddTHHmmssZ')
$imageTag = "local/new-api:$shortCommit-$utcStamp"
$overrideName = "docker-compose.qiniu-probe-management-$shortCommit.yml"
$releaseEnv = "SHORT_COMMIT=$shortCommit`nIMAGE_TAG=$imageTag`nOVERRIDE_NAME=$overrideName`n"
```

Transfer the tested source archive, verify SHA-256, and build `$imageTag`. Never
overwrite the old tag. Write `$overrideName` as a qiniu-only override retaining
old environment variables for rollback. Validate the exact Compose file set and
prove only `new-api` changes. Transfer the non-secret `$releaseEnv` content as
`deploy-public-probe-management.env` and record all concrete values in the
redacted manifest.

- [ ] **Step 5: Recreate only the app**

```bash
cd /www/dk_project/dk_app/newapi/newapi_EWdt
. ./deploy-public-probe-management.env
docker compose -p newapi_ewdt \
  -f docker-compose.yml \
  -f "${OVERRIDE_NAME}" \
  up -d --no-deps --no-build --pull never new-api
```

Never run project-wide `down`, `restart`, or unscoped `up`.

- [ ] **Step 6: Verify migration, CRUD, public output, and isolation**

Require healthy app/restart count 0; unchanged MySQL/Redis identity; no panic,
migration, unknown-column, credential, or invalid-config log; original environment
target imported with its key; sanitized Root GET; usable management page; safe
create/edit/disable/re-enable/delete; temporary target removed before handoff;
public `/status` consistency; at least two complete 60-second slots; no duplicate
slot rows; no probe-attributable channel, key-state, quota, billing, or consume-log
mutation; and clean desktop/mobile screenshots.

- [ ] **Step 7: Roll back immediately on any failed gate**

Point the override to the preserved image and run the same scoped
`up -d --no-deps --no-build --pull never new-api`. Do not restore the whole
database unless migration or integrity evidence requires it. Retained environment
values restore the old image's probe behavior.

- [ ] **Step 8: Present evidence and prove GitHub stayed unchanged**

Report qiniu URL, local commit, candidate image/digest, backup directory, rollback
command, two-cycle observations, screenshots, and dependency comparison. Re-run:

```powershell
git rev-parse origin/feature/affiliate-commission
git status --short --branch
```

The remote hash must equal the pre-deployment value. Do not push.
