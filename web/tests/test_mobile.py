"""Real browser coverage for equal blind-listening cells, mobile player and sleep timer.

Run with an isolated frontend: MUSIC_TEST_URL=http://127.0.0.1:5194 python3 web/tests/test_mobile.py.
Existing playlist/control checks are inherited as the player is shared with them.
"""
import json
import os
from urllib.parse import urlsplit
from playwright.sync_api import expect
import test_controls as fixtures


def setup_fixture(self):
    fixtures.Controls.setUp(self)
    self.songs = [{**self.item, 'songId': n, 'title': f'晚风里的第 {n} 首歌', 'cover': '/test-cover.svg',
                   'copies': [{**self.item['copies'][0], 'id': str(n)}]} for n in range(11, 29)]
    self.context.add_init_script("const OriginalAudio=window.Audio;window.__testAudios=[];window.Audio=function(...args){const audio=new OriginalAudio(...args);window.__testAudios.push(audio);return audio};window.Audio.prototype=OriginalAudio.prototype;")
    self.hold_stream = False
    self.stream_requests = []


class Mobile(fixtures.Controls):
    def setUp(self):
        setup_fixture(self)

    def route(self, route):
        path = urlsplit(route.request.url).path
        if path == '/api/home':
            data = dict(daily=self.songs[:8], recommendations=self.songs, fm=self.songs[2:6], recent=self.songs[4:7],
                        liked=self.songs[:8], playlists=[self.playlist], picks=[])
        elif path == '/api/discover':
            data = dict(hero=self.songs[0], more=self.songs[1:6], radar=[], fresh=[], picks=[], random=[], world=[])
        elif path == '/api/accounts':
            data = {k: {'state': 'not_connected'} for k in ['netease', 'bilibili', 'youtube', 'qq', 'kugou', 'migu']}
        elif path == '/api/stream':
            self.stream_requests.append(route)
            if self.hold_stream: return
            return fixtures.Controls.route(self, route)
        else: return fixtures.Controls.route(self, route)
        route.fulfill(content_type='application/json', body=json.dumps(data))

    def open(self,path='/playlist/1'):
        self.page.goto(os.environ.get('MUSIC_TEST_URL','http://127.0.0.1:5194')+path)
        self.page.locator({'/':'.crate','/discover':'.discover-opening'}.get(path,'.collection-header,.download-manager-page')).first.wait_for(timeout=8000)

    def phone(self, width=390):
        self.page.set_viewport_size({'width': width, 'height': 844})

    def timer(self):
        if urlsplit(self.page.url).path != '/settings':
            self.page.locator('a[href="/settings"]:visible').first.click()
        self.page.get_by_role('button', name='定时停止播放', exact=True).click()
        return self.page.get_by_role('dialog', name='定时停止播放', exact=True)

    def set_duration(self, hours=0, minutes=0, seconds=2):
        d = self.timer()
        for label, value in [('小时', hours), ('分钟', minutes), ('秒', seconds)]:
            field = d.get_by_role('spinbutton', name=label, exact=True)
            field.focus(); field.press('Home')
            for _ in range(value): field.press('ArrowDown')
            expect(field).to_have_attribute('aria-valuenow', str(value))
        d.get_by_role('button', name='开始计时', exact=True).click()
        expect(d).not_to_be_visible()

    def play(self):
        self.open('/')
        self.page.locator('.crate-entry-daily,.crate-row .sleeve').first.click()
        self.page.locator('.mini-play-mobile[aria-label="暂停"]').wait_for()

    def test_blind_four_equal_centres_on_all_surfaces(self):
        self.open('/discover')
        for width in [320, 390, 430, 760, 820, 1280, 1560]:
            self.page.set_viewport_size({'width': width, 'height': 900})
            self.page.wait_for_timeout(100)
            result = self.page.locator('.discover-opening').evaluate('''root=>{
              const r=root.getBoundingClientRect(),s=getComputedStyle(root),left=parseFloat(s.paddingLeft),right=parseFloat(s.paddingRight),w=r.width-left-right;
              return {cells:['.blind-lead','.blind-art','.blind-info','.discover-refresh'].map((q,i)=>{const b=root.querySelector(q).getBoundingClientRect();return {actual:b.x+b.width/2,expected:r.x+left+w*(i+.5)/4,y:b.y+b.height/2}}),overflow:document.documentElement.scrollWidth>innerWidth};
            }''')
            for cell in result['cells']: self.assertAlmostEqual(cell['actual'], cell['expected'], delta=2, msg=f'{width}: {result}')
            self.assertLess(max(c['y'] for c in result['cells'])-min(c['y'] for c in result['cells']), 3)
            self.assertFalse(result['overflow'])
        self.phone(); self.page.get_by_role('button', name='揭晓', exact=True).click()
        expect(self.page.locator('.blind-info')).to_contain_text(self.songs[0]['title'])
        self.page.get_by_role('button', name='换一首', exact=False).click()
        expect(self.page.locator('.blind-mask')).to_be_visible()

    def test_mobile_home_has_four_visible_entries_without_horizontal_crate(self):
        for width in [320, 390, 430, 760]:
            self.phone(width); self.open('/')
            expect(self.page.locator('.crate-entry-daily')).to_be_visible()
            expect(self.page.locator('.crate-entry-fm')).to_be_visible()
            expect(self.page.locator('.crate-entry-liked')).to_be_visible()
            expect(self.page.locator('.crate-entry-recent')).to_be_visible()
            metrics=self.page.locator('.crate-row').evaluate('e=>({width:e.clientWidth,scroll:e.scrollWidth,children:[...e.children].filter(c=>c.getClientRects().length).length})')
            self.assertEqual(metrics['children'],4); self.assertLessEqual(metrics['scroll'],metrics['width']+1)

    def test_mobile_cover_tap_enters_lyrics(self):
        self.phone(); self.play()
        self.page.locator('.mini-song').click()
        art=self.page.get_by_role('button',name='查看歌词',exact=True)
        expect(art).to_be_visible(); art.click()
        expect(self.page.locator('.np-body')).to_have_class('np-body pane-lyrics')
        expect(self.page.locator('.now-playing .lyric')).to_contain_text('演示歌词')
        self.page.get_by_role('button',name='歌词',exact=True).click()
        expect(art).to_be_visible()

    def test_sleep_timer_pauses_from_another_page_and_does_not_skip_after_expiry(self):
        self.phone(); self.play(); self.set_duration()
        self.page.goto(os.environ.get('MUSIC_TEST_URL','http://127.0.0.1:5194') + '/')
        # The absolute deadline survives navigation/reload and can no longer auto-start audio.
        self.page.wait_for_timeout(2200)
        self.page.locator('.crate-row .sleeve').first.click()
        expect(self.page.locator('.mini-play-mobile')).to_have_attribute('aria-label','暂停')
        self.set_duration(seconds=2)
        self.page.locator('.tab-bar a[href="/"]').click()
        self.page.wait_for_timeout(2400)
        expect(self.page.locator('.mini-play-mobile')).to_have_attribute('aria-label','播放')
        before=len(self.stream_requests)
        self.page.evaluate('window.__testAudios.find(a=>a.src)?.dispatchEvent(new Event("ended"))')
        self.page.wait_for_timeout(200)
        self.assertEqual(len(self.stream_requests),before)

    def test_sleep_timer_cancel_zero_wheel_and_wall_clock_resume(self):
        self.phone(); self.play(); d=self.timer()
        for label in ['小时','分钟','秒']: d.get_by_role('spinbutton',name=label,exact=True).press('Home')
        expect(d.get_by_role('button',name='开始计时',exact=True)).to_be_disabled()
        seconds=d.get_by_role('spinbutton',name='秒',exact=True); seconds.press('ArrowDown')
        expect(seconds).to_have_attribute('aria-valuenow','1')
        d.get_by_role('button',name='开始计时',exact=True).click()
        d=self.timer(); d.get_by_role('button',name='取消定时',exact=True).click()
        self.page.wait_for_timeout(1200); expect(self.page.locator('.mini-play-mobile')).to_have_attribute('aria-label','暂停')
        self.set_duration(minutes=30,seconds=0)
        # iOS can freeze JS while hidden: advance wall time without running timer callbacks,
        # then deliver a media event. Pausing must be based on the deadline, not tick counts.
        self.page.evaluate('''window.__realNow=Date.now;Date.now=()=>window.__realNow()+31*60000;window.dispatchEvent(new Event('pageshow'));''')
        expect(self.page.locator('.mini-play-mobile')).to_have_attribute('aria-label','播放')
        self.page.evaluate('Date.now=window.__realNow')

    def test_timer_expires_during_pending_audio_and_wheel_scroll_is_real(self):
        self.phone(); self.open('/')
        d=self.timer()
        minutes=d.get_by_role('spinbutton',name='分钟',exact=True)
        minutes.press('Home');minutes.locator('.timer-wheel-scroll').hover()
        self.page.mouse.wheel(0,88)
        expect(minutes).to_have_attribute('aria-valuenow','2')
        d.get_by_role('button',name='关闭定时设置',exact=True).click()
        self.set_duration(seconds=2)
        self.hold_stream=True
        self.page.locator('.tab-bar a[href="/"]').click()
        self.page.locator('.crate-entry-daily').click()
        expect(self.page.locator('.mini-play-mobile')).to_have_attribute('aria-label','暂停')
        self.page.wait_for_timeout(2300)
        expect(self.page.locator('.mini-play-mobile')).to_have_attribute('aria-label','播放')
        self.assertGreater(len(self.stream_requests),0)
        for request in self.stream_requests:
            request.fulfill(content_type='application/json',body=json.dumps({'url':'/fixture.wav','source':'netease','id':'11','format':'wav','bitrate':128000}))
        self.page.wait_for_timeout(300)
        expect(self.page.locator('.mini-play-mobile')).to_have_attribute('aria-label','播放')

    def test_timer_remains_clickable_under_real_desktop_window_overlay(self):
        from pathlib import Path
        self.open('/')
        desktop=Path(__file__).resolve().parents[2]/'desktop'
        origin=os.environ.get('MUSIC_TEST_URL','http://127.0.0.1:5194')
        script=(desktop/'desktop-chrome.js').read_text().replace('__MUSIC_ORIGIN__',json.dumps(origin)).replace('__MUSIC_LOGIN_ORIGIN__','""').replace('__MUSIC_DESKTOP_CSS__',json.dumps((desktop/'desktop-chrome.css').read_text()))
        self.page.evaluate('window.chrome??={};window.chrome.webview={postMessage(){},addEventListener(){},removeEventListener(){}}')
        self.page.add_script_tag(content=script)
        dialog=self.timer()
        dialog.get_by_role('spinbutton',name='分钟',exact=True).press('Home')
        dialog.get_by_role('spinbutton',name='分钟',exact=True).press('ArrowDown')
        expect(dialog.get_by_role('spinbutton',name='分钟',exact=True)).to_have_attribute('aria-valuenow','1')
        dialog.get_by_role('button',name='开始计时',exact=True).click()
        expect(dialog).not_to_be_visible()
        expect(self.page.locator('.sleep-timer-setting')).to_contain_text('00:01:00')

    def test_dismiss_gesture_never_fades_a_translated_grey_fullscreen_layer(self):
        self.context.close()
        self.context=self.browser.new_context(viewport={'width':390,'height':844},is_mobile=True,has_touch=True)
        self.context.add_init_script('localStorage.setItem("player",JSON.stringify({volume:0}))')
        self.context.route('**/api/**',self.route)
        self.context.route('**/fixture.wav',lambda r:r.fulfill(body=self.audio,content_type='audio/wav'))
        self.context.route('**/test-cover.svg',lambda r:r.fulfill(content_type='image/svg+xml',body='<svg xmlns="http://www.w3.org/2000/svg" width="100" height="100"><rect width="100" height="100" fill="#c58b67"/></svg>'))
        self.page=self.context.new_page();self.page.set_default_timeout(3500)
        self.page.on('pageerror',lambda error:self.errors.append(str(error)))
        self.play();self.page.locator('.mini-song').click();self.page.wait_for_timeout(650)
        self.page.evaluate('''window.__frames=[];window.__record=true;function capture(){const e=document.querySelector('.now-playing'),r=e?e.getBoundingClientRect():{y:innerHeight},s=e?getComputedStyle(e):{opacity:'0',visibility:'hidden'};window.__frames.push({open:!!e?.classList.contains('is-open'),y:r.y,opacity:Number(s.opacity),visible:s.visibility});if(window.__record)requestAnimationFrame(capture)}capture()''')
        cdp=self.context.new_cdp_session(self.page)
        cdp.send('Input.dispatchTouchEvent',{'type':'touchStart','touchPoints':[{'x':190,'y':210}]})
        for y in [235,270,315,375,440]:
            cdp.send('Input.dispatchTouchEvent',{'type':'touchMove','touchPoints':[{'x':190,'y':y}]});self.page.wait_for_timeout(18)
        cdp.send('Input.dispatchTouchEvent',{'type':'touchEnd','touchPoints':[]})
        self.page.wait_for_timeout(650)
        self.page.evaluate('window.__record=false')
        expect(self.page.locator('.now-playing.is-open')).to_have_count(0)
        frames=self.page.evaluate('window.__frames')
        dragging=[f for f in frames if f['open'] and f['y']>12]
        self.assertTrue(dragging,'real touch gesture did not move sheet')
        self.assertTrue(all(f['opacity']>.98 for f in dragging),'dragged sheet must remain opaque; Safari blur compositing flashes otherwise')
        closing=[f for f in frames if not f['open'] and f['visible']=='visible' and f['opacity']>.05]
        self.assertTrue(all(f['y']>12 for f in closing),'closed sheet flashed back across the full screen')
        # Reopening a completed dismiss must restore artwork and all controls.
        self.page.locator('.mini-song').click()
        expect(self.page.get_by_role('button',name='查看歌词',exact=True)).to_be_visible()
        self.page.wait_for_timeout(450)
        # A short pull springs back and must not be mistaken for a cover tap.
        cdp.send('Input.dispatchTouchEvent',{'type':'touchStart','touchPoints':[{'x':190,'y':210}]})
        cdp.send('Input.dispatchTouchEvent',{'type':'touchMove','touchPoints':[{'x':190,'y':240}]})
        self.page.wait_for_timeout(120)
        cdp.send('Input.dispatchTouchEvent',{'type':'touchEnd','touchPoints':[]})
        self.page.wait_for_timeout(380)
        expect(self.page.locator('.np-body')).to_have_class('np-body pane-cover')
        expect(self.page.locator('.now-playing')).to_have_class('now-playing is-open')
        # Interrupt a committed dismissal by immediately reopening the mini player.
        cdp.send('Input.dispatchTouchEvent',{'type':'touchStart','touchPoints':[{'x':190,'y':210}]})
        for y in [250,310,390]:
            cdp.send('Input.dispatchTouchEvent',{'type':'touchMove','touchPoints':[{'x':190,'y':y}]});self.page.wait_for_timeout(18)
        cdp.send('Input.dispatchTouchEvent',{'type':'touchEnd','touchPoints':[]})
        self.page.locator('.mini-song').click()
        self.page.wait_for_timeout(500)
        expect(self.page.locator('.now-playing')).to_have_class('now-playing is-open')
        expect(self.page.get_by_role('button',name='查看歌词',exact=True)).to_be_visible()
        self.assertEqual(self.page.locator('.now-playing').get_attribute('data-dismissed'),None)


if __name__ == '__main__':
    import unittest
    unittest.main()
