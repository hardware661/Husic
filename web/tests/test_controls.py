"""User-facing playlist, navigation and mini-player checks against an isolated instance.

Start the frontend, then run MUSIC_TEST_URL=http://127.0.0.1:5190 python3 web/tests/test_controls.py.
Requires an existing Python Playwright installation and Chromium.
"""
import io
import json
import os
import unittest
import wave
from urllib.parse import urlsplit
from playwright.sync_api import expect, sync_playwright


class Controls(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.runtime = sync_playwright().start()
        cls.browser = cls.runtime.chromium.launch(headless=True)
        audio = io.BytesIO()
        with wave.open(audio, 'wb') as wav:
            wav.setnchannels(1); wav.setsampwidth(2); wav.setframerate(8000)
            wav.writeframes(b'\0\0' * 8000 * 30)
        cls.audio = audio.getvalue()

    @classmethod
    def tearDownClass(cls):
        cls.browser.close(); cls.runtime.stop()

    def setUp(self):
        self.context = self.browser.new_context(viewport={'width': 1280, 'height': 860}, reduced_motion='reduce')
        self.context.add_init_script('if(!localStorage.getItem("player"))localStorage.setItem("player",JSON.stringify({volume:0}))')
        self.page = self.context.new_page()
        self.page.set_default_timeout(3500)
        self.errors = []; self.patches = []; self.downloads = []; self.hold_download = False; self.fail_rename = False
        self.page.on('pageerror', lambda error: self.errors.append(str(error)))
        self.item = dict(songId=11, title='测试曲目', artists=['演示歌手'], album='演示专辑', duration=30,
                         cover='', liked=False, copies=[dict(source='netease', id='11', title='测试曲目', artists=['演示歌手'], duration=30)])
        self.playlist = dict(id=1, name='午后慢慢听', description='保留原简介', cover='/test-cover.svg', origin='',
                             count=1, covers=[], updatedAt='2026-10-23T00:00:00Z')
        self.context.route('**/api/**', self.route)
        self.context.route('**/fixture.wav', lambda r: r.fulfill(body=self.audio, content_type='audio/wav'))
        self.context.route('**/test-cover.svg', lambda r: r.fulfill(content_type='image/svg+xml', body='<svg xmlns="http://www.w3.org/2000/svg" width="100" height="100"><rect width="100" height="100" fill="#aaa"/></svg>'))

    def tearDown(self):
        self.context.close()
        self.assertEqual(self.errors, [])

    def route(self, route):
        path = urlsplit(route.request.url).path
        if path == '/api/playlists/1' and route.request.method == 'PATCH':
            self.patches.append(route.request.post_data_json)
            if self.fail_rename:
                route.fulfill(status=500, content_type='application/json', body='{"error":"测试失败"}'); return
            self.playlist.update(route.request.post_data_json)
            data = {'ok': True}
        elif path == '/api/playlists/1': data = {'playlist': self.playlist, 'songs': [self.item]}
        elif path == '/api/library': data = {'likes': [], 'playlists': [self.playlist]}
        elif path == '/api/home': data = dict(daily=[self.item], recommendations=[self.item], fm=[], recent=[], liked=[], playlists=[self.playlist], picks=[])
        elif path == '/api/stream': data = {'url': '/fixture.wav', 'source': 'netease', 'id': '11', 'format': 'wav', 'bitrate': 128000}
        elif path == '/api/lyrics': data = {'synced': True, 'lines': [{'t': 0, 'text': '演示歌词'}]}
        elif path == '/api/download':
            self.downloads.append(route)
            if not self.hold_download: self.finish_download(route)
            return
        else: data = {}
        route.fulfill(content_type='application/json', body=json.dumps(data))

    def finish_download(self, route):
        route.fulfill(body=self.audio, headers={'Content-Type': 'audio/wav', 'X-Husic-Audio-Format': 'wav'})

    def open(self, path='/playlist/1'):
        self.page.goto(os.environ.get('MUSIC_TEST_URL', 'http://127.0.0.1:5190') + path)
        self.page.locator('.collection-header,.crate,.download-manager-page').first.wait_for()

    def menu(self):
        self.page.get_by_role('button', name='歌单操作', exact=True).click()
        return self.page.get_by_role('menu', name='歌单操作', exact=True)

    def test_rename_updates_title_navigation_and_persists_without_losing_metadata(self):
        self.open(); self.menu().get_by_role('menuitem', name='重命名', exact=True).click()
        dialog = self.page.get_by_role('dialog', name='重命名歌单')
        field = dialog.get_by_role('textbox', name='歌单名称')
        expect(field).to_have_value('午后慢慢听'); expect(field).to_be_focused()
        field.fill('  晚风里的歌  '); dialog.get_by_role('button', name='保存', exact=True).click()
        expect(dialog).not_to_be_visible()
        expect(self.page.locator('.collection-info h1')).to_have_text('晚风里的歌')
        expect(self.page.locator('.side-playlists a')).to_have_text('晚风里的歌')
        self.assertEqual(self.patches, [dict(name='晚风里的歌', description='保留原简介', cover='/test-cover.svg')])
        self.page.reload(); expect(self.page.locator('.collection-info h1')).to_have_text('晚风里的歌')
        expect(self.page.locator('.collection-sub')).to_have_text('1 首歌')

    def test_rename_validation_failure_and_cancel_preserve_original(self):
        self.open(); self.menu().get_by_role('menuitem', name='重命名', exact=True).click()
        dialog = self.page.get_by_role('dialog', name='重命名歌单'); field = dialog.get_by_role('textbox')
        field.fill('   '); expect(dialog.get_by_role('button', name='保存', exact=True)).to_be_disabled()
        self.fail_rename = True; field.fill('不能保存的新名字'); dialog.get_by_role('button', name='保存', exact=True).click()
        expect(dialog.get_by_role('alert')).to_be_visible()
        expect(self.page.locator('.collection-info h1')).to_have_text('午后慢慢听')
        self.page.keyboard.press('Escape'); expect(dialog).not_to_be_visible()
        expect(self.page.get_by_role('button', name='歌单操作')).to_be_focused()

    def test_menu_on_imported_copy_mobile_bounds_and_keyboard(self):
        self.playlist['origin'] = 'netease:123'
        for width in [320, 390, 820, 1440]:
            self.page.set_viewport_size({'width': width, 'height': 860}); self.open()
            menu = self.menu(); expect(menu.get_by_role('menuitem', name='删除歌单', exact=True)).to_be_visible()
            box = menu.bounding_box(); self.assertGreaterEqual(box['x'], 0); self.assertLessEqual(box['x']+box['width'], width)
            self.assertFalse(self.page.evaluate('document.documentElement.scrollWidth>innerWidth'))
            self.page.keyboard.press('Escape'); expect(menu).not_to_be_visible()
        button = self.page.get_by_role('button', name='歌单操作'); button.focus(); self.page.keyboard.press('ArrowDown')
        expect(menu.get_by_role('menuitem', name='重命名', exact=True)).to_be_focused()
        self.page.keyboard.press('ArrowDown'); expect(menu.get_by_role('menuitem', name='删除歌单', exact=True)).to_be_focused()

    def test_navigation_suppresses_native_drag_and_callout_without_breaking_clicks(self):
        self.open()
        for selector in ['.side-nav a', '.tab-bar a']:
            for link in self.page.locator(selector).all():
                self.assertFalse(link.evaluate('(e)=>e.draggable'))
                self.assertFalse(link.evaluate('(e)=>e.dispatchEvent(new DragEvent("dragstart",{bubbles:true,cancelable:true}))'))
                self.assertFalse(link.evaluate('(e)=>e.dispatchEvent(new MouseEvent("contextmenu",{bubbles:true,cancelable:true}))'))
        self.page.locator('.side-nav a[href="/"]').click(); expect(self.page.locator('.crate')).to_be_visible()
        self.page.locator('.side-playlists a').focus(); self.page.keyboard.press('Enter')
        expect(self.page.locator('.collection-info h1')).to_have_text('午后慢慢听')

    def test_mini_download_saves_audio_once_without_opening_player_or_interrupting_song(self):
        self.hold_download = True; self.open(); self.page.locator('.song-row').first.click()
        self.page.wait_for_function('navigator.mediaSession.playbackState==="playing"')
        button = self.page.locator('.mini-extra').get_by_role('button', name='下载', exact=True)
        button.click(); expect(self.page.locator('.mini-extra').get_by_role('button', name='正在下载', exact=True)).to_be_disabled()
        for _ in range(40):
            if self.downloads: break
            self.page.wait_for_timeout(25)
        self.assertEqual(len(self.downloads), 1)
        expect(self.page.locator('.now-playing.is-open')).to_have_count(0)
        expect(self.page.locator('.mini-transport').get_by_role('button', name='暂停', exact=True)).to_be_visible()
        self.finish_download(self.downloads[0])
        expect(self.page.locator('.mini-extra').get_by_role('button', name='已下载', exact=True)).to_be_visible()
        self.page.locator('.side-nav a[href="/library"]').click()
        self.page.get_by_role('button', name='下载管理', exact=False).click()
        expect(self.page.locator('[data-local-audio-id]')).to_have_count(1)
        self.assertEqual(len(self.downloads), 1)
        self.page.reload()
        expect(self.page.locator('.mini-extra').get_by_role('button', name='已下载', exact=True)).to_be_disabled()
        self.page.get_by_role('button', name='移除测试曲目的本地副本', exact=True).click()
        expect(self.page.locator('.mini-extra').get_by_role('button', name='下载', exact=True)).to_be_enabled()

    def test_mini_download_failure_is_retryable_and_local_audio_exports(self):
        self.hold_download = True; self.open(); self.page.locator('.song-row').first.click()
        self.page.locator('.mini-extra').get_by_role('button', name='下载', exact=True).click()
        for _ in range(40):
            if self.downloads: break
            self.page.wait_for_timeout(25)
        self.assertEqual(len(self.downloads), 1)
        self.downloads[0].fulfill(status=502, content_type='application/json', body='{"error":"测试下载失败"}')
        expect(self.page.get_by_role('status')).to_contain_text('测试下载失败')
        expect(self.page.locator('.mini-extra').get_by_role('button', name='下载', exact=True)).to_be_enabled()
        self.hold_download = False
        self.page.locator('.mini-extra').get_by_role('button', name='下载', exact=True).click()
        expect(self.page.locator('.mini-extra').get_by_role('button', name='已下载', exact=True)).to_be_visible()
        self.page.locator('.side-nav a[href="/library"]').click()
        self.page.get_by_role('button', name='下载管理', exact=False).click()
        self.page.locator('[data-local-audio-id]').click()
        with self.page.expect_download() as pending:
            self.page.locator('.mini-extra').get_by_role('button', name='另存为', exact=True).click()
        from pathlib import Path
        self.assertEqual(Path(pending.value.path()).read_bytes(), self.audio)


if __name__ == '__main__': unittest.main()
