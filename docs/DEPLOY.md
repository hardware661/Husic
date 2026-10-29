# 部署说明

先读 README 的「三条命令跑起来」。这里补充放到服务器上时要注意的事。

## 为什么必须加登录

Husic 是个人播放器，没有用户系统。能打开页面的人可以：播放、收藏、改歌单，以及用你连接的网易云、B 站账号听歌。所以 Husic 默认**只接受本机的请求**，其它来源一律返回 403。要从别处访问，二选一：

### 方式一：只在私有网络里用

局域网、Tailscale / WireGuard 这类 VPN 里用，网络本身已经限制了谁能连进来：

```bash
HUSIC_ALLOW_REMOTE=true
```

Docker 的 `compose.yaml` 已经设了这个值，因为请求是从 Docker 自己的网络进到容器的。它默认只把端口开在 `127.0.0.1`；想让局域网访问，把 `ports` 改成 `"18788:18788"`。**不要**在公网服务器上这样开放端口。

### 方式二：公网，前面有登录

让反向代理负责登录，登录通过后再加一个只有它知道的请求头。Husic 校验这个请求头，没有就拒绝，就算有人绕过代理直连端口也没用。

1. 生成一个随机值，存成文件：

   ```bash
   openssl rand -hex 32 > gateway.key && chmod 600 gateway.key
   ```

2. Husic 启动时指定它（不需要再设 `HUSIC_ALLOW_REMOTE`）：

   ```bash
   HUSIC_GATEWAY_KEY_FILE=/path/to/gateway.key
   ```

3. 反向代理在**登录之后**给转发的请求加上 `X-Husic-Gateway: <文件里的值>`，并且先删掉客户端自己带来的同名请求头。nginx 示例（登录部分用你自己的方式，比如 `auth_request` 或 `auth_basic`）：

   ```nginx
   location / {
       auth_basic           "Husic";
       auth_basic_user_file /etc/nginx/husic.htpasswd;

       proxy_set_header X-Husic-Gateway "这里填 gateway.key 的内容";
       proxy_set_header Host $host;
       proxy_set_header X-Forwarded-Proto $scheme;
       proxy_pass http://127.0.0.1:18788;
       proxy_buffering off;   # 下载和音频转发更顺畅
   }
   ```

   `proxy_set_header` 会覆盖客户端带来的同名请求头。

请求头的名字可以用 `HUSIC_GATEWAY_HEADER` 改。

## 国内平台与代理

QQ 音乐、酷我、酷狗、咪咕、网易云大多只对国内网络提供完整播放。服务器在海外时，给 Husic 一个国内出口：

```bash
HUSIC_CN_PROXY=socks5h://127.0.0.1:1080   # 也支持 http://
```

这个代理只用于访问这些音乐平台，不会改系统代理。没有它时，这些平台的歌可能只能搜到、放不了，Husic 会自动尝试其它平台。

## 网易云账号

在「设置」里扫码连接即可，连接保存在 `HUSIC_ACCOUNT_STATE` 目录里（Docker 里是 `/data/accounts`）。这个目录里是登录凭据，注意备份和权限。

## YouTube Music

可选功能。需要在**运行 Husic 的机器上**装好 [yt-dlp](https://github.com/yt-dlp/yt-dlp) 和 [Deno](https://deno.com/)，然后：

```bash
HUSIC_YOUTUBE_TOOL=/usr/local/bin/yt-dlp
HUSIC_YOUTUBE_DENO=/usr/local/bin/deno
HUSIC_YOUTUBE_STATE=/path/to/youtube-state   # 一个私有的可写目录
```

两个路径都要写绝对路径。登录用浏览器扩展完成，见 [connect/README.md](../connect/README.md)。Docker 镜像里没有这两个工具，需要自己在镜像基础上加装。

## 备份

- SQLite：备份 `husic.db`（Docker 卷 `husic-data` 里）。备份前先停一下 Husic，或者用 `sqlite3 husic.db ".backup backup.db"`。
- PostgreSQL：`pg_dump`。
- 两种方式都要一起备份账号目录。

## 从 0.1 升级

- 数据库：PostgreSQL 的表结构没有变，直接用新版本打开原来的库就行。
- 环境变量 `MUSIC_DB` 改名为 `HUSIC_DB`；网关请求头的默认名字从 `X-Music-Gateway` 改为 `X-Husic-Gateway`，继续用旧名字的话请设 `HUSIC_GATEWAY_HEADER=X-Music-Gateway`。
- 现在必须设置 `-account-state`（`HUSIC_ACCOUNT_STATE`），启动时就会检查它能不能写。
- Windows 客户端第一次启动时，会把数据从 `%LOCALAPPDATA%\Music` 搬到 `%LOCALAPPDATA%\Husic`。
