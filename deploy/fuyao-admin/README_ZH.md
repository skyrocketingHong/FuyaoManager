# fuyao-admin 服务器部署

[English](./README.md) | 简体中文

Fuyao 管理平台在目标服务器上的部署产物。部署刻意采用文件方式（构建产物 + 受控源码包，经 SSH 上传），不依赖 git push 或镜像仓库。

## 服务器目录布局

```text
/srv/fuyao-admin/
├── current -> releases/<commit-sha>     # 当前部署版本
├── releases/<commit-sha>/               # 程序：二进制 + 控制台 + 权威契约
│   ├── fuyao-admin                      # linux/amd64 二进制
│   ├── frontend/                        # 构建后的 React 控制台（vite dist）
│   └── contracts/                       # 管理 HTTP 与主题契约
├── themes/<theme-id>/<version>/         # 独立主题包、锁文件、site/ 和依赖
├── static-page/                         # 独立公网静态页，不含管理台 bundle
├── backups/                             # 受限访问的程序、数据库与配置备份
├── data/                                # 持久运行数据（绝不放在 releases/ 内）
│   ├── sites/                           # 按站点内容/配置树
│   ├── builds/                          # 构建任务工作区、快照、产物
│   └── releases/                        # 冻结发布目录 + 按站点 current 指针
└── etc/
    ├── db.env                           # FUYAO_ADMIN_DATABASE_URL（权限 600）
    └── config.env                       # 其余环境变量（权限 600）
```

程序与数据分离：更换 `current` 不影响内容、发布与数据库。

## 拓扑（已部署：HTTPS 域名）

- 生产管理台位于 `https://manager.fuyaoskyrocket.ing`。管理 API 只监听回环（`127.0.0.1:8787`），由 Caddy 终止 TLS 并反向代理。
- **公网** vhost 只提供 `static-page/` 下的独立静态页——不含管理台 bundle、不做任何代理，API 与预览路径显式返回 404。模板见 `caddy/manager.public.caddy.example`；该文件必须放在平台受管片段目录之外（受管片段由后端整体覆写）。
- **私网** vhost（同域名，绑定私网地址）反向代理到回环监听；两个 vhost 共用该域名自动签发的证书。私网入口配置、Split DNS 与完整切换手册按约定为运维本地文件（不提交仓库），见随服务器运维资料保存的运行手册。
- 私有构建预览保留自己的绑定/端口/URL 规则（平台分配端口，默认 `20000-20100`）；管理域名不是它们的统一替代。
- 已发布站点由系统 Caddy 经平台受管片段 `/etc/caddy/fuyao-admin/fuyao-admin.caddy` 服务（由 `/etc/caddy/Caddyfile` import）。私有入口只绑定私网地址；公开入口绑定全部接口并以公网 IP 通告。
- reload 特权：服务用户 `fuyao-admin` 仅可通过 sudoers 执行一个 root 命令（`/usr/local/sbin/fuyao-caddy-apply`，固定参数的校验+reload）；服务本身绝不由 root 运行。
- 通行密钥要求域名 RP（`FUYAO_ADMIN_RP_ID=manager.fuyaoskyrocket.ing`、HTTPS origins）；一次性 CLI 码是身份确认与恢复入口。

## 本目录文件

- `fuyao-admin.service` —— systemd 单元（低权限用户、cgroup 内存上限、2 GB 主机的 NODE_OPTIONS 堆调优）。
- `config.example.env` —— 公开的生产环境变量模板（HTTPS 域名拓扑）；私网绑定地址与预览 URL 由本地运维配置补充，升级时保留既有私网值。
- `fuyao-caddy-apply` —— 安装在 `/usr/local/sbin` 的运维控制校验+reload 脚本。
- `static-page/` —— 独立公网静态页（部署到独立目录，如 `/srv/fuyao-admin/static-page`，不属于管理台 bundle）。
- `caddy/manager.public.caddy.example` —— 公网 vhost 模板（仅静态页，API/预览路径显式 404）。

## 部署 / 升级流程

1. 备份数据库、当前程序和配置，记录当前指针；从本仓库根目录构建独立 release：
   ```bash
   manager_stage="$(mktemp -d "${TMPDIR:-/tmp}/fuyao-manager-release.XXXXXX")"
   mkdir -p "$manager_stage/frontend" "$manager_stage/contracts"
   CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go -C backend build -o "$manager_stage/fuyao-admin" ./cmd/fuyao-admin
   npm --prefix frontend run build
   cp -R frontend/dist/. "$manager_stage/frontend/"
   cp -R contracts/. "$manager_stage/contracts/"
   cp -R deploy/fuyao-admin/static-page "$manager_stage/static-page"
   chmod 0755 "$manager_stage/fuyao-admin"
   ```
   新检出或依赖变化时先执行 `npm --prefix frontend install`。任一步失败时停止打包，不替换线上指针；验证结束后可删除该临时目录。
2. 将已登记的每个主题版本保存在 `/srv/fuyao-admin/themes/<theme-id>/<version>/`，包含主题自身源码、package.json、锁文件与已安装依赖；保留仍被站点引用的版本。依赖由服务用户安装，root 安装后须恢复属主。
3. 上传到全新的 `/srv/fuyao-admin/releases/<sha>/`，核对二进制摘要和执行权限；静态页放入独立 `static-page/`。复用旧配置时，将 `FUYAO_ADMIN_THEME_CONTRACTS` 指向新版本的 `contracts/theme`，不能保留拆分前的 `theme/contracts/theme`。
4. 停止管理服务后，在服务用户可进入的新 release 目录中，加载运维环境并以新二进制执行 `migrate`；再用显式 `--contracts` 和每个主题包的 `site/` 路径执行 `register-theme`。成功后再原子切换 `current` 并启动服务。不能先重启一个要求新迁移的程序，也不能从不可访问的 root 私有目录运行服务用户 CLI。
5. 合并独立入口配置后，以实际 Caddy 重载账号执行完整 adapt、validate，并核对已发布站点监听，再热重载。受管目录须授予该账号必要读取、遍历及默认继承 ACL，否则不可读的 import 通配符可能被静默忽略；仅 root 检查通过不足以证明服务能读取。公网 IPv4 经 NAT，不能作为本机 bind 地址；私网代理入口必须显式绑定。私网 DNS 和完整操作命令按运维本地手册执行。
6. 验证服务、可信 TLS、私网认证状态及公网隔离，再允许清理旧程序。清理前解除主题登记及历史工作区依赖链接；依赖锁和安装记录不一致时停止清理。站点、快照和业务发布保留在 `data/` 中。

2026-11-12 已切换至应用提交 `dc0a4ad`，迁移版本为 0012，旧程序目录已清理；实际验证及尚未完成的人工验收见路线图 M5。

回滚：若旧程序目录已清理，先从受限备份归档恢复，再成套恢复对应程序指针、环境和入口配置。数据库不会随指针自动降级；回退前检查迁移与认证语义兼容性，失败时恢复最后良好配置，不反复重启错误版本。

## 运维

```bash
systemctl status fuyao-admin          # 状态
journalctl -u fuyao-admin -f          # 日志
systemctl restart fuyao-admin         # 重启（构建执行器会恢复中断任务）
fuyao-admin login-code --username fuyao   # 身份确认/恢复码（在服务器执行）
fuyao-admin revoke-sessions --username fuyao
```
