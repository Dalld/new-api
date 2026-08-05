# Group Probe Status Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 按可配置分组和模型执行无业务副作用的合成探针，保存独立历史，并通过安全的公共状态页和管理员界面展示结果。

**Architecture:** typed setting 保存唯一 JSON 配置；`group_probe` system task 通过现有租约跨实例去重，并用普通分组/模型选路调用带 `ChannelTestOptions` 的既有渠道测试。结果写入独立表，服务端完成 24 小时聚合，公共 API 只返回白名单 DTO。

**Tech Stack:** Go 1.22、Gin、GORM v2、现有 system-task/channel-test/adaptor、React 19、TanStack Router/Query、Bun。

---

### Task 1: Typed setting 与能力校验

**Files:**
- Create: `setting/group_probe_setting/group_probe_setting.go`
- Create: `setting/group_probe_setting/group_probe_setting_test.go`
- Modify: `setting/setting.go`

- [ ] **Step 1: 写失败测试**

覆盖默认关闭、10 分钟、7 天、45 秒、空 groups；边界 interval 5..1440、retention 1..90、timeout 5..120、最多 50 个唯一 group；字段 trim、非空和长度；重复 group 拒绝；保存时必须存在启用的 group/model ability。

- [ ] **Step 2: 运行失败测试**

Run: `go test ./setting/group_probe_setting -count=1`
Expected: FAIL，包不存在。

- [ ] **Step 3: 实现配置结构与 option 往返**

定义 `Setting{Enabled, IntervalMinutes, RetentionDays, TimeoutSeconds, Groups []GroupMapping}` 和 `GroupMapping{Group, DisplayName, Model string; Public bool}`；JSON 通过 `common.Marshal/Unmarshal`；泛化安装默认空映射且禁用，历史三组只在部署验收时写入。

- [ ] **Step 4: 复测并提交**

Run: `go test ./setting/group_probe_setting -count=1`
Expected: PASS。

Run: `git add setting/group_probe_setting setting/setting.go && git commit -m "feat: add group probe settings"`

### Task 2: 结果模型、迁移、脱敏和聚合

**Files:**
- Create: `model/group_probe_result.go`
- Create: `model/group_probe_result_test.go`
- Modify: `model/main.go`
- Modify: `model/main_test.go`

- [ ] **Step 1: 写失败测试**

覆盖 `GroupProbeResult` 迁移、插入、已删除 channel id 可查询、retention 清理；脱敏去除 authorization/key/token、URL query 和上游 body 并截断；聚合 48 个 30 分钟桶，当前 healthy/degraded/down/unknown、稀疏数据和 stale 边界，成功率及仅成功样本平均延迟。

- [ ] **Step 2: 运行失败测试**

Run: `go test ./model -run 'TestGroupProbe' -count=1`
Expected: FAIL，模型和聚合不存在。

- [ ] **Step 3: 实现模型和纯聚合逻辑**

字段严格按规格：`TaskID, GroupName, DisplayName, ModelName, ChannelID, Success, LatencyMS, ErrorCode, ErrorMessage, CheckedAt`；索引 `(group_name, checked_at)` 及 `checked_at`。公共 DTO 不嵌入数据库模型，仅包含 display/group/model/state/availability/latency/sample/freshness/buckets。

- [ ] **Step 4: 复测并提交**

Run: `go test ./model -run 'TestGroupProbe' -count=1`
Expected: PASS。

Run: `git add model/group_probe_result* model/main.go model/main_test.go && git commit -m "feat: persist and aggregate group probes"`

### Task 3: 渠道测试 probe 模式无副作用

**Files:**
- Modify: `controller/channel-test.go`
- Modify: `controller/channel_test_internal_test.go`
- Modify: `service/channel.go`
- Create: `service/group_probe_request_test.go`

- [ ] **Step 1: 写失败测试**

为 `ChannelTestOptions` 测试默认调用保持现状；probe 模式显式传播 `UsingGroup` 与 context timeout，并跳过 channel response time、自动启停、consume log、quota export、perf metric 和用户 quota 更新。成功、provider 失败、超时和无可用渠道均关闭 body 并返回受控错误分类。

- [ ] **Step 2: 运行失败测试**

Run: `go test ./controller ./service -run 'Test.*ProbeMode' -count=1`
Expected: FAIL，现有测试函数没有 options。

- [ ] **Step 3: 提取带 options 的复用入口**

