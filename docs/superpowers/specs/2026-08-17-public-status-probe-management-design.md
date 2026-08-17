# Public Status Probe Management Design

## 1. Decision and supersession

This design adds a Root-only administrator module for configuring the isolated
public status probe. It supersedes only the following decisions in
`2026-08-16-isolated-public-status-probe-design.md`:

- the administrator settings prohibition in sections 1, 3, and 11;
- environment-only ownership in section 4;
- the frontend verification statement that management pages contain no probe UI.

All probe isolation, provider transport, persistence, public API allowlisting,
public `/status` behavior, scheduling lease, billing safety, and deployment
constraints from the earlier design remain mandatory unless this document
explicitly changes them.

The accepted configuration architecture is a structured JSON document stored
through the existing database-backed Option mechanism. The public `/status`
page remains the only end-user surface. The new management page is available
only to Root administrators and does not expose probe results or provider
credentials.

## 2. Goals

- Add an independent Root administrator page named "Public Status Probe".
- Allow administrators to add, edit, enable, disable, and delete probe targets.
- Derive display groups from target configuration without a separate group table.
- Configure bounded global probe behavior while keeping the interval fixed at
  exactly 60 seconds.
- Preserve the currently deployed environment configuration during the first
  upgrade to database-owned configuration.
- Apply administrator changes without recreating or restarting the application
  container.
- Keep public output, probe isolation, and all existing channel, quota, billing,
  logging, and routing behavior unchanged.
- Deploy and validate on qiniu without pushing any commit to GitHub.

## 3. Non-goals

- No empty groups, group table, group-only CRUD, nested group settings, or
  independent group ordering.
- No manual probe action, protected result browser, incident management, alert
  delivery, SLA configuration, or status-page content editor.
- No configurable interval; the scheduler remains aligned to 60-second UTC slots.
- No API key, Base URL, channel mapping, proxy, header, raw provider response, or
  other credential editor in this module.
- No immediate deletion of historical observations when a target is disabled or
  deleted.
- No GitHub push, force push, pull request, or remote branch modification.

## 4. Configuration ownership and migration

### 4.1 Canonical Option document

The canonical configuration is one versioned JSON document stored under the
stable `PublicStatusProbeConfig` Option key and registered with the existing
Option synchronization and validation mechanisms. The document contains:

```json
{
  "schema_version": 1,
  "version": 1,
  "enabled": true,
  "ping_timeout_seconds": 8,
  "chat_timeout_seconds": 45,
  "degraded_latency_ms": 6000,
  "concurrency": 5,
  "retention_days": 7,
  "targets": []
}
```

Rules:

| Field | Rule |
| --- | --- |
| `schema_version` | Exact supported document schema; unknown values are rejected |
| `version` | Positive optimistic-lock version incremented on every successful mutation |
| `enabled` | Global scheduler and public-target switch |
| `ping_timeout_seconds` | Integer 1-15 |
| `chat_timeout_seconds` | Integer 5-60 |
| `degraded_latency_ms` | Integer 1-60000 |
| `concurrency` | Integer 1-20 |
| `retention_days` | Integer 1-30 |
| `targets` | At most 20 validated target records |

The interval is not stored in the document. Runtime and public API contracts use
exactly 60 seconds.

Future document changes require an explicit backward-compatible parser or data
migration. An instance that receives an unknown `schema_version` keeps its last
valid runtime snapshot and reports one bounded error rather than guessing field
semantics.

### 4.2 First-upgrade import

When the canonical Option is absent, the master instance loads and validates the
existing `PUBLIC_STATUS_PROBE_*` environment variables using the current bounded
parser. It then creates the canonical Option exactly once. A compare-and-create
operation prevents two instances from overwriting each other during startup.

The imported target keys and all valid field values are preserved byte-for-byte
after existing trimming and normalization, so current history remains attached
to the deployed target. Targets imported from the environment default to enabled.

