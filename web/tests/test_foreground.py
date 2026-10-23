"""Touch navigation and foreground-work regressions; not a phone thermal test."""
import json,os,unittest
from urllib.parse import urlsplit
from playwright.sync_api import expect
import test_mobile as mobile

class Foreground(unittest.TestCase):
    setUpClass=classmethod(mobile.Mobile.setUpClass.__func__)
    tearDownClass=classmethod(mobile.Mobile.tearDownClass.__func__)
    play=mobile.Mobile.play
    open=mobile.Mobile.open
    finish_download=mobile.fixtures.Controls.finish_download
    def setUp(self):
        mobile.setup_fixture(self)
        self.context.close()
        self.context=self.browser.new_context(viewport={'width':390,'height':844},is_mobile=True,has_touch=True,reduced_motion='no-preference')
        self.context.add_init_script('''localStorage.setItem('player',JSON.stringify({volume:0}));const OriginalAudio=window.Audio;window.__testAudios=[];window.Audio=function(...a){const audio=new OriginalAudio(...a);window.__testAudios.push(audio);return audio};window.Audio.prototype=OriginalAudio.prototype;
        window.__scrollWrites=0;const set=Storage.prototype.setItem;Storage.prototype.setItem=function(k,v){if(k.startsWith('music-scroll:'))window.__scrollWrites++;return set.call(this,k,v)};''')
        self.context.route('**/api/**',self.route)
        self.context.route('**/fixture.wav',lambda r:r.fulfill(body=self.audio,content_type='audio/wav'))
        self.context.route('**/test-cover.svg',lambda r:r.fulfill(content_type='image/svg+xml',body='<svg xmlns="http://www.w3.org/2000/svg" width="100" height="100"><rect width="100" height="100" fill="#c58b67"/></svg>'))
        self.page=self.context.new_page();self.page.set_default_timeout(4500)
        self.page.on('pageerror',lambda e:self.errors.append(str(e)))
        self.chart_requests=[];self.home_requests=0
    def tearDown(self): mobile.Mobile.tearDown(self)
    def route(self,r):
        path=urlsplit(r.request.url).path
        if path=='/api/charts':data={'charts':[dict(source=s,id=str(i),short='热歌' if i==0 else '新歌',name=s+str(i)) for s in ['netease','qq','kuwo','kugou'] for i in range(4)]}
        elif path.startswith('/api/charts/'):
            self.chart_requests.append(path);_,_,_,source,id=path.split('/');data=dict(chart=dict(source=source,id=id,short='热歌' if id=='0' else '新歌',name='热歌'),updated='今天',songs=self.songs)
        else:
            if path=='/api/home':self.home_requests+=1
            return mobile.Mobile.route(self,r)
        r.fulfill(content_type='application/json',body=json.dumps(data))
    def swipe(self,x,y,dx,dy=0):
        c=self.context.new_cdp_session(self.page)
        c.send('Input.dispatchTouchEvent',dict(type='touchStart',touchPoints=[dict(x=x,y=y)]))
        for n in range(1,7):
            c.send('Input.dispatchTouchEvent',dict(type='touchMove',touchPoints=[dict(x=x+dx*n/6,y=y+dy*n/6)]));self.page.wait_for_timeout(22)
        c.send('Input.dispatchTouchEvent',dict(type='touchEnd',touchPoints=[]));c.detach()
        self.page.wait_for_timeout(650)
    def test_lyrics_tap_returns_but_scroll_does_not(self):
        self.play();self.page.locator('.mini-song').click();self.page.get_by_role('button',name='查看歌词',exact=True).click()
        self.page.locator('.lyric').click()
        expect(self.page.locator('.np-body')).to_have_class('np-body pane-cover')
        self.page.get_by_role('button',name='查看歌词',exact=True).click()
        self.swipe(160,240,0,120)
        expect(self.page.locator('.np-body')).to_have_class('np-body pane-lyrics')
    def test_player_edge_returns_cover_then_closes_without_interrupting_audio(self):
        self.play();self.page.locator('.mini-song').click();self.page.get_by_role('button',name='查看歌词',exact=True).click()
        self.swipe(8,300,180)
        expect(self.page.locator('.np-body')).to_have_class('np-body pane-cover')
        self.swipe(8,300,32)
        expect(self.page.locator('.now-playing.is-open')).to_have_count(1)
        self.swipe(8,300,210)
        expect(self.page.locator('.now-playing.is-open')).to_have_count(0)
        self.assertFalse(self.page.evaluate('window.__testAudios.find(a=>a.src).paused'))
        self.assertEqual(urlsplit(self.page.url).path,'/')
    def test_regular_pages_block_browser_edge_back_and_links_still_work(self):
        self.open('/');self.page.locator('.tab-bar a[href="/discover"]').click()
        # Cancellation must happen on touchstart: too late on touchmove in iOS Safari.
        blocked=self.page.evaluate('''()=>{const t=new Touch({identifier:1,target:document.querySelector('.content'),clientX:4,clientY:300});return !document.querySelector('.content').dispatchEvent(new TouchEvent('touchstart',{touches:[t],targetTouches:[t],changedTouches:[t],bubbles:true,cancelable:true}))}''')
        self.assertTrue(blocked,'native history swipe must be blocked at the left edge')
        self.swipe(8,300,220);self.assertEqual(urlsplit(self.page.url).path,'/discover')
        self.page.locator('.tab-bar a[href="/library"]').click();self.assertEqual(urlsplit(self.page.url).path,'/library')
    def test_scroll_records_are_coalesced_and_saved_before_navigation(self):
        self.open('/')
        self.page.evaluate('''async()=>{window.__scrollWrites=0;const e=document.querySelector('.content');for(let i=1;i<=24;i++){e.scrollTop=i*8;e.dispatchEvent(new Event('scroll'));await new Promise(r=>setTimeout(r,8))}}''')
        during=self.page.evaluate('window.__scrollWrites')
        self.assertLessEqual(during,2,'storage must not block each scrolling frame')
        old=self.page.locator('.content').evaluate('e=>e.scrollTop')
        self.page.locator('.shelf-item').first.evaluate('e=>e.click()');self.page.go_back()
        expect(self.page.locator('.crate')).to_be_visible()
        self.assertAlmostEqual(self.page.locator('.content').evaluate('e=>e.scrollTop'),old,delta=2)
    def test_mobile_charts_only_fetch_selected_not_hidden_rail_or_all_platforms(self):
        self.open('/');self.page.locator('.tab-bar a[href="/charts"]').click()
        self.page.locator('.podium').wait_for();self.page.wait_for_timeout(700)
        self.assertEqual(self.chart_requests,['/api/charts/netease/0'])
        self.page.get_by_role('tab',name='新歌',exact=True).first.click()
        self.page.wait_for_timeout(250)
        self.assertIn('/api/charts/netease/1',self.chart_requests)
    def test_home_short_return_reuses_data(self):
        self.open('/');self.page.locator('.tab-bar a[href="/discover"]').click();self.page.locator('.tab-bar a[href="/"]').click()
        self.page.wait_for_timeout(300)
        self.assertEqual(self.home_requests,1,'returning immediately should not re-fetch and re-store the home payload')
    def test_primary_tabs_do_not_grow_history_but_details_can_return(self):
        self.open('/')
        before=self.page.evaluate('history.length')
        for path in ['/discover','/library','/','/discover','/library','/']:
            self.page.locator(f'.tab-bar a[href="{path}"]').click()
        self.assertEqual(self.page.evaluate('history.length'),before)
        self.assertLessEqual(self.page.evaluate('Object.keys(sessionStorage).filter(k=>k.startsWith("music-scroll:")).length'),4,'peer tabs must reuse bounded scroll records too')
        self.page.locator('.shelf-item').first.click()
        expect(self.page.locator('.collection-header')).to_be_visible()
        self.page.get_by_role('button',name='返回上一页',exact=True).click()
        expect(self.page.locator('.crate')).to_be_visible()
    def test_equalizer_does_not_relayout_each_frame(self):
        self.play();self.page.locator('.tab-bar a[href="/charts"]').click();self.page.locator('.podium').wait_for()
        self.page.wait_for_timeout(800)
        # Play a row below the podium so the real equalizer animates.
        self.page.locator('.rank-list li').first.click();self.page.wait_for_timeout(650)
        c=self.context.new_cdp_session(self.page);c.send('Performance.enable')
        before={m['name']:m['value'] for m in c.send('Performance.getMetrics')['metrics']}
        self.page.wait_for_timeout(1100)
        after={m['name']:m['value'] for m in c.send('Performance.getMetrics')['metrics']}
        self.assertLess(after['LayoutCount']-before['LayoutCount'],8,'equalizer should animate on the compositor, not relayout the whole page')
        c.detach()
if __name__=='__main__':unittest.main()