保留当前 channel-test 包装函数和默认副作用；增加内部/服务入口接受 options，probe 调用 normal group/model selection retry zero 和同一 adaptor request path，但所有业务副作用标志均为 false。不得复制 provider adaptor。

- [ ] **Step 4: 复测并提交**

Run: `go test ./controller ./service -run 'Test.*ProbeMode|Test.*ChannelTest' -count=1`
Expected: PASS，现有渠道测试行为不变。

Run: `git add controller/channel-test.go controller/channel_test_internal_test.go service/channel.go service/group_probe_request_test.go && git commit -m "refactor: add side-effect-free channel probe mode"`

### Task 4: System task 调度、租约和执行

**Files:**
- Modify: `model/system_task.go`
- Modify: `controller/system_task_handlers.go`
- Modify: `service/system_task.go`
- Create: `service/group_probe_task.go`
- Create: `service/group_probe_task_test.go`
- Modify: `service/system_task_test.go`

- [ ] **Step 1: 写失败测试**

覆盖 `SystemTaskTypeGroupProbe`、启用时按 interval 入队、关闭时不入队、active key 去重、管理员手动入队也走相同任务、租约丢失/取消停止剩余 probe、单组失败不阻断后续、每个映射恰好一条结果、成功调度运行后清理 retention。

- [ ] **Step 2: 运行失败测试**

Run: `go test ./service ./model -run 'TestGroupProbeTask|TestSystemTask.*GroupProbe' -count=1`
Expected: FAIL，任务类型和 handler 不存在。

- [ ] **Step 3: 实现顺序执行器**

使用现有 `CreateSystemTask`、active key 和 heartbeat；每组独立 recover 并写脱敏结果，任务 result 只保存 total/succeeded/failed，详细错误仅在管理员结果表。scheduler 使用配置 interval，不创建未追踪 goroutine。

- [ ] **Step 4: 复测并提交**

Run: `go test ./service ./model -run 'TestGroupProbeTask|TestSystemTask.*GroupProbe' -count=1`
Expected: PASS。

Run: `git add model/system_task.go controller/system_task_handlers.go service/system_task.go service/group_probe_task* service/system_task_test.go && git commit -m "feat: schedule group probe tasks"`

### Task 5: 公共和管理员 API

**Files:**
- Create: `controller/group_probe.go`
- Create: `controller/group_probe_test.go`
- Modify: `router/api-router.go`
- Modify: `router/api_router_test.go`

- [ ] **Step 1: 写失败测试**

公共 `GET /api/status/probes` 测试 15 秒缓存、只含 public mappings、空映射成功、字段白名单且不含 channel/task/error/credential；管理员 settings GET/PUT、run POST、results GET 测试认证、权限、critical rate limit、分页上限和过滤。

- [ ] **Step 2: 运行失败测试**

Run: `go test ./controller ./router -run 'TestGroupProbe' -count=1`
Expected: FAIL，控制器和路由不存在。

- [ ] **Step 3: 实现专用 DTO 和路由**

公共控制器从 settings 的 `Public` allowlist 和聚合查询构造响应，绝不序列化 `GroupProbeResult`；管理员 run 返回现有 system task id，前端后续轮询 `/api/system-task/:task_id`。

- [ ] **Step 4: 复测并提交**

Run: `go test ./controller ./router -run 'TestGroupProbe' -count=1`
Expected: PASS。

Run: `git add controller/group_probe* router/api-router.go router/api_router_test.go && git commit -m "feat: expose group probe APIs"`

### Task 6: 公共状态页

**Files:**
- Create: `web/src/features/group-probe-status/api.ts`
- Create: `web/src/features/group-probe-status/types.ts`
- Create: `web/src/features/group-probe-status/index.tsx`
- Create: `web/src/features/group-probe-status/components/group-status-row.tsx`
- Create: `web/src/features/group-probe-status/components/status-timeline.tsx`
- Create: `web/src/features/group-probe-status/__tests__/status-page.test.tsx`
- Create: `web/src/routes/status.tsx`

- [ ] **Step 1: 写失败交互测试**

覆盖 30 秒 refetch、loading/error/empty/stale/partial；状态有文字等价；48 格时间线可横向滚动且 tooltip 可访问；展开内容不含保护字段；轮询在卸载后停止且不重叠。

- [ ] **Step 2: 运行失败测试**

Run: `cd web && bun test group-probe-status`
Expected: FAIL，模块不存在。

- [ ] **Step 3: 实现公开路由和页面**

