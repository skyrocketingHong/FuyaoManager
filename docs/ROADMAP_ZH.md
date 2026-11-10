# 内容管理平台路线图（MVP 迭代台账）

[English](./ROADMAP.md) | 简体中文

内容管理平台的敏捷台账。每个工作包交付可运行、已验证的增量；未实现并验证的能力不列为已完成。

## 已完成

1. **后端 MVP（本地开发）**（`admin/backend`）：PostgreSQL + SQL migrations、仅 Passkey 的 WebAuthn 认证（可发现凭据、强制 UV）、CLI（`migrate`、`serve`、`bootstrap-admin`、`create-site`）、带 CSRF/Origin 校验的 Cookie 会话、事务化 ETag 并发的站点列表/详情/配置。仅回环监听。契约：`contracts/openapi/management.yaml`。
2. **加固轮**：事务化首次登记闭环（咨询锁，并发完成仅一者成功）、verify 时点重检许可有效期、完整 ETag 的 If-Match 绑定（拒绝通配）、会话内稳定 CSRF 令牌（多标签页可用）、verify 路径请求体限额与单对象 JSON、TTL 配置错误显式失败、CORS 暴露 ETag。
3. **管理前端 MVP**（`admin/frontend`）：登记、登录/注销、站点列表/详情/配置（dirty 字段提交、412 冲突恢复、viewer 只读）；由契约生成的 API 类型与漂移检查；中英文 Crossfade；日夜主题；同源开发代理拓扑（5174 端口固定、仅回环）。非浏览器的协议联调（后端 serve + 开发代理 + Cookie/Origin 流程）已在本包验证中覆盖；真实平台认证器仪式与视觉验收仍由用户执行。

## 后续（按序，一次一个工作包）

4. 由用户驱动的真实浏览器回归（Touch ID / 硬件钥匙等真实平台认证器）与视觉验收。
5. 文章：列表、编辑、发布；复用 `site/` 脚本的 Markdown 管道。
6. 页面与版式模块；按站点划分的内容边界。
7. 站群分发与成员管理界面。
8. 服务端密钥配置、GitHub 源码更新、构建/部署与回滚入口，保留任务状态与失败记录。
9. 将 `site/scripts` 其余职责（媒体处理、校验、监听、外部数据缓存、版本管理、迁移、发布）逐项迁入后台；对照验收后停用旧入口。
10. Tailscale 私网接入与 30 分钟公网门禁；生产部署路径。

MVP 不做：导入生产数据、迁移真实密钥、拉取生产站部署。
