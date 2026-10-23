"""Touch-only selection/callout behavior, with editable controls preserved."""
import unittest
import json
from playwright.sync_api import expect
import test_foreground as fg

class TouchUI(unittest.TestCase):
    setUpClass=classmethod(fg.Foreground.setUpClass.__func__)
    tearDownClass=classmethod(fg.Foreground.tearDownClass.__func__)
    setUp=fg.Foreground.setUp
    tearDown=fg.Foreground.tearDown
    def route(self,r):
        if r.request.url.split('?')[0].endswith('/api/discover'):
            return r.fulfill(content_type='application/json',body=json.dumps(dict(hero=self.songs[0],more=self.songs,radar=self.songs[:12],fresh=self.songs,picks=[],random=[],world=[])))
        return fg.Foreground.route(self,r)
    open=fg.Foreground.open
    play=fg.Foreground.play
    finish_download=fg.Foreground.finish_download
    def test_phone_content_and_lyrics_disable_selection_and_callout(self):
        self.play()
        for selector in ['.section-head h2','.feature-rows li strong']:
            node=self.page.locator(selector).first
            self.assertEqual(node.evaluate('e=>getComputedStyle(e).userSelect'),'none')
            if self.page.evaluate('CSS.supports("-webkit-touch-callout","none")'):
                self.assertEqual(node.evaluate('e=>getComputedStyle(e).webkitTouchCallout'),'none')
        title=self.page.locator('.section-head h2').first
        title.dblclick()
        self.assertEqual(self.page.evaluate('getSelection().toString()'),'')
        self.page.locator('.mini-song').click();self.page.get_by_role('button',name='查看歌词',exact=True).click()
        self.assertEqual(self.page.locator('.lyric').evaluate('e=>getComputedStyle(e).userSelect'),'none')
    def test_phone_rename_and_search_remain_editable(self):
        self.open('/playlist/1');self.page.get_by_role('button',name='歌单操作',exact=True).click()
        self.page.get_by_role('menuitem',name='重命名',exact=True).click()
        field=self.page.get_by_role('textbox',name='歌单名称')
        self.assertEqual(field.evaluate('e=>getComputedStyle(e).userSelect'),'text')
        field.fill('保留 编辑');field.press('ControlOrMeta+a')
        self.assertEqual(field.evaluate('e=>e.selectionEnd-e.selectionStart'),5)
        self.page.keyboard.press('Escape')
        self.page.locator('.tab-bar a[href="/"]').click()
        self.page.locator('.page-header button[aria-label="搜索"]').click()
        field=self.page.get_by_role('combobox').first
        field.fill('测试 歌曲');field.press('ControlOrMeta+a')
        self.assertEqual(field.evaluate('e=>e.selectionEnd-e.selectionStart'),5)
    def test_desktop_text_selection_is_unchanged(self):
        self.context.close();self.context=self.browser.new_context(viewport={'width':1280,'height':860})
        self.context.route('**/api/**',self.route);self.context.route('**/test-cover.svg',lambda r:r.fulfill(content_type='image/svg+xml',body='<svg xmlns="http://www.w3.org/2000/svg"/>'))
        self.page=self.context.new_page();self.open('/')
        title=self.page.locator('.section-head h2').first
        self.assertNotEqual(title.evaluate('e=>getComputedStyle(e).userSelect'),'none')
        title.dblclick();self.assertNotEqual(self.page.evaluate('getSelection().toString()'),'')
    def test_heavy_roots_reuse_dom_without_leaving_hidden_animations_running(self):
        self.play();self.page.evaluate('window.__homeNode=document.querySelector(".crate")')
        for path in ['/discover','/charts','/','/discover','/charts','/']:
            self.page.locator(f'.tab-bar a[href="{path}"]').tap()
        self.assertTrue(self.page.evaluate('window.__homeNode===document.querySelector(".crate")'),'revisits should restore the existing page, not rebuild its whole DOM')
        expect(self.page.locator('.discover')).to_be_hidden()
        expect(self.page.locator('.charts-page')).to_be_hidden()
        self.assertEqual(self.page.evaluate('document.querySelectorAll(".route-panel[data-route-active=true]").length'),1)
        self.page.wait_for_timeout(500)
        self.assertEqual(self.page.evaluate('document.getAnimations().filter(a=>a.playState==="running"&&a.effect.target.closest(".route-panel")?.dataset.routeActive!=="true").length'),0)
        self.page.locator('.tab-bar a[href="/discover"]').tap()
        expect(self.page.locator('.discover')).to_be_visible()
        self.assertEqual(self.page.locator('.radar-sweep').evaluate('e=>getComputedStyle(e).animationPlayState'),'running')
    def test_retained_home_refreshes_after_renaming_a_playlist_elsewhere(self):
        self.open('/');self.page.locator('.shelf-item').first.click()
        self.page.get_by_role('button',name='歌单操作',exact=True).click()
        self.page.get_by_role('menuitem',name='重命名',exact=True).click()
        dialog=self.page.get_by_role('dialog',name='重命名歌单')
        dialog.get_by_role('textbox',name='歌单名称').fill('新的名字')
        dialog.get_by_role('button',name='保存',exact=True).click();expect(dialog).not_to_be_visible()
        self.page.locator('.tab-bar a[href="/"]').click()
        expect(self.page.locator('.shelf-item').first).to_contain_text('新的名字')
    def test_unsubmitted_form_survives_presentation_sleep(self):
        self.context.add_init_script('window.__hidden=false;Object.defineProperty(document,"hidden",{get:()=>window.__hidden})')
        self.open('/playlist/1');self.page.get_by_role('button',name='歌单操作',exact=True).click()
        self.page.get_by_role('menuitem',name='重命名',exact=True).click()
        field=self.page.get_by_role('textbox',name='歌单名称');field.fill('尚未保存的名字')
        self.page.evaluate('window.__hidden=true;document.dispatchEvent(new Event("visibilitychange"))')
        self.page.wait_for_timeout(150)
        self.assertEqual(self.page.locator('.frame').evaluate('e=>getComputedStyle(e).display'),'none')
        self.page.evaluate('window.__hidden=false;document.dispatchEvent(new Event("visibilitychange"))')
        expect(field).to_be_visible();expect(field).to_have_value('尚未保存的名字')
        self.page.keyboard.press('Escape');self.page.locator('.tab-bar a[href="/"]').click()
        self.page.locator('.page-header button[aria-label="搜索"]:visible').click()
        search=self.page.get_by_role('combobox');search.fill('尚未提交的搜索')
        self.page.evaluate('window.__hidden=true;document.dispatchEvent(new Event("visibilitychange"))')
        self.page.wait_for_timeout(150)
        self.page.evaluate('window.__hidden=false;document.dispatchEvent(new Event("visibilitychange"))')
        expect(search).to_be_visible();expect(search).to_have_value('尚未提交的搜索')
if __name__=='__main__':unittest.main()
