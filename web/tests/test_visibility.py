"""Visible-but-unfocused is not backgrounded on desktop, WebView2, or touch UI."""
import unittest
from playwright.sync_api import expect
import test_power as power


class Visibility(unittest.TestCase):
    setUpClass=classmethod(power.Power.setUpClass.__func__)
    tearDownClass=classmethod(power.Power.tearDownClass.__func__)
    setUp=power.Power.setUp
    tearDown=power.Power.tearDown
    route=power.Power.route
    open=power.Power.open
    phone=power.Power.phone
    hidden=power.Power.hidden
    finish_download=power.Power.finish_download

    def setUp(self):
        power.Power.setUp(self)
        self.page.set_default_timeout(4000)

    def play(self):
        self.open('/')
        self.page.locator('.crate-entry-daily,.crate-row .sleeve').first.click()
        self.page.locator('.mini-player button[aria-label="暂停"]:visible').first.wait_for()

    def test_visible_blur_never_blanks_any_surface_or_loses_ui_state(self):
        for surface,width in [('desktop-web',1280),('phone-web',390)]:
            with self.subTest(surface=surface):
                self.page.set_viewport_size({'width':width,'height':844})
                self.play()
                self.page.locator('.mini-song').click()
                if width<760:
                    self.page.get_by_role('button',name='查看歌词',exact=True).click()
                self.page.evaluate('window.dispatchEvent(new Event("blur"))')
                expect(self.page.locator('.frame')).to_be_visible()
                expect(self.page.locator('.now-playing.is-open')).to_be_visible()
                if width<760:
                    expect(self.page.locator('.np-body')).to_have_class('np-body pane-lyrics')
                self.assertFalse(self.page.evaluate('document.documentElement.hasAttribute("data-page-hidden")'))
                self.assertFalse(self.page.evaluate('window.__testAudios.find(a=>a.src).paused'))
                self.page.evaluate('window.dispatchEvent(new Event("focus"))')

    def test_hidden_then_visible_without_focus_restores_ui_and_same_audio(self):
        self.play()
        self.page.locator('.content').evaluate('e=>e.scrollTop=180')
        self.page.evaluate('window.__sameAudio=window.__testAudios.find(a=>a.src);window.dispatchEvent(new Event("blur"))')
        self.hidden(True)
        expect(self.page.locator('.frame')).not_to_be_visible()
        before=self.page.evaluate('window.__sameAudio.currentTime')
        self.page.wait_for_timeout(600)
        self.assertGreater(self.page.evaluate('window.__sameAudio.currentTime'),before+.3)
        self.hidden(False) # A visible inactive window need not receive focus.
        expect(self.page.locator('.frame')).to_be_visible()
        self.assertTrue(self.page.evaluate('window.__testAudios.find(a=>a.src)===window.__sameAudio'))
        self.assertAlmostEqual(self.page.locator('.content').evaluate('e=>e.scrollTop'),180,delta=2)

    def test_pagehide_and_freeze_still_suspend_then_pageshow_and_resume_restore(self):
        self.play()
        for hide,show,target in [('pagehide','pageshow','window'),('freeze','resume','document')]:
            self.page.evaluate(f'{target}.dispatchEvent(new Event("{hide}"))')
            expect(self.page.locator('.frame')).not_to_be_visible()
            self.assertFalse(self.page.evaluate('window.__testAudios.find(a=>a.src).paused'))
            self.page.evaluate(f'{target}.dispatchEvent(new Event("{show}"))')
            expect(self.page.locator('.frame')).to_be_visible()


if __name__=='__main__':unittest.main()