After the Option exists, database configuration is the sole runtime source.
Environment variables are retained only as rollback input and never overwrite a
database configuration. Invalid environment input does not create an Option,
does not expose the supplied value in logs, and leaves probing disabled with a
bounded configuration error.

The deployment verifier must prove that qiniu's current target appears in the
management API and public status response after migration before the Compose
environment configuration is considered obsolete.

## 5. Target model and group semantics

Each target contains:

```json
{
  "key": "probe-opaque-id",
  "enabled": true,
  "group": "codex",
  "display_name": "Codex",
  "model": "gpt-5.5",
  "protocol": "openai_chat",
  "channel_id": 23,
  "key_index": 0
}
```

`key` is generated by the backend for a newly created target, is globally unique
within the configuration, and is immutable. Imported keys remain unchanged.
Changing group, display name, model, protocol, channel, key index, or enabled
state does not change the key and therefore does not disconnect valid history.

`group` is a normalized target field rather than a separate entity. On the
public page, a group appears when its first enabled target exists and disappears
when its final enabled target is disabled or deleted. The management page still
lists disabled target records grouped by their configured value so an
administrator can re-enable or edit them. It cannot create an empty group that
has no target record.

Target array order is the administrator-defined display order used by the public
API and `/status`. Targets in one group retain their relative array order. A
group may contain any number of targets within the global 20-target limit, and
targets in one group may use different display names. Two targets may select the
same channel, model, protocol, and key index when their stable keys differ; this
supports deliberate comparative display configurations and is not treated as a
duplicate identity.

Supported protocols remain:

- `openai_chat`;
- `openai_responses`;
- `anthropic_messages`;
- `gemini_generate_content`.

All existing UTF-8, rune-length, JSON-size, protocol, target-count, channel-id,
and key-index limits remain. Create and update validation also verifies that the
selected channel exists, is enabled, has a native type matching the protocol,
and has the selected enabled key. Unsupported proxies, custom overrides, and
authentication paths are rejected with a bounded field-level error. Validation
performs no upstream request and does not mutate the channel or channel cache.

## 6. Root management API

All routes use `middleware.RootAuth()`:

```text
GET    /api/public-status-probe/config
PUT    /api/public-status-probe/config
POST   /api/public-status-probe/targets
PUT    /api/public-status-probe/targets/:key
DELETE /api/public-status-probe/targets/:key
```

The GET response returns global settings, target metadata, current version, and
only the minimum channel display metadata needed by the form. It never returns a
channel key, Base URL, organization, headers, model mapping, proxy settings, raw
channel settings, or provider response.

The global PUT replaces only the bounded global fields and cannot replace the
target array. Target routes mutate one target while preserving the remaining
array order. Every mutation requires the version last returned by GET. The
server performs a database-level compare-and-swap on that version; a prior read
followed by an unconditional write is forbidden. A version mismatch returns HTTP
409 and the current version without applying the stale mutation. Unknown JSON
fields, oversized bodies, duplicate target keys, invalid enum values, and
out-of-range fields are rejected. Mutation responses return the complete
sanitized canonical configuration and its incremented version. Authentication
failures retain the repository's standard 401/403 contract; validation returns
400, a missing target returns 404, and version conflict returns 409.

Create ignores any client-supplied key and generates a stable opaque identifier.
Update uses the path key and rejects attempts to change it. Delete is idempotent
only for an existing version: a missing key returns 404 rather than silently
incrementing the configuration.

Every successful mutation atomically persists the complete validated document,
publishes the new in-memory snapshot, and invalidates the public probe response
cache and ETag. A database error leaves the old runtime snapshot active and
returns a bounded error.

Mutation requests are non-cacheable, size bounded, and decoded with unknown
fields rejected. Successful and rejected write attempts emit a bounded
application audit event containing the authenticated operator identifier,
action, target key when applicable, and configuration version. The audit event
must not contain the request body or private channel fields and does not create a
consume log or other billing/business record.

## 7. Runtime configuration and multi-instance behavior

