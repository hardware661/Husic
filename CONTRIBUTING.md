# 参与开发

欢迎提 Issue 和 PR。较大的改动请先开 Issue 聊聊想法。

## 目录

| 目录 | 内容 |
| --- | --- |
| `cmd/husic` | 服务端入口和启动参数 |
| `internal/api` | HTTP 接口 |
| `internal/source` | 各音乐平台的免登录搜索、播放、歌词 |
| `internal/netease`、`internal/platform`、`internal/youtube` | 需要账号的平台（网易云、B 站、YouTube Music） |
| `internal/resolve` | 一首歌在哪个平台能放 |
| `internal/store` | 曲库，PostgreSQL / SQLite |
| `web` | React 网页（电脑和手机共用） |
| `desktop` | Windows 客户端，见 [desktop/README.md](desktop/README.md) |
| `connect` | YouTube 登录用的 Chrome 扩展 |

新增一个免登录平台：在 `internal/source` 里实现 `Provider`（`Name`、`Search`、`Stream`，可选 `Lyrics`、`Chart`），然后在 `cmd/husic/main.go` 里按优先级加进 `providers`。

## 本地开发

```bash
npm --prefix web ci
npm --prefix web run dev            # 前端：http://127.0.0.1:5190，接口转发到 18788
go run ./cmd/husic -db-file …       # 或 HUSIC_DB=sqlite:./dev.db HUSIC_ACCOUNT_STATE=./.local/accounts go run ./cmd/husic
```

## 提交前

```bash
gofmt -l . && go vet ./... && go test ./...
npm --prefix web run check && npm --prefix web test && npm --prefix web run build
```

- 数据库测试默认只跑 SQLite。设置 `HUSIC_TEST_POSTGRES=postgres://…`（指向一个可以随意清空的空库）会同时跑 PostgreSQL。
- 浏览器回归测试见 [web/tests/README.md](web/tests/README.md)，需要 Python Playwright。
- 改了 Windows 客户端，请在 Windows 上跑 `desktop/test.ps1`。

修 bug 时请先写一个能复现问题的测试，确认它失败，再修。

## 风格

- Go 代码用 `gofmt`；注释写「为什么」，不写「做了什么」。
- 前端沿用现有组件和样式，界面风格见现有页面：黑白灰为主，封面保留自己的颜色。
- 不要提交账号、Cookie、数据库地址或任何私人部署信息。
