[English](./README.md) | **简体中文**

# FuyaoManager 后端

Go 服务负责认证、内容存储、站点配置、构建、预览与发布。`internal/content` 提供按来源隔离、带 ETag 的文件写入，`internal/httpapi` 实现管理接口，`internal/store` 负责数据库访问；CLI 入口为 `cmd/fuyao-admin`。

环境准备见[项目说明](../README_ZH.md)，环境变量见[配置说明](../CONFIGURATION_ZH.md)，内容来源与媒体边界见[内容工作区说明](../docs/CONTENT_WORKSPACES_ZH.md)。

```bash
go -C backend test ./...
go -C backend vet ./...
go -C backend build ./...
```

数据库集成测试使用独立的 `fuyao_mvp_test` 数据库。真实主题测试需要将 `FUYAO_TEST_THEME_ROOT` 指向已安装依赖的 FuyaoHomepage 主题目录。