The scheduler remains alive for the application process lifetime and reads one
immutable current configuration snapshot at the start of each UTC minute slot.
It skips the slot when the global switch is off or no enabled targets exist. Each
slot uses one internally consistent snapshot for global limits and targets; a
save during a running slot applies to the next slot and never cancels work already
holding a lease.

The local instance publishes a successful mutation immediately. Other instances
receive the document through the existing Option synchronization mechanism and
atomically replace their snapshot only after complete validation. Existing
per-target leases and the unique result constraint remain the duplicate barriers
while instances observe different configuration versions.

Remote convergence is bounded by one configured Option synchronization interval.
When an instance accepts a newer configuration, it invalidates its own public
cache and ETag. The configuration version participates in cache identity, so an
instance cannot serve a response built for a previous target list after it has
accepted the new version. Before synchronization, the normal 15-second public
cache bound still applies to that instance's currently known version.

Disabled targets are excluded from scheduling and from the public target list.
Deleted targets are also excluded immediately after cache invalidation. Their
result rows remain until ordinary retention cleanup. Lease rows may remain as
inert bounded records; deletion must not race with an in-flight lease owner.
Recreating a logically similar target produces a new key and cannot inherit the
deleted target's history.

Retention cleanup remains active while the global switch is off or the enabled
target list is empty. Disabling probes must not leave historical rows permanently
outside the configured retention policy.

The public status controller reads the same canonical snapshot as the scheduler.
It must not infer active targets from historical result rows. Therefore disabled
or deleted targets cannot reappear due to stale database history.

## 8. Administrator UI

Add one independent Root-only route and sidebar entry named "Public Status
Probe" using the repository's existing authenticated layout, route guards,
components, spacing, icons, dark mode, and internationalization conventions.
The module is not embedded in channel management or the public `/status` page.

The page contains an unframed settings section for the global switch and bounded
numeric fields, followed by a stable target table or list grouped by `group`.
Each target row shows enabled state, display name, group, model, protocol,
channel, and key index. Familiar icon actions provide edit and delete with
tooltips and accessible labels. Delete requires an explicit confirmation naming
the public display target.

Create and edit use one focused dialog with:

- enabled toggle;
- group name;
- public display name;
- channel selector;
- model input;
- protocol selector;
- non-negative key-index input.

The generated target key is read-only and need not be prominent. The channel
selector shows safe identity fields only. Protocol options are explicit and may
be filtered by channel type, while the backend remains authoritative. Form
errors appear at their field; HTTP 409 refreshes the current configuration and
asks the administrator to review rather than overwriting another edit.

The page defines loading, empty, save-in-progress, success, validation-error,
conflict, and request-error states. Controls cannot submit twice. Layout must
remain usable without horizontal page overflow at 320, 375, 768, and 1440 pixel
widths. All visible text, accessible names, confirmation content, and server
error mappings use the existing i18n system.

When a 409 occurs, the page fetches the latest configuration and preserves the
administrator's unsaved form values for review; it must not automatically retry
or overwrite the newer server state. Navigation away from a dirty create or edit
dialog requires confirmation.

## 9. Security and isolation invariants

The management feature may write only its canonical Option. Probe execution may
continue to write only `public_status_probe_results` and
`public_status_probe_leases`. Configuration changes must not write channel
status, test time, response time, channel info, multi-key order/index, user or
subscription quota, consume logs, quota data, billing records, or performance
metrics.

The management controller and DTOs use explicit allowlists rather than serializing
Option internals, channel models, or probe result models. Logs may include a
bounded action name, configuration version, and target key, but never request
JSON, credentials, upstream URLs, provider bodies, or raw database errors.

Root authorization is enforced server-side on every route and is covered by
router tests. Hiding the sidebar entry is not an authorization control. The
public status endpoint remains public, read-only, separately rate limited, and
credential-free.

## 10. Error handling

- Invalid persisted configuration is rejected as a whole; the last valid runtime
  snapshot remains active and one bounded error is logged.
