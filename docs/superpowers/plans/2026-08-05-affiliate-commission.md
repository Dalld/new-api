# Affiliate Commission Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在所有直接充值渠道中，以创建订单时冻结的实付金额为基数，原子、幂等地结算邀请返佣，并提供用户和管理员审计界面。

**Architecture:** `TopUp` 保存返佣基数，所有支付回调统一调用 `model.SettleTopUp`；该函数锁定订单，在一个 GORM 事务中完成订单、充值额度、返佣记录和邀请人待领取余额。查询 API 只返回专用 DTO，前端沿用钱包领取流程并增加用户与管理员页面。

**Tech Stack:** Go 1.22、Gin、GORM v2、SQLite/MySQL/PostgreSQL、React 19、TanStack Router/Query、TypeScript、Bun。

---

### Task 1: 返佣配置、模型与迁移

**Files:**
- Create: `setting/affiliate_setting/affiliate_setting.go`
- Create: `setting/affiliate_setting/affiliate_setting_test.go`
- Create: `model/commission.go`
- Create: `model/commission_test.go`
- Modify: `model/topup.go`
- Modify: `model/main.go`
- Modify: `model/main_test.go`

- [ ] **Step 1: 写失败测试**

测试 `ValidateRate` 对 `0`、`1`、`0.25` 成功，对负数、大于 1、`NaN`、正负无穷失败；测试 `CommissionRecord.TopUpID` 唯一，`TopUp.CommissionBaseQuota` 和 `CommissionRate` 可迁移并回读。

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./setting/affiliate_setting ./model -run 'Test(ValidateCommissionRate|CommissionMigration)' -count=1`
Expected: FAIL，缺少包、字段或数据表。

- [ ] **Step 3: 实现最小模型与配置**

定义 `CommissionRecord{ID, TopUpID, OrderNo, InviterID, InviteeID, BaseQuota, Rate, CommissionQuota, CreatedAt}`，其中 `TopUpID` 使用 `uniqueIndex`；在 `TopUp` 增加 `CommissionBaseQuota int64` 和 `CommissionRate float64`。配置包导出 `GetRate() float64`、`SetRate(float64) error`、`ValidateRate(float64) error`，并通过现有 option 注册机制持久化 `CommissionRate`。

- [ ] **Step 4: 注册两条迁移路径并复测**

Run: `go test ./setting/affiliate_setting ./model -run 'Test(ValidateCommissionRate|CommissionMigration)' -count=1`
Expected: PASS；SQLite 重复迁移不报错。

- [ ] **Step 5: 提交**

Run: `git add setting/affiliate_setting model/commission.go model/commission_test.go model/topup.go model/main.go model/main_test.go && git commit -m "feat: add affiliate commission model"`

### Task 2: 冻结实付金额并统一事务结算

**Files:**
- Create: `model/topup_settlement.go`
- Create: `model/topup_settlement_test.go`
- Modify: `controller/topup.go`
- Modify: `controller/topup_stripe.go`
- Modify: `controller/topup_creem.go`
- Modify: `controller/topup_waffo.go`
- Modify: `controller/topup_waffo_pancake.go`
- Modify: `model/topup.go`

- [ ] **Step 1: 写订单创建与结算失败测试**

表驱动覆盖 epay、Stripe、Creem、Waffo、Waffo Pancake：创建订单时 `CommissionBaseQuota` 等于实际支付金额按 `QuotaPerUnit` 转换后的额度且 `CommissionRate` 被冻结；结算后订单成功、被邀请人充值、`commission_records` 插入、邀请人的 `aff_quota/aff_history` 同时增加。

- [ ] **Step 2: 写幂等、并发与回滚失败测试**

同一 `top_up_id` 重复/并发结算只能充值和返佣一次；注入返佣记录创建失败时，订单状态、被邀请人 quota、返佣记录、邀请人余额全部不变；无邀请人、禁用费率、零基数和四舍五入为零时不创建记录；订阅兼容订单拒绝进入直接充值结算。

- [ ] **Step 3: 运行测试确认失败**

Run: `go test ./model ./controller -run 'Test(FreezeCommissionBase|SettleTopUp)' -count=1`
Expected: FAIL，尚无统一结算函数。

- [ ] **Step 4: 实现统一结算原语**

实现 `SettleTopUp(req TopUpSettlementRequest) (*TopUpSettlementResult, error)`；使用 `DB.Transaction` 和 `lockForUpdate(tx)`，校验 provider/状态，在同一事务内更新订单、被邀请人充值、以唯一 `top_up_id` 创建记录，并用 `gorm.Expr` 更新邀请人 `aff_quota` 与 `aff_history`。金额转额度统一调用 `common.QuotaFromFloat`/`QuotaRound`，事务提交后再写日志和同步/失效缓存。

- [ ] **Step 5: 将所有回调和管理员补单切换到统一结算**

Epay 控制器不得直接写 `top_ups` 或 `users`；Stripe/Creem/Waffo/Waffo Pancake 和管理员补单只验证渠道载荷后调用 `SettleTopUp`。已成功订单返回渠道要求的成功响应，provider 不匹配或非法状态不修改数据。

- [ ] **Step 6: 复测并提交**

Run: `go test ./model ./controller -run 'Test(FreezeCommissionBase|SettleTopUp|Epay|Stripe|Creem|Waffo)' -count=1`
Expected: PASS，`-race` 下并发测试无重复记账。

Run: `git add model/topup* controller/topup*.go && git commit -m "feat: settle topups and commission atomically"`

### Task 3: 注册邀请关系兼容

**Files:**
- Modify: `model/user.go`
- Modify: `model/user_test.go`
- Modify: `controller/user.go`
- Modify: `controller/oauth.go`
- Modify: `controller/auth_flow_test.go`

- [ ] **Step 1: 写失败测试**

密码注册和 OAuth 注册在 `QuotaForInviter == 0` 时仍持久化 `inviter_id` 并增加一次 `aff_count`；重复完成 OAuth 不重复奖励或计数。

- [ ] **Step 2: 运行失败测试**

Run: `go test ./model ./controller -run 'Test.*InviterRelationship' -count=1`
Expected: FAIL，零固定奖励路径未完整保存关系。

- [ ] **Step 3: 统一事务内用户插入规则**

让 `Insert` 和 `InsertWithTx` 共用同一事务感知逻辑：邀请码有效即写 `InviterId`，固定奖励是否为零只影响 quota，不影响关系、计数和历史；所有注册入口调用该逻辑。

- [ ] **Step 4: 复测并提交**

Run: `go test ./model ./controller -run 'Test.*InviterRelationship' -count=1`
Expected: PASS。

Run: `git add model/user.go model/user_test.go controller/user.go controller/oauth.go controller/auth_flow_test.go && git commit -m "fix: preserve inviter relationships without fixed rewards"`

### Task 4: 用户与管理员返佣 API

**Files:**
- Create: `controller/affiliate.go`
- Create: `controller/affiliate_test.go`
- Modify: `model/commission.go`
- Modify: `router/api-router.go`
- Modify: `router/api_router_test.go`

- [ ] **Step 1: 写失败测试**

覆盖 `GET /api/user/aff/invitees`、`/commissions`、`/recharge_total` 的用户所有权与分页搜索；覆盖管理员 `/api/affiliate/relations`、`/commissions`；未登录、普通用户访问管理员接口被拒绝。搜索使用参数化 LIKE 和项目的分页边界。

- [ ] **Step 2: 运行失败测试**

Run: `go test ./controller ./router -run 'TestAffiliate' -count=1`
Expected: FAIL，路由或控制器不存在。

- [ ] **Step 3: 实现查询 DTO、控制器和路由**

用户 DTO 不返回邀请人隐私字段；管理员审计记录返回订单号、双方用户名、基数、费率、返佣额度和时间。关系查询包含 `inviter_id` 实际关系或返佣记录中的邀请人，不能只依赖 `aff_count > 0`。

- [ ] **Step 4: 复测并提交**

Run: `go test ./controller ./router -run 'TestAffiliate' -count=1`
Expected: PASS。

Run: `git add controller/affiliate* model/commission.go router/api-router.go router/api_router_test.go && git commit -m "feat: add affiliate audit APIs"`

### Task 5: 返佣设置与用户/管理员前端

**Files:**
- Create: `web/src/features/my-affiliate/api.ts`
- Create: `web/src/features/my-affiliate/types.ts`
- Create: `web/src/features/my-affiliate/index.tsx`
- Create: `web/src/features/my-affiliate/__tests__/referral-page.test.tsx`
- Create: `web/src/features/affiliate/api.ts`
- Create: `web/src/features/affiliate/types.ts`
- Create: `web/src/features/affiliate/index.tsx`
- Create: `web/src/features/affiliate/__tests__/affiliate-admin.test.tsx`
- Create: `web/src/routes/_authenticated/my-affiliate/index.tsx`
- Create: `web/src/routes/_authenticated/affiliate/index.tsx`
- Modify: `web/src/features/wallet/components/affiliate-rewards-card.tsx`
- Modify: `web/src/features/system-settings/billing/index.tsx`
- Modify: `web/src/components/layout/config/sidebar-data.ts`

- [ ] **Step 1: 写失败的页面和表单测试**

测试钱包奖励卡和转账弹窗仍存在；用户页展示邀请码/链接、汇总、受邀人和返佣列表；管理员页有关系/流水 tabs；费率表单以百分比展示，`0%` 原样保存，非法输入不提交。

- [ ] **Step 2: 运行失败测试**

Run: `cd web && bun test my-affiliate affiliate-admin`
Expected: FAIL，模块不存在。

- [ ] **Step 3: 实现页面、查询、路由与侧栏**

使用现有 `api`、React Query、表格、分页、图标和权限侧栏配置。保留现有钱包卡片及 `POST /api/user/aff_transfer`，不把返佣直接写入 spendable quota。

- [ ] **Step 4: 复测并提交**

Run: `cd web && bun test my-affiliate affiliate-admin`
Expected: PASS。

Run: `git add web/src/features web/src/routes web/src/components/layout && git commit -m "feat: add affiliate management pages"`

### Task 6: i18n、路由生成与全量验证

**Files:**
- Modify: `web/src/i18n/locales/en.json`
- Modify: `web/src/i18n/locales/zh.json`
- Modify: `web/src/i18n/locales/zh-TW.json`
- Modify: `web/src/i18n/locales/fr.json`
- Modify: `web/src/i18n/locales/ru.json`
- Modify: `web/src/i18n/locales/ja.json`
- Modify: `web/src/i18n/locales/vi.json`
- Generate: `web/src/routeTree.gen.ts`
- Modify: `.gitignore`
- Modify: `.dockerignore`

- [ ] **Step 1: 同步翻译并生成路由**

Run: `cd web && bun run i18n:sync && bun run build`
Expected: `routeTree.gen.ts` 包含两个新鉴权路由，所有 locale 均有 fallback，不覆盖归档中的生成文件。

- [ ] **Step 2: 执行前后端质量门禁**

Run: `go test ./...`
Expected: PASS。

Run: `go build ./...`
Expected: PASS。

Run: `cd web && bun run typecheck && bun run lint && bun run format:check && bun run build`
Expected: 全部退出码 0。

- [ ] **Step 3: 检查仓库卫生并提交**

确认 `.env*`、SQLite 数据库、备份压缩包、`node_modules` 和本地工具状态不被提交，同时保留 example 配置和 `web/src/features/usage-logs/data/schema.ts`。

Run: `git diff --check && git status --short`
Expected: 无空白错误，变更仅为计划内代码。

Run: `git add .gitignore .dockerignore web/src/i18n web/src/routeTree.gen.ts && git commit -m "test: verify affiliate commission workflows"`

### Task 7: 历史服务器备份、部署和回滚证明

**Files:**
- Create: `docs/deployment/affiliate-commission-test-server.md`

- [ ] **Step 1: 记录当前镜像与配置**

在服务器 `/www/dk_project/dk_app/newapi/newapi_EWdt` 记录 `docker compose config`、容器健康、挂载和当前镜像 ID；不得备份运行中的 MySQL 数据目录。

- [ ] **Step 2: 创建并验证逻辑备份**

在 MySQL 容器执行带 `--single-transaction --routines --triggers --events --hex-blob` 的 `mysqldump`，gzip 后验证非空并用 `gzip -t` 检查；归档 compose、`.env` 权限信息和非数据库持久文件。

- [ ] **Step 3: 构建不可变镜像并部署**

镜像标签包含 `git rev-parse --short HEAD`，只修改测试 new-api 服务的 image，保留旧标签；启动后等待 healthy 并验证 `http://TEST_SERVER_IP:3000`、迁移、登录、用户/管理员 API、重复结算和领取流程。

- [ ] **Step 4: 证明回滚可执行**

文档记录旧镜像、备份路径、恢复命令和验收结果；若迁移或测试数据改变数据库，先恢复旧镜像/config，再按需恢复逻辑 dump，并再次验证端口 3000 健康。

- [ ] **Step 5: 提交部署记录**

Run: `git add docs/deployment/affiliate-commission-test-server.md && git commit -m "docs: record affiliate deployment and rollback"`
