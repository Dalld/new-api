# 2026-10 upstream sync (affiliate commission fork)

Branch: `update/affiliate-main-2026-10`
Base: fork `main` at `0ab02020` (2026-08-01) + feature branch `feature/affiliate-commission`
Merged: `QuantumNous/new-api` `upstream/main` at `e449cc700` (2026-10-28), 440 commits

Result: 55 commits ahead of `upstream/main`, 157 files changed, +32,282 / -3,868.

## What the fork keeps

- 分销佣金：`controller/affiliate*.go`、`model/commission.go`、`model/affiliate_bind.go`、
  `setting/affiliate_setting`、统一结算 `model.SettleTopUp`（佣金快照 + 幂等）。
- 公共状态探针：`controller/public_status_probe*.go`、`model/public_status_probe*.go`、
  `setting/public_status_probe_setting`、CAS 版本化配置。
- 前端：`/affiliate`、`/my-affiliate`、系统设置中的佣金/探针分区、邀请人绑定对话框、
  侧边栏 “Referral Program” 入口。

## Conflict resolutions worth knowing

| 文件 | 处理 |
| --- | --- |
| `model/topup.go` | 保留 `SettleTopUp` 统一结算（含佣金），并入上游的 `creditTopUpQuota` 原子钱包上限、`syncCreditUserQuotaCache` 缓存同步、`common.Message` 配额错误；`RechargeEpay` 改为 `SettleTopUp` 的薄封装，兼容上游支付测试。 |
| `model/main.go` | 只保留一个 `migrateDB`（上游已删除 `migrateDBFast`），补入 `EnsureLegacyAccessTokenRetireAt`，保留 `commission_records` / `group_probe_results` / `public_status_probe_*` 迁移。 |
| `model/option.go` | 探针配置仍走 CAS;叠加 上游的 request-policy / passkey-domain / model-pricing 分发，`loadOptionsFromDatabase` 返回 `errors.Join`。 |
| `model/user.go` | 邀请奖励仍在 `insertWithInviterTx` 事务内发放，外层只记日志；采用上游 i18n 文案。 |
| `controller/audit.go` | 保留 `affiliate.inviter_bind` 模板；上游已把审计写入独立 `audit_logs` 表。 |
| `controller/topup*.go` | 保留 `SettleTopUp` 调用点，采用上游 webhook 验签日志与支付前额度校验，去掉重复的 `"success"` 响应写入。 |
| i18n `zh/zh-TW` | 以 `upstream/main` 为底，仅覆盖本分支真正改过或独有的键（避免把上游新键翻译回旧文案）。 |
| `routeTree.gen.ts` | 取上游版本后由 rsbuild 重新生成，`/affiliate`、`/my-affiliate` 路由已回归。 |
| 测试 | 15 个自研前端用例从 `node:test` / `happy-dom` / `bun:test` 迁移到仓库的 vitest + jsdom；`middleware/access_token_routes.go` 为本分支新增路由声明 scope，否则 scoped access token 会 403。 |

## Verification

- `go build ./...`、`go vet ./...`、`go test ./...`（全绿）。
- `bun run build`、`bun run test`（215 个文件全绿）。
- 部署验证（`/opt/new-api`，systemd `new-api.service`，SQLite）：
  - `/api/status`、`/api/status/probes`、`/api/public-status-probe/config`、
    `/api/affiliate/relations|commissions`、`/api/user/aff/overview` 均 200。
  - 端到端佣金：邀请人绑定 → 佣金率 0.1 → 待支付订单(10 USD, 快照基数 5,000,000)
    → 管理员补单 → 被邀请人 +5,000,000 额度、邀请人 aff_quota/aff_history +500,000、
    `commission_records` 1 行；重复补单不重复入账。
  - scoped access token：`ops:read` 可读探针配置、`ops:write`/无 scope 被 403，
    `wallet:read` 可读 `/api/user/aff/overview`。

## Follow-ups

- 上游把内置任务适配器改成了沙箱 JS 插件（`eb48396d5`），与本功能无关，但部署镜像需带上
  `plugins/` 目录。
- 上游引入 legacy access token 退役期限（`LegacyAccessTokenRetireAt`），现有旧 token 到期后
  需在“安全设置”里重新创建带 scope 的 token。

## 应用方式（已验证）

```bash
# 1) 先确保有上游与旧 main
git fetch origin && git fetch upstream

# 2) 从 bundle 取回合并分支（bundle 内包含分支引用）
git fetch /path/to/new-api-affiliate-merged.bundle \
  'refs/heads/update/affiliate-main-2026-10:refs/heads/update/affiliate-main-2026-10'

# 3) main 可直接快进（旧 main 是合并分支的祖先）
git checkout main
git merge --ff-only update/affiliate-main-2026-10

# 4) 推送
git push origin main
```

校验：`git bundle verify` 需要仓库里已有 `0ab02020`（旧 main）与 `e449cc700`（upstream/main）。
另有一份等价补丁 `affiliate-on-upstream.diff`（`git diff upstream/main..HEAD`），
在干净的 `upstream/main` 上 `git apply` 后得到的 tree 与合并分支完全一致（已实测）。
