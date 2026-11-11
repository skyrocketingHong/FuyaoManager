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

## 拓扑

- 管理 API 与控制台只监听服务器 Tailscale 地址（`100.117.154.115:8787`）；公网 IP 连接到不了服务。
- 私有构建预览绑定同一 Tailscale 地址，端口由平台分配（默认范围 `20000-20100`）；预览 URL 携带 Tailscale 基址，绝不返回 `127.0.0.1`。
- 已发布站点由系统 Caddy 经平台受管片段 `/etc/caddy/fuyao-admin/fuyao-admin.caddy` 服务（由 `/etc/caddy/Caddyfile` import）。私有入口只绑定 Tailscale 地址；公开入口绑定全部接口并以公网 IP 通告。
- reload 特权：服务用户 `fuyao-admin` 仅可通过 sudoers 执行一个 root 命令（`/usr/local/sbin/fuyao-caddy-apply`，固定参数的校验+reload）；服务本身绝不由 root 运行。

## 本目录文件

- `fuyao-admin.service` —— systemd 单元（低权限用户、cgroup 内存上限、2 GB 主机的 NODE_OPTIONS 堆调优）。
- `config.example.env` —— 与线上部署一致的环境变量模板。
- `fuyao-caddy-apply` —— 安装在 `/usr/local/sbin` 的运维控制校验+reload 脚本。

## 部署 / 升级流程

1. 在仓库根目录本地构建：
   ```bash
   CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go -C admin/backend build -o <stage>/fuyao-admin ./cmd/fuyao-admin
   npm run admin:build && cp -R admin/frontend/dist <stage>/frontend
   rsync -a --exclude node_modules --exclude .fuyao --exclude build --exclude .svelte-kit --exclude .DS_Store site/ <stage>/theme/site/
   cp package.json package-lock.json .npmrc <stage>/theme/
   cp -R contracts/theme <stage>/theme/contracts/theme
   ```
2. 上传至 `/srv/fuyao-admin/releases/<sha>/`，`chown -R fuyao-admin:fuyao-admin`，以服务用户执行 `npm ci`（或 root 安装后整体 `chown -R`）。
3. 重指 `current`，`systemctl restart fuyao-admin`。
4. 重新注册首主题（身份一致的重复登记会刷新记录的 source 路径）：
   ```bash
   cd /srv/fuyao-admin/current && . /srv/fuyao-admin/etc/db.env && export FUYAO_ADMIN_DATABASE_URL
   ./fuyao-admin migrate                 # 如有新迁移
   ./fuyao-admin register-theme --contracts theme/contracts/theme theme/site
   ```
5. 验证：`systemctl is-active fuyao-admin`、Tailscale HTTP 可达控制台、公网探测失败、`journalctl -u fuyao-admin` 出现 `edge applied`。

回滚：将 `current` 指回上一 release 目录并重启；运行数据与数据库模式在同一迁移序列内保持兼容（不支持跨已应用迁移降级）。

## 运维

```bash
systemctl status fuyao-admin          # 状态
journalctl -u fuyao-admin -f          # 日志
systemctl restart fuyao-admin         # 重启（构建执行器会恢复中断任务）
fuyao-admin login-code --username fuyao   # 每次登录的一次性码（在服务器执行）
fuyao-admin revoke-sessions --username fuyao
```
