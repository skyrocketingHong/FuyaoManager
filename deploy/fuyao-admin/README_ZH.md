# fuyao-admin 服务器部署

[English](./README.md) | 简体中文

Fuyao 管理平台在目标服务器上的部署产物。部署刻意采用文件方式（构建产物 + 受控源码包，经 SSH 上传），不依赖 git push 或镜像仓库。

## 服务器目录布局

```text
/srv/fuyao-admin/
├── current -> releases/<commit-sha>     # 当前部署版本
├── releases/<commit-sha>/               # 程序：二进制 + 控制台 + 主题源码
│   ├── fuyao-admin                      # linux/amd64 二进制
│   ├── frontend/                        # 构建后的 React 控制台（vite dist）
│   └── theme/                           # 受控源码包：package.json、锁文件、
│                                        # site/（首主题）、contracts/theme/
│       └── node_modules/                # npm ci 产物（属主 fuyao-admin）
├── data/                                # 持久运行数据（绝不放在 releases/ 内）
│   ├── sites/                           # 按站点内容/配置树
│   ├── builds/                          # 构建任务工作区、快照、产物
│   └── releases/                        # 冻结发布目录 + 按站点 current 指针
└── etc/
    ├── db.env                           # FUYAO_ADMIN_DATABASE_URL（权限 600）
    └── config.env                       # 其余环境变量（权限 600）
```

程序与数据分离：更换 `current` 不影响内容、发布与数据库。

## 拓扑（目标：HTTPS 域名）

- 生产管理台位于 `https://manager.fuyaoskyrocket.ing`。管理 API 只监听回环（`127.0.0.1:8787`），由 Caddy 终止 TLS 并反向代理。
- **公网** vhost 只提供 `static-page/` 下的独立静态页——不含管理台 bundle、不做任何代理，API 与预览路径显式返回 404。模板见 `caddy/manager.public.caddy.example`；该文件必须放在平台受管片段目录之外（受管片段由后端整体覆写）。
- **私网** vhost（同域名，绑定私网地址）反向代理到回环监听；两个 vhost 共用该域名自动签发的证书。私网入口配置、Split DNS 与完整切换手册按约定为运维本地文件（不提交仓库），见随服务器运维资料保存的运行手册。
- 私有构建预览保留自己的绑定/端口/URL 规则（平台分配端口，默认 `20000-20100`）；管理域名不是它们的统一替代。
- 已发布站点由系统 Caddy 经平台受管片段 `/etc/caddy/fuyao-admin/fuyao-admin.caddy` 服务（由 `/etc/caddy/Caddyfile` import）。私有入口只绑定私网地址；公开入口绑定全部接口并以公网 IP 通告。
- reload 特权：服务用户 `fuyao-admin` 仅可通过 sudoers 执行一个 root 命令（`/usr/local/sbin/fuyao-caddy-apply`，固定参数的校验+reload）；服务本身绝不由 root 运行。
- 通行密钥要求域名 RP（`FUYAO_ADMIN_RP_ID=manager.fuyaoskyrocket.ing`、HTTPS origins）；一次性 CLI 码是身份确认与恢复入口。

## 本目录文件

- `fuyao-admin.service` —— systemd 单元（低权限用户、cgroup 内存上限、2 GB 主机的 NODE_OPTIONS 堆调优）。
- `config.example.env` —— 生产环境变量模板（HTTPS 域名拓扑）。
- `fuyao-caddy-apply` —— 安装在 `/usr/local/sbin` 的运维控制校验+reload 脚本。
- `static-page/` —— 独立公网静态页（部署到独立目录，如 `/srv/fuyao-admin/static-page`，不属于管理台 bundle）。
- `caddy/manager.public.caddy.example` —— 公网 vhost 模板（仅静态页，API/预览路径显式 404）。

## 部署 / 升级流程

1. 从本仓库根目录构建平台 release：
   ```bash
   CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go -C backend build -o <stage>/fuyao-admin ./cmd/fuyao-admin
   npm --prefix frontend ci && npm --prefix frontend run build && cp -R frontend/dist <stage>/frontend
   cp -R contracts <stage>/contracts
   ```
2. 从主题项目自身的检出（FuyaoHomepage）加入首主题包：
   ```bash
   rsync -a --exclude node_modules --exclude .fuyao --exclude build --exclude .svelte-kit --exclude .DS_Store \
     /path/to/FuyaoHomepage/site/ <stage>/theme/site/
   cp /path/to/FuyaoHomepage/package.json /path/to/FuyaoHomepage/package-lock.json /path/to/FuyaoHomepage/.npmrc <stage>/theme/
   ```
3. 上传至 `/srv/fuyao-admin/releases/<sha>/`，`chown -R fuyao-admin:fuyao-admin`，以服务用户在 `<stage>/theme` 内执行 `npm ci`（或 root 安装后整体 `chown -R`）。
4. 重指 `current`，`systemctl restart fuyao-admin`。
5. 重新注册首主题（身份一致的重复登记会刷新记录的 source 路径）：
   ```bash
   cd /srv/fuyao-admin/current && . /srv/fuyao-admin/etc/db.env && export FUYAO_ADMIN_DATABASE_URL
   ./fuyao-admin migrate                 # 如有新迁移
   ./fuyao-admin register-theme --contracts contracts/theme theme/site
   ```
6. 验证：`systemctl is-active fuyao-admin`；公网域名只出静态页（API 路径 404）；私网经 HTTPS 进入管理台（可完成绑定与通行密钥登录）；公网探测管理端口失败；`journalctl -u fuyao-admin` 出现 `edge applied`。

当前已部署服务器仍运行拆分前的单仓 release（主题契约与 repo root 位于 `<current>/theme`）；待路线图的部署切换执行时再换到本布局。

回滚：将 `current` 指回上一 release 目录并重启；运行数据与数据库模式在同一迁移序列内保持兼容（不支持跨已应用迁移降级）。

## 运维

```bash
systemctl status fuyao-admin          # 状态
journalctl -u fuyao-admin -f          # 日志
systemctl restart fuyao-admin         # 重启（构建执行器会恢复中断任务）
fuyao-admin login-code --username fuyao   # 身份确认/恢复码（在服务器执行）
fuyao-admin revoke-sessions --username fuyao
```
