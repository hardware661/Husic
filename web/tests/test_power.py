"""Measure actual background UI work while a real HTMLAudioElement keeps playing.

Visibility is overridden only in the isolated fixture to exercise the app's
standard hidden event without relying on headless Chromium's tab scheduling.
This is not an iPhone temperature or battery measurement.
"""
import json
import os
import unittest
from playwright.sync_api import expect
import test_mobile as mobile


class Power(unittest.TestCase):
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
        self.page.set_default_timeout(7000)
        self.context.add_init_script('''
          window.__pageHidden=false;
          Object.defineProperty(document,'hidden',{get:()=>window.__pageHidden});
          Object.defineProperty(document,'visibilityState',{get:()=>window.__pageHidden?'hidden':'visible'});
          window.__rafCalls=0;const nativeRAF=requestAnimationFrame.bind(window);
          window.requestAnimationFrame=callback=>nativeRAF(t=>{window.__rafCalls++;callback(t)});
        ''')
        self.phone()
        self.page.emulate_media(reduced_motion='no-preference')
        self.cdp=self.context.new_cdp_session(self.page)
        self.cdp.send('Performance.enable')

    def tearDown(self):
        mobile.Mobile.tearDown(self)

    def hidden(self, value):
        self.page.evaluate('value=>{window.__pageHidden=value;document.dispatchEvent(new Event("visibilitychange"))}',value)
        self.page.wait_for_timeout(350)

    def meter(self, seconds=2):
        before={m['name']:m['value'] for m in self.cdp.send('Performance.getMetrics')['metrics']}
        self.page.evaluate('''window.__rafCalls=0;window.__changes=0;
          window.__observer?.disconnect();window.__observer=new MutationObserver(records=>window.__changes+=records.length);
          window.__observer.observe(document.querySelector('.frame'),{subtree:true,childList:true,attributes:true,characterData:true});
          window.__audioBefore=window.__testAudios.find(a=>a.src)?.currentTime||0;
        ''')
        self.page.wait_for_timeout(seconds*1000)
        data=self.page.evaluate('''(()=>{window.__observer.disconnect();const audio=window.__testAudios.find(a=>a.src);return {
          rafCalls:window.__rafCalls,domChanges:window.__changes,audioPaused:audio?.paused,audioProgress:(audio?.currentTime||0)-window.__audioBefore,
          runningAnimations:document.getAnimations().filter(a=>a.playState==='running').length,fullPlayerNodes:document.querySelectorAll('.now-playing .np-art').length,
          decorativeHidden:document.querySelector('.np-backdrop')?getComputedStyle(document.querySelector('.np-backdrop')).visibility:null
        }})()''')
        after={m['name']:m['value'] for m in self.cdp.send('Performance.getMetrics')['metrics']}
        data['taskMilliseconds']=round((after['TaskDuration']-before['TaskDuration'])*1000,2)
        data['scriptMilliseconds']=round((after['ScriptDuration']-before['ScriptDuration'])*1000,2)
        print(json.dumps({'test':self._testMethodName,'meter':data}),flush=True)
        return data

    def test_closed_player_does_not_build_full_artwork_or_request_lyrics(self):
        requests=[]
        self.page.on('request',lambda r:requests.append(r.url) if '/api/lyrics' in r.url else None)
        self.play();self.page.wait_for_timeout(800)
        data=self.meter(1)
        self.assertEqual(data['fullPlayerNodes'],0,'closed full player should not load large artwork')
        self.assertEqual(requests,[],'closed player must not fetch visual-only lyrics')
        self.assertFalse(data['audioPaused'])

    def test_hidden_page_has_no_visual_work_and_audio_continues(self):
        self.play();self.page.locator('.mini-song').click();self.page.wait_for_timeout(1200)
        self.hidden(True);data=self.meter()
        self.assertGreater(data['audioProgress'],1.5)
        self.assertFalse(data['audioPaused'])
        self.assertEqual(data['fullPlayerNodes'],0)
        self.assertEqual(data['runningAnimations'],0)
        self.assertLessEqual(data['domChanges'],1,'hidden player must not rewrite progress/lyrics')
        self.hidden(False)
        expect(self.page.get_by_role('button',name='查看歌词',exact=True)).to_be_visible()
        expect(self.page.locator('.mini-play-mobile')).to_have_attribute('aria-label','暂停')

    def test_hidden_releases_frame_render_surface_and_restores_scroll(self):
        self.play();self.page.locator('.content').evaluate('e=>e.scrollTop=180')
        self.hidden(True)
        self.assertEqual(self.page.locator('.frame').evaluate('e=>getComputedStyle(e).display'),'none','screen-off must release the renderer surface, not only make pixels transparent')
        self.assertFalse(self.page.evaluate('window.__testAudios.find(a=>a.src).paused'))
        self.hidden(False)
        self.assertAlmostEqual(self.page.locator('.content').evaluate('e=>e.scrollTop'),180,delta=2)

    def test_local_playback_diagnostics_are_bounded_and_exclude_personal_content(self):
        self.play();self.hidden(True)
        self.page.evaluate('for(let i=0;i<80;i++)window.__testAudios.find(a=>a.src).dispatchEvent(new Event("stalled"))')
        self.hidden(False)
        self.page.locator('a[href="/settings"]:visible').first.click()
        self.page.evaluate('Object.defineProperty(navigator,"clipboard",{configurable:true,value:{writeText:async text=>{window.__diagnostics=text}}})')
        self.page.get_by_role('button',name='复制播放诊断',exact=True).click()
        report=self.page.evaluate('window.__diagnostics');data=json.loads(report)
        self.assertLessEqual(len(data['recent']),32)
        self.assertGreaterEqual(data['counts']['stalled'],80)
        self.assertGreaterEqual(data['counts']['suspend'],1)
        self.assertEqual(data['current']['format'],'wav')
        for value in ['fixture.wav','晚风','演示歌手','userAgent','device','http']:
            self.assertNotIn(value,report)

    def test_visible_blur_preserves_player_until_page_is_actually_hidden(self):
        self.play();self.page.locator('.mini-song').click();self.page.wait_for_timeout(700)
        self.page.evaluate('window.dispatchEvent(new Event("blur"))')
        data=self.meter()
        self.assertGreater(data['audioProgress'],1.5)
        self.assertFalse(data['audioPaused'])
        expect(self.page.locator('.frame')).to_be_visible()
        self.assertGreater(data['fullPlayerNodes'],0,'visible but unfocused player must keep its artwork')
        self.hidden(True)
        expect(self.page.locator('.frame')).not_to_be_visible()
        self.hidden(False)
        self.page.evaluate('window.dispatchEvent(new Event("focus"))')
        expect(self.page.get_by_role('button',name='查看歌词',exact=True)).to_be_visible()

    def test_hidden_audio_advances_and_sleep_deadline_still_pauses(self):
        import io,wave
        payload=io.BytesIO()
        with wave.open(payload,'wb') as wav:
            wav.setnchannels(1);wav.setsampwidth(2);wav.setframerate(8000);wav.writeframes(b'\0\0'*8000*2)
        self.audio=payload.getvalue()
        self.play();self.set_duration(seconds=7)
        self.page.evaluate('window.__sameAudio=window.__testAudios.find(a=>a.src);window.__titleBeforeHidden=navigator.mediaSession.metadata.title')
        self.hidden(True)
        # An actual short WAV ends naturally; no synthetic ended event or
        # unsupported Range seeking stands in for background auto-advance.
        self.page.wait_for_function('navigator.mediaSession.metadata?.title!==window.__titleBeforeHidden',polling=100,timeout=3500)
        state=self.page.evaluate('({same:window.__testAudios.find(a=>a.src)===window.__sameAudio,paused:window.__sameAudio.paused})')
        self.assertTrue(state['same']);self.assertFalse(state['paused'])
        self.page.wait_for_function('window.__sameAudio.paused',polling=100,timeout=8000)
        self.hidden(False)
        expect(self.page.locator('.mini-play-mobile')).to_have_attribute('aria-label','播放')

    def test_visible_word_fill_runs_only_while_playing(self):
        self.context.route('**/api/lyrics',lambda route:route.fulfill(content_type='application/json',body=json.dumps({'synced':True,'lines':[{'t':0,'text':'演示歌词','words':[{'t':0,'d':15000,'w':'演示歌词'}]}]})))
        self.play();self.page.locator('.mini-song').click();self.page.get_by_role('button',name='查看歌词',exact=True).click()
        word=self.page.locator('.lyric .word').first;word.wait_for()
        self.page.wait_for_timeout(150)
        before=word.evaluate('e=>parseFloat(e.style.getPropertyValue("--p"))')
        self.page.wait_for_timeout(150)
        after=word.evaluate('e=>parseFloat(e.style.getPropertyValue("--p"))')
        self.assertGreater(after,before)
        self.page.locator('.np-transport .big').click();self.page.wait_for_timeout(900)
        data=self.meter(1);self.assertTrue(data['audioPaused']);self.assertLessEqual(data['rafCalls'],2)

    def test_discovery_decorations_pause_hidden_and_resume_visible(self):
        self.play()
        self.context.route('**/api/discover',lambda route:route.fulfill(content_type='application/json',body=json.dumps({'hero':self.songs[0],'more':self.songs[1:6],'radar':self.songs[:9],'fresh':[],'picks':[],'random':[],'world':[]})))
        self.page.locator('.tab-bar a[href="/discover"]').click()
        self.page.locator('.radar').scroll_into_view_if_needed();self.page.wait_for_timeout(500)
        self.assertGreater(self.page.evaluate('document.getAnimations().filter(a=>a.playState==="running").length'),0)
        self.hidden(True);data=self.meter()
        self.assertFalse(data['audioPaused']);self.assertGreater(data['audioProgress'],1.5)
        self.assertEqual(data['runningAnimations'],0);self.assertEqual(data['domChanges'],0)
        self.hidden(False)
        expect(self.page.locator('.radar')).to_be_visible()
        self.assertGreater(self.page.evaluate('document.getAnimations().filter(a=>a.playState==="running").length'),0)

    def test_pagehide_during_artwork_transition_suspends_only_visuals(self):
        self.play();self.page.locator('.mini-song').click()
        self.page.evaluate('window.dispatchEvent(new PageTransitionEvent("pagehide"))')
        self.page.wait_for_timeout(300);data=self.meter(1)
        self.assertFalse(data['audioPaused']);self.assertGreater(data['audioProgress'],.7)
        self.assertEqual(data['fullPlayerNodes'],0);self.assertEqual(data['rafCalls'],0)
        self.page.evaluate('window.dispatchEvent(new PageTransitionEvent("pageshow"))')
        expect(self.page.get_by_role('button',name='查看歌词',exact=True)).to_be_visible()
        expect(self.page.locator('.mini-play-mobile')).to_have_attribute('aria-label','暂停')

    def test_paused_line_lyrics_have_no_continuous_frame_loop(self):
        self.play();self.page.locator('.mini-song').click();self.page.get_by_role('button',name='查看歌词',exact=True).click()
        self.page.locator('.np-transport .big').click()
        self.page.wait_for_timeout(1200)
        data=self.meter()
        self.assertTrue(data['audioPaused'])
        self.assertLessEqual(data['rafCalls'],2,'paused lyrics cannot run a 60fps callback loop')

if __name__=='__main__':unittest.main()
