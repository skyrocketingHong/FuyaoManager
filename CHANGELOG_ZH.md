[English](./CHANGELOG.md) | **简体中文**

# 变更日志

本项目所有重要变更记录于此。格式基于
[Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/)；产品版本采用
`Y.M.P` 三段式，API 和主题契约版本独立维护。

## 0.2.1 - 2026-11-13

### 修复

- 站点资料页在加载完成后增加 Hook 调用而触发 React #310；顶部插槽始终无条件注册，加载失败可重试。路由异常显示可恢复的错误页。
- 为 antd 启用样式分层，恢复参考平台共享 CSS 的优先级；继续复用侧栏分区、顶栏、Lucide 图标和紧凑表格，平台首页入口不再与菜单重复。
- 切换语言及时更新页面文案且保留草稿；站点资料保存锁定编辑并采用服务器规范值；站点列表缓存隔离账号代数，丢弃旧账号迟到响应。
- 设置页测试显式卸载组件，取消 antd 延迟更新，消除测试环境拆卸后的未处理异常。

## 0.2.0 - 2026-11-13

### 新增

- 账号管理后端基础：平台 admin/member 角色、账号生命周期与登记码事务、
  凭据撤销、最后管理员及有效站点所有者保护。用户管理 HTTP 接口、界面和
  操作日志仍待后续完成。
- 按路由切换的“Fuyao Manager - 页面名称”标题，以及管理台和独立公网入口
  共用的 SVG favicon。
- 自行修改用户名：`PATCH /api/v1/account/profile` 原子改写调用者自己的账号
  （冲突返回 409 `username_taken`）；账号设置页从账号下拉进入，通行密钥与
  登录码在改名后继续有效。
- 默认运行隔离：`FUYAO_ADMIN_DEPLOYMENT_ENABLED` 关闭（默认）时，建站、
  内容、构建与私有预览可用，发布、回滚、可见性变更与 Caddy 应用返回
  `503 deployment_disabled`；边缘状态报告 enabled/isolated 事实与发布指针
  状态。
- 确定性运行根：仓库根从工作目录向上发现（或显式配置），三个运行根必须
  解析为相互独立的真实目录，供应写入绝不覆盖既有站点文件。

### 变更

- 平台首页和站点总览使用独立的侧栏固定入口；复用机器人平台的通用侧栏
  组件及共享表面样式。
- 关闭预览时撤销令牌、停止监听、释放端口并结束页面生命周期；实例探针
  防止旧页面在端口复用后继续存活，预览入口拒绝 Service Worker 注册。
- 会话处理：业务请求 401 在客户端边界使旧会话过期，路由可进入登录页；
  仪式失败与网络错误绝不伪装退出，代数守卫防止旧响应覆盖新登录。
- 管理台表面与共享控制台模式对齐（面板结构、表格与控件密度、单一表
  表面），公网静态页改题为“Fuyao Manager - 访问说明”并加入同源 favicon。

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
