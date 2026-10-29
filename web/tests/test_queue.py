"""Play-queue regressions: shuffle with "play next", removing songs, the
user's repeat choice, and switching between two remote playlists.

Run with a frontend: HUSIC_TEST_URL=http://127.0.0.1:5190 python3 web/tests/test_queue.py
"""
import json
import unittest
from urllib.parse import urlsplit
from playwright.sync_api import expect
import test_mobile as mobile


class Queue(unittest.TestCase):
    setUpClass = classmethod(mobile.Mobile.setUpClass.__func__)
    tearDownClass = classmethod(mobile.Mobile.tearDownClass.__func__)
    open = mobile.Mobile.open
    tearDown = mobile.fixtures.Controls.tearDown
    finish_download = mobile.fixtures.Controls.finish_download

    def setUp(self):
        mobile.setup_fixture(self)
        self.held = {}

    def route(self, route):
        path = urlsplit(route.request.url).path
        if path.startswith('/api/netease/playlists/'):
            pid = path.rsplit('/', 1)[1]
            songs = [{**s, 'title': f'歌单{pid}·{s["title"]}'} for s in self.songs[:8]]
            body = json.dumps({'playlist': {'id': pid, 'name': f'远程歌单 {pid}', 'cover': ''}, 'songs': songs})
            if pid == 'b':
                self.held['b'] = lambda: route.fulfill(content_type='application/json', body=body)
                return
            route.fulfill(content_type='application/json', body=body); return
        return mobile.Mobile.route(self, route)

    def upcoming(self):
        return self.page.locator('.queue-upcoming .queue-song strong').all_inner_texts()

    def open_queue(self):
        self.page.get_by_role('button', name='打开正在播放', exact=True).first.click()
        self.page.locator('.now-playing').get_by_role('button', name='待播', exact=True).click()

    def start_shuffled(self):
        self.page.set_viewport_size({'width': 1280, 'height': 860})
        self.open('/remote/netease/a')
        self.page.get_by_role('button', name='播放', exact=True).first.click()
        self.page.locator('.mini-transport [aria-label="暂停"]').wait_for()
        self.open_queue()
        self.page.locator('.now-playing').get_by_role('button', name='随机播放', exact=True).click()

    def test_play_next_in_shuffle_comes_next_and_keeps_the_order(self):
        self.start_shuffled()
        before = self.upcoming()
        self.page.get_by_role('button', name='收起', exact=True).click()
        self.page.locator('.side-playlists a').first.click()
        expect(self.page.locator('.collection-info h1')).to_have_text('午后慢慢听')
        self.page.locator('.song-row').first.get_by_role('button', name='更多', exact=True).click()
        self.page.get_by_role('menuitem', name='接下来播放').click()
        self.open_queue()
        expect(self.page.locator('.queue-upcoming .queue-song strong').first).to_have_text('测试曲目')
        self.assertEqual(self.upcoming()[1:], before, 'the rest of the shuffle order must stay as it was')

    def test_removing_a_song_keeps_the_shuffle_order(self):
        self.start_shuffled()
        before = self.upcoming()
        self.page.get_by_role('button', name=f'从待播移除 {before[2]}', exact=True).click()
        expect(self.page.locator('.queue-upcoming .queue-song strong')).to_have_count(len(before) - 1)
        self.assertEqual(self.upcoming(), before[:2] + before[3:])

    def test_playing_a_list_keeps_the_chosen_repeat_mode(self):
        self.page.set_viewport_size({'width': 1280, 'height': 860})
        self.open()
        self.page.locator('.song-row').first.click()
        self.page.locator('.mini-transport [aria-label="暂停"]').wait_for()
        self.page.get_by_role('button', name='打开正在播放', exact=True).first.click()
        repeat = self.page.locator('.now-playing .mode[aria-label$="循环"]')
        expect(repeat).to_have_attribute('aria-label', '列表循环')
        repeat.click(); expect(repeat).to_have_attribute('aria-label', '单曲循环')
        self.page.get_by_role('button', name='收起', exact=True).click()
        self.page.locator('.song-row').first.click()
        self.page.wait_for_timeout(300)
        expect(repeat).to_have_attribute('aria-label', '单曲循环')

    def test_switching_remote_playlists_never_shows_the_previous_one(self):
        self.open('/remote/netease/a')
        expect(self.page.locator('.collection-info h1')).to_have_text('远程歌单 a')
        self.page.evaluate('history.pushState(null,"","/remote/netease/b");dispatchEvent(new PopStateEvent("popstate"))')
        self.page.wait_for_timeout(200)
        expect(self.page.get_by_text('远程歌单 a')).to_have_count(0)
        expect(self.page.locator('.song-row')).to_have_count(0)
        self.held['b']()
        expect(self.page.locator('.collection-info h1')).to_have_text('远程歌单 b')


if __name__ == '__main__': unittest.main()
