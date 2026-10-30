# Husic · AGENTS

自部署音乐播放器：Go 服务端 + React 网页（电脑/手机共用）+ Windows 客户端，多个音乐平台的歌放在一个曲库里。本仓是公开开源版。

## 先读

1. `README.md`（功能与部署）
2. `CONTRIBUTING.md`（目录、本地开发、提交前检查）

## 常用命令

```bash
gofmt -l . && go vet ./... && go test ./...
npm --prefix web run check && npm --prefix web test && npm --prefix web run build
```

## 分层

- `internal/source`：免登录平台，实现 `Provider`；`internal/netease`、`internal/platform`、`internal/youtube`：需账号的平台。
- `internal/resolve` 只决定"一首歌在哪个平台能放"，不直接拉流。
- `internal/api` 只做 HTTP，调下层；`internal/store` 同时支持 SQLite 与 PostgreSQL。

## 禁止

- 公开仓：不提交账号、Cookie、数据库地址、私人部署信息或交接记录（线上部署与交接放在作者自己的运维仓）。
- 修 bug 先写能复现的测试。
- 推送、打标签、发 Release 每一步都要主人当次同意。