`/status` 使用公开布局，首屏直接显示状态体验；稳定尺寸避免刷新跳动。每组展示 display name、model、current state、24h availability、成功样本平均 latency、last update 和 48 个 bucket；明确标为 synthetic probe，不声称真实流量 SLA。

- [ ] **Step 4: 复测并提交**

Run: `cd web && bun test group-probe-status`
Expected: PASS。

Run: `git add web/src/features/group-probe-status web/src/routes/status.tsx && git commit -m "feat: add public group status page"`

### Task 7: 管理员设置、运行和结果界面

**Files:**
- Create: `web/src/features/system-settings/operations/group-probe/api.ts`
- Create: `web/src/features/system-settings/operations/group-probe/types.ts`
- Create: `web/src/features/system-settings/operations/group-probe/index.tsx`
- Create: `web/src/features/system-settings/operations/group-probe/__tests__/settings.test.tsx`
- Modify: `web/src/features/system-settings/operations/section-registry.tsx`

- [ ] **Step 1: 写失败测试**

覆盖启停、数值边界、从 enabled abilities 选择 group/model、增删映射、public 开关、重复 group 拒绝、保存、立即运行防重复点击、轮询现有 task、recent protected results 和卸载清理。

- [ ] **Step 2: 运行失败测试**

Run: `cd web && bun test group-probe-settings`
Expected: FAIL，设置模块不存在。

- [ ] **Step 3: 实现设置 UI**

使用 React Hook Form + Zod、现有 `api` 和 system settings section registry。管理员结果可显示 sanitized error、channel id 和 task id，公共组件/类型不得导入管理员 DTO。

- [ ] **Step 4: 复测并提交**

Run: `cd web && bun test group-probe-settings`
Expected: PASS。

Run: `git add web/src/features/system-settings/operations && git commit -m "feat: manage group probes in admin settings"`

### Task 8: i18n、路由生成和完整验证

**Files:**
- Modify: `web/src/i18n/locales/en.json`
- Modify: `web/src/i18n/locales/zh.json`
- Modify: `web/src/i18n/locales/zh-TW.json`
- Modify: `web/src/i18n/locales/fr.json`
- Modify: `web/src/i18n/locales/ru.json`
- Modify: `web/src/i18n/locales/ja.json`
- Modify: `web/src/i18n/locales/vi.json`
- Generate: `web/src/routeTree.gen.ts`

- [ ] **Step 1: 同步 i18n 并生成路由**

Run: `cd web && bun run i18n:sync && bun run build`
Expected: `/status` 出现在生成路由中，所有 locale 有合法 fallback；不复制参考归档的 generated file。

- [ ] **Step 2: 后端与前端全量门禁**

Run: `go test ./... && go build ./...`
Expected: PASS。

Run: `cd web && bun run typecheck && bun run lint && bun run format:check && bun run build`
Expected: 全部退出码 0。

- [ ] **Step 3: 提交验证变更**

Run: `git diff --check && git status --short`
Expected: 无空白错误或构建产物。

Run: `git add web/src/i18n web/src/routeTree.gen.ts && git commit -m "test: verify group probe status workflows"`

### Task 9: 历史服务器配置和验收

**Files:**
- Create: `docs/deployment/group-probe-test-server.md`

- [ ] **Step 1: 复用返佣部署快照**

确认逻辑 MySQL dump、compose 归档、旧镜像 ID 和回滚命令均已记录；禁止复制 live MySQL data directory。

- [ ] **Step 2: 迁移后写入历史三组配置**

通过管理员 API 保存 `codex -> gpt-5.5`、`codex纯血PRO池 -> gpt-5.5`、`provip -> gpt-5.5`，启用、interval 10、retention 7、timeout 45、均公开；配置不得硬编码入源码。

- [ ] **Step 3: 手动与定时验收**

手动运行一次，验证每组一条脱敏结果；前后快照对比 user quota、consume logs、quota export、perf metrics、channel status/response time 均未改变。验证 `/api/status/probes` 字段白名单与 `/status` 桌面/移动布局，等待第二次定时运行并确认 active task 去重。

- [ ] **Step 4: 记录结果和回滚**

文档写入任务 ID、结果数量、HTTP/健康检查、无副作用对比、旧/新镜像和恢复命令；失败时恢复旧 image/config，数据库被改变时从逻辑 dump 恢复。

- [ ] **Step 5: 提交部署记录**

Run: `git add docs/deployment/group-probe-test-server.md && git commit -m "docs: record group probe deployment acceptance"`
