[English](./CHANGELOG.md) | **简体中文**

# 变更日志

本项目所有重要变更记录于此。格式基于
[Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/)；营销版本采用
`主版本.次版本`（内部为 `主版本.次版本.修订`）。

## 0.1.0 - 2026-11-12

### 新增

- 项目拆分：FuyaoManager 从 FuyaoHomepage 拆分而来，保留平台开发历史（`docs/SOURCE_MIGRATION.md` 记录拆分过程与提交映射）。
- 独立布局：`backend/`（Go module `github.com/skyrocketingHong/FuyaoManager/backend`）、`frontend/`（`fuyao-manager-frontend`，独立锁文件）、`contracts/`（管理 OpenAPI + 权威主题契约）、`deploy/fuyao-admin/` 及双语根文档。
- 内置契约默认：`FUYAO_ADMIN_THEME_CONTRACTS` 未设置时，`serve` 从仓库根解析 `contracts/theme`。
- 运行数据根默认改为 `<repo>/data/{builds,sites,releases}`，不再位于主题仓内工作树。
- 生产构建在站点 favicon 目录为空时，从主题自身 `static/favicon`（任务内已校验的源码副本）播种。
- 内置契约合法的测试主题（`backend/internal/httpapi/testdata/theme`），无需主题检出即可覆盖目录、选择、登记、发布管线与恢复测试。
- 跨仓真实主题联调入口：`FUYAO_TEST_THEME_ROOT` 启用六个真实 Node 构建测试；CI 增加手动触发的外部主题联调作业。

### 变更

- 管理台品牌、OpenAPI 标题与文档更名为 FuyaoManager；`fuyao-admin` CLI 二进制、`FUYAO_ADMIN_*` 变量、服务名与部署路径保持不变。

### 兼容性

- 数据库模式与迁移不变；主题身份（`fuyao-homepage`、`node22-sveltekit-static`）与契约协议标识不变。
