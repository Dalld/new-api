# 推荐计划与渠道状态中文化实施计划

**目标：** 合并压缩包的黑金推荐计划 UI，补齐返佣与探针中文资源，在公共导航增加“渠道状态”，并发布到测试服务器。

### Task 1: 推荐计划 UI 合并

**Files:**
- Modify: `web/src/features/my-affiliate/index.tsx`
- Modify: `web/src/features/my-affiliate/__tests__/status-page.test.tsx` 或现有对应测试

- [ ] 将压缩包黑金邀请卡移植到当前组件。
- [ ] 保留当前查询、URL 搜索参数、错误处理、移动端列表与分页。
- [ ] 覆盖邀请链接复制、统计值、加载和窄屏布局测试。

### Task 2: 推荐计划与探针国际化

**Files:**
- Modify: `web/src/i18n/locales/zh.json`
- Modify: `web/src/i18n/locales/zh-TW.json`
- Modify: related affiliate/probe components with hard-coded English
- Create: focused locale completeness test

- [ ] 收集用户、管理员、设置、钱包和公共状态页全部源键。
- [ ] 补齐简体和繁体中文翻译。
- [ ] 将硬编码英文改为 `t()`。
- [ ] 用测试断言关键中文键存在且不是英文回退。

### Task 3: 公共导航入口

**Files:**
- Modify: public navigation configuration/components
- Modify: related navigation tests

- [ ] 桌面导航和移动菜单加入 `/status`。
- [ ] 源键使用 `Channel Status`，中文翻译为“渠道状态”。
- [ ] 入口不依赖登录，不改变管理端侧栏权限。

### Task 4: 本地验证

- [ ] 运行相关前端测试。
- [ ] 运行 `bun run typecheck`。
- [ ] 运行生产构建。
- [ ] 运行 `git diff --check` 并审查变更范围。

### Task 5: 测试服务器发布

- [ ] 生成排除 `.git`、缓存和构建产物的源码归档及 SHA256。
- [ ] 上传归档并在服务器构建新镜像。
- [ ] 只替换 New API 镜像，不覆盖 `.env`、MySQL、Redis 和 `baota_net`。
- [ ] 验证容器健康、数据库迁移、公开 API 和三组探针。
- [ ] 浏览器验收首页导航、推荐计划和 `/status` 的桌面/窄屏中文页面。

### Task 6: 生产更新清单

- [ ] 列出提交、文件、迁移、构建参数、部署命令和验收命令。
- [ ] 记录源码 SHA256、新旧镜像 ID 和回滚命令。
