# 前端浏览器回归

检查使用隔离的浏览器上下文、演示数据和有效的静音 WAV，不连接个人音乐账号。需要环境已有 Python Playwright 和 Chromium；应用的 Node 依赖仍安装在 `web/` 内。

先在仓库根目录启动前端：

```bash
npm --prefix web run dev
```

再在另一个终端运行：

```bash
MUSIC_TEST_URL=http://127.0.0.1:5190 python3 -m unittest discover -s web/tests -p 'test_*.py'
```

也可以用 `MUSIC_TEST_URL` 指向本地生产构建的预览服务。测试会拦截 API 和测试音频请求，不要把地址指向个人线上实例。

| 文件 | 覆盖内容 |
| --- | --- |
| `test_controls.py` | 歌单改名、菜单、导航与单曲下载 |
| `test_bulk_download.py` | 批量下载、去重、取消、存储空间与桌面窗框避让 |
| `test_mobile.py` | 手机布局、播放页、盲听与定时停止 |
| `test_foreground.py`、`test_touch_ui.py` | 触摸手势、选字、输入、缓存与滚动恢复 |
| `test_visibility.py`、`test_desktop_mobile_boundary.py` | 可见窗口失焦、恢复、桌面历史与榜单预取 |
| `test_power.py`、`test_background_wake.py` | 隐藏呈现、原生音频推进、必要回调与定时精度 |
| `test_native_audio.py` | 同曲原生对照、音频交接和外站脚本隔离 |

Chromium 的手机视口和合成页面事件不等于真实手机、Safari 锁屏或温度测量。Windows 壳的原生操作另由 `desktop/test.ps1` 验证。
