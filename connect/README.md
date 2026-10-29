# Husic 连接助手（Chrome 扩展）

YouTube 没有给第三方播放器开放可用的登录接口。这个扩展帮你在一个**独立的无痕窗口**里登录 YouTube，再把这一次登录交给你自己的 Husic。它不读取日常浏览器的 Cookie，也不是 Google 官方的 OAuth。

## 安装

1. 在 Husic「设置 → YouTube Music」里点「下载扩展」，把压缩包解压到一个固定的位置。
2. 打开 Chrome 的 `chrome://extensions`，开启「开发者模式」，点「加载已解压的扩展程序」，选择刚才的文件夹。
3. 在扩展详情里开启「允许在无痕模式下运行」。
4. 回到 Husic 设置，点「已安装，继续连接」，在弹出的窗口里登录 Google。进入 YouTube Music 后，回到设置完成连接。

服务器真正播放成功一首测试音频后才会保存这次连接；失败时不会显示「已连接」。手机上不用装扩展，电脑连接好以后，手机也能用同一个服务器上的连接。

## 它会做什么、不会做什么

- 只读取自己开的那个无痕窗口里 `youtube.com` 的 Cookie，读完就关掉那个窗口。
- 权限只有：你的 Husic 站点、`youtube.com`、Cookie 和扩展自己的临时状态。
- 登录过程中请不要另开无痕窗口：Chrome 的无痕窗口共用一个会话。扩展发现已有无痕窗口时只会提示，不会替你关掉。
- 不改代理、网络、开机启动，也不碰其它扩展。

## 自己部署 HTTPS 实例

扩展默认只认 `http://127.0.0.1:18788`。如果你的 Husic 跑在别的地址，在仓库根目录生成对应的扩展包：

```bash
python3 connect/build.py --origin https://music.example.com
```

生成的文件在 `web/public/connect/Husic-Connect.zip`，重新构建网页后，设置页的「下载扩展」就会给出这个版本。

## 测试

```bash
node connect/test.mjs
```
