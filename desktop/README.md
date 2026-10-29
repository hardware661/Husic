# Husic · Windows 客户端

一个单文件的绿色 `Husic.exe`：用 WebView2 打开你自己的 Husic 服务器，加上托盘、媒体键、透明桌面歌词和原生窗口外观。它不带后端或数据库，网页界面随服务器更新，原生外壳单独升级。

## 系统要求

- Windows 10 / 11 x64
- .NET Framework 4.7.2 或更新版本（系统一般自带）
- Microsoft Edge WebView2 运行时（Windows 11 自带；没有时客户端会提示去官网安装）

## 使用

1. 双击 `Husic.exe`。默认连接本机 `http://127.0.0.1:18788`，连接自己的服务器见下文「构建」。
2. 关闭窗口默认收进托盘继续播放。单击托盘图标打开窗口；右键菜单有当前歌曲、播控、桌面歌词、窗口置顶和退出。
3. 第二次双击只会唤起原来的窗口，不会开第二个播放器。
4. 客户端不注册开机自启，也不改默认浏览器、代理或系统网络。

电脑设置页里有一节「桌面体验」，可以调整：

| 选项 | 说明 |
| --- | --- |
| 桌面歌词 | 透明浮窗，横排两行或竖排两列；可调颜色、字号范围、底板，锁定后鼠标点击会穿透 |
| 关闭窗口时 | 收进托盘继续听（默认），或直接退出 |
| 窗口置顶 | 默认关闭，也可以从托盘切换 |
| 界面大小 | 85% / 100% / 115% / 125%，只缩放网页内容 |
| 窗口大小 | 70%–125%，下次启动居中打开；也可以「保存当前窗口大小」 |

深浅色和「减少动态效果」跟随网页设置，不另设一套。

## 数据在哪里

客户端数据都在 `%LOCALAPPDATA%\Husic\`：

- `WebView\`：客户端自己的网页登录、播放队列、偏好、背景图，以及下载和导入的音频副本。
- `desktop.json`：桌面偏好（关闭行为、置顶、缩放、窗口尺寸、歌词设置）。
- `Runtime\`：校验过的 WebView2 Loader。

0.1 版本的数据放在 `%LOCALAPPDATA%\Music\`。0.2 第一次启动时会自动搬到新位置；如果旧版还开着、搬不动，就继续用原来的位置，不会丢数据。

升级时退出客户端、替换 exe 即可。删除 exe 不会删除这些数据。退出后运行 `Husic.exe --reset-ui` 可以把客户端恢复到初始状态：会清空本机队列、偏好、图片和下载副本，服务器上的曲库不受影响。

## 构建

需要 Windows 自带的 .NET Framework C# 编译器，不用另装 .NET SDK、Rust 或 Visual Studio。WebView2 SDK 下载到 `desktop/vendor/`，版本、官方地址和 SHA256 锁定在 `packages.lock.json`。

```powershell
python desktop/fetch-sdk.py
powershell -NoProfile -ExecutionPolicy Bypass -File desktop/build.ps1
```

产物是 `desktop/dist/Husic.exe`。连接自己的 HTTPS 服务器：

```powershell
./desktop/build.ps1 -ServerOrigin https://music.example.com
```

服务器前面另有一个独立的登录页面时，再加 `-LoginOrigin https://login.example.com`。HTTP 只允许本机回环地址。

在 WSL 里构建时，先用 `wslpath -w` 把脚本路径转换成 Windows 路径，再交给 `powershell.exe`。

## 测试

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File desktop/test.ps1
```

测试会另外生成一个 `Husic.QA.exe`（不随包分发），用虚拟 HTTPS 站点、项目真实样式和静音 WAV，在 `%LOCALAPPDATA%\Husic\QA\` 的隔离目录里运行，检查真实的鼠标拖动、边角拉伸、窗口按钮、托盘、单实例、偏好保存和导航边界。截图和结果 JSON 保存在 `desktop/build/`。

可以只跑某一组：`-LyricsOnly`、`-WindowOnly`、`-FontOnly`、`-RestoreOnly`、`-StartupOnly`、`-SecondaryOnly`、`-ResetOnly`、`-AudioOnly`。

物理媒体键、长时间连续播放还没有自动化测试。

## 代码结构

| 文件 | 负责 |
| --- | --- |
| `Bootstrap.cs` | 入口、内嵌资源、WebView2 Loader |
| `DesktopForm.cs` | 主窗口、WebView2、导航边界、网页消息 |
| `DesktopChrome.cs`、`desktop-chrome.js/css` | 原生窗口外观、窗口按钮、桌面设置区 |
| `DesktopState.cs` | 构建配置、数据目录、桌面偏好 |
| `TrayMenu.cs` | 自绘托盘菜单 |
| `DesktopLyrics.cs`、`LayeredSurface.cs` | 逐像素透明的桌面歌词浮窗 |
| `SingleInstance.cs` | 单实例与二次启动唤起 |
| `desktop-bridge.js` | 网页播放状态、媒体控制与原生之间的桥 |
| `DesktopTests.cs` | 只编进 `Husic.QA.exe` 的原生测试 |

边界：网页不能执行本机命令、读写任意文件或打开任意 URL。原生层只接收固定格式的播放状态、64px 封面、枚举化的桌面偏好和固定的窗口动作。只嵌入你的 Husic 站点（以及可选的登录站点），外部 HTTPS 链接只在用户点击时交给系统浏览器。新增原生能力时请保持这种固定白名单的做法。

参考：[WebView2 WinForms](https://learn.microsoft.com/microsoft-edge/webview2/get-started/winforms)、[WebView2 安全最佳实践](https://learn.microsoft.com/microsoft-edge/webview2/concepts/security)。