- A failed first import leaves probing disabled rather than partially importing
  targets.
- A failed create, update, or delete does not increment the version or invalidate
  a valid cache entry.
- A configuration conflict returns 409 and never merges fields implicitly.
- A channel deleted or disabled after configuration was saved produces the
  existing sanitized `invalid_target` observation until the administrator edits,
  disables, or deletes the target.
- Public API and UI errors remain sanitized and cannot include administrator
  configuration fields.

## 11. Verification requirements

### 11.1 Backend

Tests must cover:

- first import, absent environment values, invalid import, and compare-and-create
  startup races;
- canonical serialization, every numeric boundary, unknown fields, duplicate
  keys, 20-target limit, and immutable generated keys;
- Root authorization and exact response allowlists;
- create, edit, enable, disable, delete, 404, and 409 contracts;
- channel/protocol/key-index validation without network or channel writes;
- atomic persistence failure preserving the previous runtime snapshot;
- immediate local publish, synchronized remote publish, and invalid synchronized
  documents retaining the previous snapshot;
- scheduler reading one snapshot per slot and applying changes on the next slot;
- retention cleanup while globally disabled or with no enabled targets;
- disabled/deleted targets disappearing from public output while history remains;
- local and synchronized-remote public cache and ETag invalidation after
  successful mutations;
- unchanged channel, cache, quota, billing, consume-log, and routing state.

Run focused tests, `go test ./...`, and `go build ./...`. Any relaykit dependency
change additionally requires `GOWORK=off go build ./...` inside `relaykit`.

### 11.2 Frontend

Tests must cover Root route visibility, loading and empty states, create/edit
validation, duplicate-submit prevention, enable/disable, delete confirmation,
409 refresh behavior, safe channel rendering, field-level errors, and mobile
layout. Run changed-feature tests, typecheck, changed-file lint, i18n validation,
and a production build.

Playwright verification at 320, 375, 768, and 1440 pixels must prove that text,
dialogs, controls, table/list content, and confirmation actions do not overlap or
escape their containers. Existing public `/status` interaction and 60-point
history tests remain green.

## 12. qiniu deployment, acceptance, and rollback

Before deployment, preserve the effective Compose files, `.env`, application
image and container inspect data, current MySQL and Redis identities, and a
verified MySQL logical dump in a timestamped mode-0700 rollback directory. Also
preserve the current `PublicStatusProbeConfig` value when present, image digest,
and exact rollback command. Record the current Git commit and remote branch hash
to prove that GitHub is unchanged.

Build one immutable local image from the tested commit. Validate the effective
Compose configuration, then recreate only `new-api` with `--no-deps`,
`--no-build`, and `--pull never`. Never run project-wide `down`, `restart`, or an
unscoped `up`. MySQL and Redis container IDs, start times, and restart counts must
remain unchanged.

Acceptance requires:

1. healthy application and no panic, migration, unknown-column, or credential log;
2. successful one-time import of qiniu's existing probe target;
3. Root management GET and sanitized target data;
4. browser creation of a temporary target, edit/disable/delete behavior, and
   public `/status` consistency, using a safe test target approved by existing
   channel validation;
5. at least two complete 60-second cycles proving dynamic configuration changes;
6. unchanged channel runtime fields, multi-key state, quotas, billing, and
   consume-log behavior;
7. unchanged MySQL and Redis container identity and restart metadata;
8. desktop and mobile screenshots of both the management page and public status.

Any temporary acceptance target is deleted before handoff, and its removal is
verified on both the management page and public `/status`.

If any gate fails, switch only `new-api` back to the preserved immutable image
using the same scoped Compose command. The new Option may remain inert for a
rollback image; restoring the database is required only if migration or data
integrity verification fails. The prior environment variables remain available
to the rollback image.

No GitHub command that modifies remote state is permitted. The qiniu result and
rollback evidence are presented to the user for acceptance while all commits
remain local.
