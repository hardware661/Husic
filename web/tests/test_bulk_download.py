"""Batch downloading and the real desktop-window overlay, using isolated audio."""
import json
import os
from pathlib import Path
import unittest
from urllib.parse import urlsplit
from playwright.sync_api import expect
import test_controls as fixtures


class BulkDownload(unittest.TestCase):
    setUpClass = classmethod(fixtures.Controls.setUpClass.__func__)
    tearDownClass = classmethod(fixtures.Controls.tearDownClass.__func__)
    open = fixtures.Controls.open

    def setUp(self):
        fixtures.Controls.setUp(self)
        self.items = [dict(self.item, songId=11+i, title=f'批量曲目{i+1}',
                           copies=[dict(self.item['copies'][0], id=str(11+i))]) for i in range(3)]
        self.items.insert(2, dict(self.items[0]))
        self.playlist['count'] = len(self.items)
        self.order = []; self.held_downloads = []; self.failing_ids = set()

    def tearDown(self):
        for route, _ in self.held_downloads:
            try: route.abort()
            except Exception: pass  # Completed or cancelled requests are already handled.
        self.context.unroute_all(behavior='ignoreErrors')
        fixtures.Controls.tearDown(self)

    def route(self, route):
        path = urlsplit(route.request.url).path
        if path == '/api/playlists/1' and route.request.method == 'GET':
            route.fulfill(content_type='application/json', body=json.dumps({'playlist': self.playlist, 'songs': self.items})); return
        if path == '/api/download':
            item = route.request.post_data_json['item']; identity = str(item['songId'])
            self.order.append(identity)
            if identity in self.failing_ids:
                route.fulfill(status=502, content_type='application/json', body='{"error":"这首歌暂时不可下载"}')
            elif self.hold_download: self.held_downloads.append((route, identity))
            else: self.fulfill_song(route, identity)
            return
        return fixtures.Controls.route(self, route)

    def fulfill_song(self, route, identity):
        # Different valid PCM bytes keep each test song's content hash distinct.
        audio = self.audio[:-2] + int(identity).to_bytes(2, 'little')
        route.fulfill(body=audio, headers={'Content-Type': 'audio/wav', 'X-Husic-Audio-Format': 'wav'})

    def wait_requests(self, count):
        for _ in range(100):
            if len(self.order) >= count: break
            self.page.wait_for_timeout(25)
        self.assertEqual(len(self.order), count)

    def manager(self):
        self.page.locator('.side-nav a[href="/library"]').click()
        self.page.get_by_role('button', name='下载管理', exact=False).click()
        return self.page.get_by_role('region', name='歌单批量下载')

    def test_menu_below_real_desktop_controls_and_remains_clickable(self):
        self.open()
        desktop = Path(__file__).resolve().parents[2] / 'desktop'
        origin = os.environ.get('HUSIC_TEST_URL', 'http://127.0.0.1:5190')
        script = (desktop/'desktop-chrome.js').read_text().replace('__HUSIC_ORIGIN__', json.dumps(origin)).replace('__HUSIC_LOGIN_ORIGIN__', '""').replace('__HUSIC_DESKTOP_CSS__', json.dumps((desktop/'desktop-chrome.css').read_text()))
        self.page.evaluate('window.chrome??={};window.chrome.webview={postMessage(){},addEventListener(){},removeEventListener(){}}')
        self.page.add_script_tag(content=script)
        controls = self.page.locator('.husic-window-controls'); controls.wait_for()
        button = self.page.get_by_role('button', name='歌单操作', exact=True)
        window_box = controls.bounding_box(); menu_box = button.bounding_box()
        self.assertGreaterEqual(menu_box['y'], window_box['y']+window_box['height']+12)
        button.click(); expect(self.page.get_by_role('menu', name='歌单操作')).to_be_visible()

    def test_batch_is_sequential_skips_duplicates_and_survives_navigation(self):
        self.hold_download = True; self.open(); self.page.locator('.song-row').first.click()
        self.page.wait_for_function('navigator.mediaSession.playbackState==="playing"')
        self.page.get_by_role('button', name='全部下载', exact=True).click(); self.wait_requests(1)
        expect(self.page.locator('.bulk-download-button')).to_be_disabled()
        batch = self.manager(); expect(batch).to_be_visible()
        self.page.wait_for_timeout(80); self.assertEqual(self.order, ['11'])
        for index in range(3):
            self.wait_requests(index+1)
            route, identity = self.held_downloads[index]; self.fulfill_song(route, identity)
        expect(batch).to_contain_text('批量下载结束')
        expect(batch).to_contain_text('跳过 1')
        expect(self.page.locator('[data-local-audio-id]')).to_have_count(3)
        self.assertEqual(self.order, ['11', '12', '13'])
        expect(self.page.locator('.mini-song strong')).to_have_text('批量曲目1')
        expect(self.page.locator('.mini-transport').get_by_role('button', name='暂停', exact=True)).to_be_visible()
        self.page.reload(); expect(self.page.locator('[data-local-audio-id]')).to_have_count(3)
        self.open(); self.page.get_by_role('button', name='全部下载', exact=True).click()
        batch = self.manager(); expect(batch).to_contain_text('批量下载结束'); expect(batch).to_contain_text('跳过 4')
        self.assertEqual(self.order, ['11', '12', '13'])

    def test_failed_song_does_not_stop_the_rest(self):
        self.failing_ids = {'12'}; self.open()
        self.page.get_by_role('button', name='全部下载', exact=True).click()
        batch = self.manager(); expect(batch).to_contain_text('批量下载结束'); expect(batch).to_contain_text('失败 1')
        expect(self.page.locator('[data-local-audio-id]')).to_have_count(2)
        self.assertEqual(self.order, ['11', '12', '13'])
        expect(self.page.get_by_role('button', name='重新下载批量曲目2', exact=True)).to_be_visible()

    def test_cancel_stops_current_and_remaining_downloads(self):
        self.hold_download = True; self.open(); self.page.get_by_role('button', name='全部下载', exact=True).click()
        self.wait_requests(1); self.fulfill_song(*self.held_downloads[0]); self.wait_requests(2)
        batch = self.manager()
        batch.get_by_role('button', name='取消全部下载', exact=True).click()
        expect(batch).to_contain_text('已取消批量下载')
        self.page.wait_for_timeout(100); self.assertEqual(self.order, ['11', '12'])
        expect(self.page.locator('[data-local-audio-id]')).to_have_count(1)

    def test_storage_full_stops_batch_instead_of_downloading_the_rest(self):
        self.context.add_init_script('''
          const original=IDBObjectStore.prototype.add;
          IDBObjectStore.prototype.add=function(...args){
            if(this.name==='records')throw new DOMException('本机存储空间不足','QuotaExceededError');
            return original.apply(this,args);
          };
        ''')
        self.open(); self.page.get_by_role('button', name='全部下载', exact=True).click()
        batch = self.manager(); expect(batch).to_contain_text('存储失败，已停止')
        self.assertEqual(self.order, ['11'])
        expect(self.page.locator('[data-local-audio-id]')).to_have_count(0)

    def test_batch_action_fits_phone_and_empty_playlist_is_disabled(self):
        for width in [320,390,820,1280]:
            self.page.set_viewport_size({'width':width,'height':860}); self.open()
            button=self.page.get_by_role('button', name='全部下载', exact=True); expect(button).to_be_visible()
            self.assertFalse(self.page.evaluate('document.documentElement.scrollWidth>innerWidth'))
            self.assertTrue(button.evaluate('(e)=>e.scrollWidth<=e.clientWidth+1'))
        self.items=[]; self.open(); expect(self.page.get_by_role('button', name='全部下载', exact=True)).to_be_disabled()


if __name__ == '__main__': unittest.main()
