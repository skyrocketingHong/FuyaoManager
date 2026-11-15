[English](./CI_UPDATES.md) | **简体中文**

# CI 构建与应用更新

GitHub Actions 使用 PostgreSQL 验证后端，验证并构建一次前端，再交叉编译 Linux amd64 应用和更新管理器。main、dev 通道的成功构建发布为不可变预发布版本，名称为 `build-<run-id>-<attempt>`。构建标识与产品版本分别维护。PR 不发布可部署版本；真实主题联调保留为独立的手动输入选项。

更新架构与维护流程移植自机器人平台的 `deploy/server/manager`、`backend/internal/httpapi/platform_update.go`、`frontend/lib/platformMaintenance.ts` 和 `PlatformComponentsPage`。GitHub 替代 GitLab，systemd 不可变程序目录替代 Docker 槽位。FuyaoManager 保留一个生产应用及 main、dev 两个更新通道，PostgreSQL、主题、站点数据、已发布网站和 Caddy 沿用现有持久位置。

## 组件分工

| 组件                                | 职责                                                       |
| ----------------------------------- | ---------------------------------------------------------- |
| `.github/workflows/ci.yml`          | 测试、一次前端构建、Linux 构包和不可变 CI 预发布版本       |
| `scripts/package-release.py`        | 交叉编译、构建身份、逐文件 SHA-256 清单和归档              |
| `backend/cmd/fuyao-manager-updater` | 独立且由 root 持有的 systemd 更新管理器                    |
| `backend/internal/platformupdate`   | GitHub 来源核验、解包、任务受理、切换日志、激活与恢复      |
| `/api/v1/admin/updates`             | 完整管理员和 CSRF 校验，通过固定 Unix socket 代理          |
| `/updates`                          | 检查版本、指定部署、通道、自动更新、钉定、回滚及任务和日志 |

## 更新过程

管理器选择本仓库 `ci.yml` 的成功运行，排除 fork 和 PR。安装前重新核验具体运行及尝试次数，下载对应 Release 资产，核对 GitHub 提供的 SHA-256，以及清单中的仓库、提交、运行号、尝试次数、通道和架构。解包拒绝链接、重复条目、路径穿越及超限归档；逐文件核对 SHA-256，可执行文件须为 Linux x86-64 ELF。

程序包在停止应用前暂存完成。固定且由 root 安装的准备命令先备份 PostgreSQL 和配置，再以 `fuyao-admin` 身份执行候选 CLI。管理器持久记录切换任务，停止 `fuyao-admin.service`、原子切换 `current`、启动服务，并通过 `/api/v1/version` 核对完整提交。失败时恢复并验证上一程序；切换中断后，由重启的管理器恢复原任务，不重新提交部署。应用验证通过后，管理器从同一已校验包替换自身。

页面区分任务受理、验证成功、准备失败、已验证回退及需要运维处理的恢复状态。HTTP 请求中断后只读取原任务，不自动重发写操作。来源或通道变化会清除已检查候选；版本钉定同时阻止手动和自动部署。首次安装默认关闭自动更新，启用后每 5 分钟检查一次。

数据库 schema 变化需由运维单独迁移。博客数据库迁移须先停止写入并备份数据库及旧内容，再运行候选程序迁移并核对全部作用域，成功后切换服务。自动更新和回退只接受与当前程序 schema 相同的包；不同 schema 的旧程序不再显示为回退候选。程序回退不恢复数据库备份，也不覆盖网站内容。已发布网站使用各自的冻结产物，不随管理程序迁移改变。

## 安装与配置

首次安装由运维执行，服务器需安装 Python 3 及与数据库服务器兼容的 PostgreSQL 客户端工具。从成功 CI 预发布版本取得产物并验证资产摘要，切换前保留旧程序及数据库备份。管理器的 `stage` 运维命令与日常更新使用相同的程序包校验器：

```text
fuyao-manager-updater stage ARCHIVE NEW_RELEASE_DIR OWNER/REPO COMMIT RUN_ID ATTEMPT BRANCH --verified-download
```

安装 `fuyao-manager-updater.service`，将管理器程序置于 `/srv/fuyao-admin/updater/`，将 `prepare-update.sh` 安装为 `/usr/local/libexec/fuyao-manager-prepare`。这些文件及其父目录、`/srv/fuyao-admin/etc/{db.env,config.env,updater.json}`、程序 release 目录均须由 root 持有，应用账号无写权限。初始配置参考 `updater.example.json`，在应用现有环境文件中配置 `FUYAO_ADMIN_UPDATER_SOCKET=/run/fuyao-manager-updater/control.sock`。执行 `systemctl daemon-reload` 后，启用 `fuyao-manager-updater.service` 并重启 `fuyao-admin.service`。

只有 `fuyao-admin` Unix 组可以访问管理器 socket。管理器不监听 TCP，不提供通用命令接口，浏览器不能指定更新仓库。仓库及部署命令仅由运维调整。公开 Release 资产使用匿名访问；维护页可选配置或清除只读令牌以提高 GitHub API 配额。令牌以 0600 权限单独保存，不返回浏览器，不写审计。任务状态和令牌位于仅 root 可访问的 `/srv/fuyao-admin/update/`。

安装后核对两个服务及 `/api/v1/version`，并确认应用账号不能写安装目录或读取令牌。后续从“平台设置 → 平台更新”操作。运维核验完成前保留备份与上一程序；清理旧 release 时须保留外部主题的依赖引用。

三个 npm 版本清单及 Go 默认版本通过 `npm run version:set -- Y.M.P` 从根产品版本统一同步。修改年度标识和第二段前，继续遵循仓库的授权规则。
