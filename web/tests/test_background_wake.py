"""Native audio callback audit; listener counts are not phone temperature."""
import json,unittest
from playwright.sync_api import expect
import test_power as power

class BackgroundWake(unittest.TestCase):
    setUpClass=classmethod(power.Power.setUpClass.__func__)
    tearDownClass=classmethod(power.Power.tearDownClass.__func__)
    tearDown=power.Power.tearDown
    open=power.Power.open
    phone=power.Power.phone
    play=power.Power.play
    hidden=power.Power.hidden
    meter=power.Power.meter
    timer=power.Power.timer
    set_duration=power.Power.set_duration
    finish_download=power.Power.finish_download
    def setUp(self):
        self.reports=[];power.Power.setUp(self)
        self.context.add_init_script('''window.__hiddenCallbacks=0;window.__hiddenEvents=0;window.__actions={};const seen=new WeakSet(),maps=new WeakMap();
          const add=HTMLMediaElement.prototype.addEventListener,remove=HTMLMediaElement.prototype.removeEventListener;
          HTMLMediaElement.prototype.addEventListener=function(type,listener,options){
            if(type!=='timeupdate'||typeof listener!=='function')return add.call(this,type,listener,options);
            let map=maps.get(this);if(!map)maps.set(this,map=new Map());let wrapped=map.get(listener);
            if(!wrapped){wrapped=function(e){if(document.hidden){window.__hiddenCallbacks++;if(!seen.has(e)){seen.add(e);window.__hiddenEvents++}}return listener.call(this,e)};map.set(listener,wrapped)}
            return add.call(this,type,wrapped,options);
          };
          HTMLMediaElement.prototype.removeEventListener=function(type,listener,options){
            const map=maps.get(this),wrapped=type==='timeupdate'?map?.get(listener):null;
            if(wrapped){map.delete(listener);return remove.call(this,type,wrapped,options)}return remove.call(this,type,listener,options);
          };
          window.__registeredTimeUpdates=()=>maps.get(window.__testAudios.find(a=>a.src))?.size||0;
          const nativeAction=navigator.mediaSession.setActionHandler.bind(navigator.mediaSession);navigator.mediaSession.setActionHandler=(name,handler)=>{window.__actions[name]=handler;return nativeAction(name,handler)};
        ''')
    def route(self,r):
        if r.request.url.split('?')[0].endswith('/api/plays'):
            self.reports.append(r.request.post_data_json);return r.fulfill(content_type='application/json',body='{"ok":true}')
        return power.Power.route(self,r)
    def test_ordinary_hidden_playback_has_no_progress_callbacks(self):
        self.play();self.hidden(True);self.page.evaluate('window.__hiddenCallbacks=window.__hiddenEvents=0')
        data=self.meter(3);data.update(self.page.evaluate('({callbackCalls:window.__hiddenCallbacks,nativeEvents:window.__hiddenEvents,handlers:window.__registeredTimeUpdates()})'))
        print(json.dumps(data),flush=True)
        self.assertGreater(data['audioProgress'],2.5);self.assertFalse(data['audioPaused'])
        self.assertEqual(data['callbackCalls'],0);self.assertEqual(data['handlers'],0)
    def test_hidden_listen_is_counted_at_native_next_boundary(self):
        self.play();self.hidden(True);self.page.wait_for_timeout(20500)
        # Invoke the exact registered lock-screen Next action, not a fake ended.
        self.page.evaluate('window.__actions.nexttrack()');self.page.wait_for_timeout(200)
        self.assertEqual(len(self.reports),1)
        self.assertGreaterEqual(self.reports[0]['seconds'],20);self.assertLessEqual(self.reports[0]['seconds'],24)
    def test_blind_reveal_still_works_hidden_then_progress_callbacks_stop(self):
        self.open('/discover');self.page.locator('.blind-play').click();self.page.wait_for_function('navigator.mediaSession.playbackState==="playing"')
        self.hidden(True);self.page.wait_for_timeout(8500)
        self.assertNotEqual(self.page.evaluate('navigator.mediaSession.metadata.title'),'先听，再看是谁')
        self.assertEqual(self.page.evaluate('window.__registeredTimeUpdates()'),0)
    def test_active_sleep_timer_keeps_its_accuracy_and_detaches_at_expiry(self):
        self.play();self.set_duration(seconds=3);self.hidden(True)
        self.assertGreater(self.page.evaluate('window.__registeredTimeUpdates()'),0)
        self.page.wait_for_timeout(3100)
        self.assertTrue(self.page.evaluate('window.__testAudios.find(a=>a.src).paused'))
        self.assertEqual(self.page.evaluate('window.__registeredTimeUpdates()'),0)
if __name__=='__main__':unittest.main()
