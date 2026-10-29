# Husic

**自己部署的音乐播放器。** 网易云、QQ 音乐、酷我、酷狗、咪咕、B 站、YouTube Music 的歌放在一个地方听：一首歌在哪个平台能放就自动用哪个，歌词逐字滚动，电脑、手机、Windows 客户端用同一个曲库。

![Husic 播放页：封面取色背景与逐字歌词](docs/screenshots/lyrics.webp)

## 亮点

- **一首歌，自动找能放的版本。** 搜索时把各平台的同一首歌合在一起；播放时按顺序找能放完整版的平台，某个平台失效了就自动换下一个，不用你自己挑音源。
- **歌词好看。** 逐行、逐字高亮，有翻译时一起显示；播放页背景取自封面颜色。
- **发现新歌的小玩法。** 盲听（先听 8 秒再揭晓是谁）、新歌雷达、每日推荐和私人电台。
- **到处都能听。** 电脑网页、手机网页（可加到主屏幕），还有一个约 1 MB 的 Windows 绿色客户端：托盘、媒体键、透明桌面歌词。
- **离线也能听。** 歌曲和整张歌单可以下载到本机，也能导入自己的 MP3 / FLAC。

| 首页 | 发现 | 手机 |
| --- | --- | --- |
| ![首页](docs/screenshots/home.webp) | ![发现](docs/screenshots/discover.webp) | ![手机](docs/screenshots/mobile.webp) |

## 三条命令跑起来

需要 Docker：

```bash
git clone https://github.com/hardware661/Husic.git && cd Husic
docker compose up -d
# 打开 http://127.0.0.1:18788
```

曲库保存在 Docker 卷里的一个 SQLite 文件中。想用 PostgreSQL，换成 `docker compose -f compose.postgres.yaml up -d`。

不连接任何账号也能搜索、播放公开内容。需要会员歌曲、每日推荐和你自己的歌单时，在「设置」里扫码连接网易云或 B 站。

## 放到服务器上

Husic 是**个人播放器，没有用户系统**：收藏、歌单和平台账号属于整个实例，谁能打开页面谁就能用。所以放到公网前，一定要在前面加一层登录，比如反向代理的 Basic Auth、Cloudflare Access、Authelia 之类。

- 只在家里局域网或 VPN 里用：设置 `HUSIC_ALLOW_REMOTE=true`，把端口开到局域网即可。
- 在公网用：让反向代理在登录通过后加上一个请求头，Husic 用 `-gateway-key-file` 校验它，没有这个头的请求一律拒绝。详见 [docs/DEPLOY.md](docs/DEPLOY.md)。

服务器在海外时，国内平台大多需要一个国内出口才能播放，用 `HUSIC_CN_PROXY=socks5h://…` 指给 Husic 即可，它只用于访问国内音乐平台，不改系统代理。

## Windows 客户端

在 [Releases](https://github.com/hardware661/Husic/releases) 下载 `Husic.exe`，双击即可，默认连接本机的 `http://127.0.0.1:18788`。连接自己的服务器、从源码构建，见 [desktop/README.md](desktop/README.md)。

## 不用 Docker 运行

需要 Go 1.26、Node.js 22 和 Python 3（构建时生成浏览器扩展包）。

```bash
npm --prefix web ci && npm --prefix web run build
go build -o bin/husic ./cmd/husic
HUSIC_DB=sqlite:./husic.db HUSIC_ACCOUNT_STATE=./accounts ./bin/husic
```

所有参数见 `./bin/husic -h`，每个参数都可以用 `HUSIC_` 开头的环境变量设置，例如 `-account-state` 对应 `HUSIC_ACCOUNT_STATE`。数据库写 `postgres://…` 用 PostgreSQL，写 `sqlite:路径` 用单个文件。

## 说明

- 界面目前只有中文。
- YouTube Music 是可选功能：服务器上要另装 yt-dlp 和 Deno（Docker 镜像里没有），登录要用一个 Chrome 扩展。见 [docs/DEPLOY.md](docs/DEPLOY.md#youtube-music) 和 [connect/README.md](connect/README.md)。
- 能不能播放、音质如何，取决于各平台和你连接的账号。Husic 不提供任何音乐内容，也不绕过平台的付费限制。

## 参与开发

见 [CONTRIBUTING.md](CONTRIBUTING.md)。改动记录见 [CHANGELOG.md](CHANGELOG.md)。

## 许可

[MIT](LICENSE)。第三方组件（如 Microsoft WebView2）保留各自的许可，见 [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md)。本许可只覆盖 Husic 自己的代码，不授予任何音乐内容或第三方服务的权利。
