"""Same-source native playback control; does not assert phone temperature."""
import io
import json
import time
import unittest
import wave
from urllib.parse import urlsplit

from playwright.sync_api import expect
import test_mobile as mobile


class NativeAudio(unittest.TestCase):
    setUpClass = classmethod(mobile.Mobile.setUpClass.__func__)
    tearDownClass = classmethod(mobile.Mobile.tearDownClass.__func__)
    route = mobile.Mobile.route
    open = mobile.Mobile.open
    phone = mobile.Mobile.phone
    play = mobile.Mobile.play
    timer = mobile.Mobile.timer
    set_duration = mobile.Mobile.set_duration
    finish_download = mobile.fixtures.Controls.finish_download

    def setUp(self):
        mobile.setup_fixture(self)
        self.phone()

    def tearDown(self):
        mobile.Mobile.tearDown(self)

    def settings(self):
        if urlsplit(self.page.url).path != '/settings':
            self.page.locator('a[href="/settings"]:visible').first.click()
        return self.page.get_by_role('button', name='原生播放对照', exact=True)

    def wait_native(self, predicate, timeout=5):
        # Playwright's in-page wait_for_function polling uses eval on later
        # frames. Observe from the driver so the control's CSP stays enforced.
        deadline=time.monotonic()+timeout
        while time.monotonic()<deadline:
            if self.page.evaluate(predicate):
                return
            self.page.wait_for_timeout(50)
        self.fail('Native audio condition did not become true: '+predicate)

    def transfer(self):
        button = self.settings()
        self.page.evaluate('''() => {
          const audio=window.__testAudios.find(a=>a.src);
          window.addEventListener('pagehide',()=>{
            localStorage.setItem('test-old-paused',String(audio.paused));
            localStorage.setItem('test-old-released',String(!audio.getAttribute('src')));
          },{once:true});
          sessionStorage.setItem('test-source',audio.currentSrc);
          sessionStorage.setItem('test-position',String(audio.currentTime));
        }''')
        button.click()
        self.page.wait_for_url('**/native-audio.html')
        self.page.locator('audio').wait_for()
        self.wait_native('document.querySelector("audio").readyState>=1')

    def test_transfer_stops_husic_and_keeps_the_exact_audio_without_resolving_again(self):
        self.play()
        self.page.wait_for_function('window.__testAudios.find(a=>a.src).currentTime>.4')
        before = len(self.stream_requests)
        self.transfer()
        self.assertEqual(self.page.evaluate('localStorage.getItem("test-old-paused")'), 'true')
        self.assertEqual(self.page.evaluate('localStorage.getItem("test-old-released")'), 'true')
        state = self.page.evaluate('''() => {
          const audio=document.querySelector('audio');
          return {same:audio.currentSrc===sessionStorage.getItem('test-source'),paused:audio.paused,
            position:audio.currentTime,previous:Number(sessionStorage.getItem('test-position')),
            handoff:sessionStorage.getItem('husic-native-audio-handoff-v1')};
        }''')
        self.assertTrue(state['same'])
        self.assertTrue(state['paused'], 'native page must wait for an explicit play gesture')
        self.assertAlmostEqual(state['position'], state['previous'], delta=.5)
        self.assertIsNone(state['handoff'], 'signed audio address must be consumed, not retained in storage')
        self.assertEqual(len(self.stream_requests), before)
        self.assertEqual(urlsplit(self.page.url).query, '')
        self.assertEqual(urlsplit(self.page.url).fragment, '')
        self.assertNotIn('assets/index-', self.page.content())

    def test_native_playback_is_single_track_and_reports_no_private_content(self):
        payload = io.BytesIO()
        with wave.open(payload,'wb') as wav:
            wav.setnchannels(1); wav.setsampwidth(2); wav.setframerate(8000)
            wav.writeframes(b'\0\0' * 8000 * 3)
        self.audio = payload.getvalue()
        self.play()
        self.transfer()
        before = len(self.stream_requests)
        self.page.get_by_role('button', name='开始对照播放', exact=True).click()
        self.wait_native('document.querySelector("audio").currentTime>1')
        self.wait_native('document.querySelector("audio").ended')
        self.assertEqual(len(self.stream_requests), before, 'the native control must not advance the Husic queue')
        self.assertTrue(self.page.locator('audio').evaluate('a=>a.paused'))
        self.page.evaluate('Object.defineProperty(navigator,"clipboard",{configurable:true,value:{writeText:async text=>window.__report=text}})')
        self.page.get_by_role('button', name='复制对照诊断', exact=True).click()
        text = self.page.evaluate('window.__report')
        report = json.loads(text)
        self.assertEqual(report['mode'], 'native-control')
        self.assertEqual(report['current']['format'], 'wav')
        for private in ('fixture.wav', '晚风', '演示歌手', 'http', 'url', 'userAgent'):
            self.assertNotIn(private, text)

    def test_return_stops_native_audio_and_preserves_the_queue(self):
        self.play()
        self.transfer()
        self.page.get_by_role('button', name='开始对照播放', exact=True).click()
        self.wait_native('document.querySelector("audio").currentTime>.3')
        self.page.evaluate('''window.addEventListener('pagehide',()=>{
          const audio=document.querySelector('audio');
          localStorage.setItem('test-native-paused',String(audio.paused));
          localStorage.setItem('test-native-released',String(!audio.getAttribute('src')));
        },{once:true})''')
        self.page.get_by_role('link', name='返回 Husic', exact=True).click()
        self.page.wait_for_url('**/settings')
        self.assertEqual(self.page.evaluate('localStorage.getItem("test-native-paused")'), 'true')
        self.assertEqual(self.page.evaluate('localStorage.getItem("test-native-released")'), 'true')
        self.assertEqual(self.page.evaluate('JSON.parse(localStorage.getItem("player")).queue.length'), len(self.songs[:8]))
        expect(self.page.locator('.mini-play-mobile')).to_have_attribute('aria-label', '播放')

    def test_native_audio_keeps_playing_hidden_without_app_timers_or_media_session(self):
        self.play()
        self.transfer()
        self.page.get_by_role('button', name='开始对照播放', exact=True).click()
        self.page.evaluate('''() => {
          window.__before=document.querySelector('audio').currentTime;
          Object.defineProperty(document,'hidden',{configurable:true,get:()=>true});
          document.dispatchEvent(new Event('visibilitychange'));
          window.dispatchEvent(new Event('blur'));
          window.__callbacks=0;
          const frame=requestAnimationFrame.bind(window),interval=setInterval.bind(window);
          window.requestAnimationFrame=callback=>frame(t=>{window.__callbacks++;callback(t)});
          window.setInterval=(callback,delay,...args)=>interval(()=>{window.__callbacks++;callback(...args)},delay);
        }''')
        self.page.wait_for_timeout(1200)
        state=self.page.evaluate('''() => ({paused:document.querySelector('audio').paused,
          progress:document.querySelector('audio').currentTime-window.__before,callbacks:window.__callbacks,
          metadata:navigator.mediaSession?.metadata,app:!!document.querySelector('#root')})''')
        self.assertFalse(state['paused'])
        self.assertGreater(state['progress'], .8)
        self.assertEqual(state['callbacks'], 0)
        self.assertIsNone(state['metadata'])
        self.assertFalse(state['app'])

    def test_unavailable_session_storage_does_not_interrupt_the_song(self):
        self.play()
        button=self.settings()
        self.page.evaluate('''() => {
          const set=Storage.prototype.setItem;
          Storage.prototype.setItem=function(key,value){
            if(key==='husic-native-audio-handoff-v1')throw new DOMException('Storage unavailable','QuotaExceededError');
            return set.call(this,key,value);
          };
        }''')
        button.click()
        self.assertEqual(urlsplit(self.page.url).path, '/settings')
        self.assertFalse(self.page.evaluate('window.__testAudios.find(a=>a.src).paused'))

    def test_injected_third_party_script_is_blocked_but_cross_origin_audio_plays(self):
        # The public edge can inject analytics which local Vite never serves.
        # Exercise actual script execution, not just the presence of a CSP tag.
        def inject(route):
            response=route.fetch()
            html=response.text().replace('</body>', '<script defer src="https://static.cloudflareinsights.com/beacon.min.js"></script></body>')
            route.fulfill(response=response,body=html)
        self.context.route('**/native-audio.html',inject)
        self.context.route('https://static.cloudflareinsights.com/**',lambda r:r.fulfill(content_type='text/javascript',body='window.__unwantedAnalyticsRan=true'))
        self.context.route('**/api/stream',lambda r:r.fulfill(content_type='application/json',body=json.dumps({
            'url':'https://media.example.test/fixture.wav','source':'netease','id':'11','format':'wav','bitrate':128000})))
        self.play()
        self.transfer()
        self.page.wait_for_load_state('networkidle')
        self.assertFalse(self.page.evaluate('window.__unwantedAnalyticsRan===true'), 'native control must not execute edge-injected analytics')
        self.assertTrue(self.page.locator('audio').evaluate('a=>a.currentSrc.startsWith("https://media.example.test/")'))
        self.page.get_by_role('button',name='开始对照播放',exact=True).click()
        self.wait_native('document.querySelector("audio").currentTime>.4')

    def test_no_source_pending_load_and_active_sleep_timer_cannot_transfer(self):
        self.open('/')
        button = self.settings()
        expect(button).to_be_disabled()
        self.hold_stream = True
        self.play()
        expect(self.settings()).to_be_disabled()
        self.hold_stream = False
        for route in self.stream_requests:
            mobile.fixtures.Controls.route(self, route)
        self.page.wait_for_function('window.__testAudios.some(a=>a.src&&!a.paused)')
        expect(self.page.get_by_role('button', name='原生播放对照', exact=True)).to_be_enabled()
        self.set_duration(minutes=5, seconds=0)
        expect(self.page.get_by_role('button', name='原生播放对照', exact=True)).to_be_disabled()

    def test_invalid_or_expired_handoff_is_discarded_without_loading_audio(self):
        self.open('/')
        base = self.page.url.rstrip('/')
        valid_expiry = self.page.evaluate('Date.now()+60_000')
        for source, expires in [('javascript:alert(1)', valid_expiry), ('https://invalid.example/audio.mp3', 1)]:
            self.page.evaluate('(value)=>sessionStorage.setItem("husic-native-audio-handoff-v1",JSON.stringify(value))',
                               {'url': source, 'expires': expires, 'position': 0, 'volume': 1, 'muted': False})
            self.page.goto(base + '/native-audio.html')
            expect(self.page.get_by_role('status')).to_contain_text('请返回 Husic')
            self.assertFalse(self.page.locator('audio').evaluate('a=>Boolean(a.getAttribute("src"))'))
            self.assertIsNone(self.page.evaluate('sessionStorage.getItem("husic-native-audio-handoff-v1")'))


if __name__ == '__main__':
    unittest.main()
