"""Desktop history and warm chart switching must survive mobile-only changes."""
import unittest
from playwright.sync_api import expect
import test_foreground as foreground


class DesktopBoundary(unittest.TestCase):
    setUpClass=classmethod(foreground.Foreground.setUpClass.__func__)
    tearDownClass=classmethod(foreground.Foreground.tearDownClass.__func__)
    tearDown=foreground.Foreground.tearDown
    route=foreground.Foreground.route
    open=foreground.Foreground.open
    finish_download=foreground.Foreground.finish_download

    def setUp(self):
        foreground.Foreground.setUp(self)
        self.context.close()
        self.context=self.browser.new_context(viewport={'width':1280,'height':860},reduced_motion='reduce')
        self.context.route('**/api/**',self.route)
        self.context.route('**/test-cover.svg',lambda r:r.fulfill(content_type='image/svg+xml',body='<svg xmlns="http://www.w3.org/2000/svg"/>'))
        self.page=self.context.new_page()
        self.page.on('pageerror',lambda e:self.errors.append(str(e)))

    def test_desktop_navigation_keeps_normal_history_and_back(self):
        self.open('/')
        before=self.page.evaluate('history.length')
        self.page.locator('.side-nav a[href="/discover"]').click()
        self.page.locator('.side-nav a[href="/library"]').click()
        self.assertEqual(self.page.evaluate('history.length'),before+2)
        self.page.go_back()
        expect(self.page.locator('.discover')).to_be_visible()
        self.page.go_back()
        expect(self.page.locator('.crate')).to_be_visible()

    def test_desktop_warms_catalog_and_switches_without_duplicate_fetch(self):
        self.open('/')
        self.page.locator('.side-nav a[href="/charts"]').click()
        self.page.locator('.podium').wait_for()
        for _ in range(50):
            if len(set(self.chart_requests))==16:break
            self.page.wait_for_timeout(100)
        self.assertEqual(len(set(self.chart_requests)),16,'desktop retains the original whole-catalog warm switching')
        before=len(self.chart_requests)
        self.page.get_by_role('tab',name='QQ 音乐',exact=True).click()
        self.page.locator('.podium').wait_for()
        self.page.get_by_role('tab',name='网易云',exact=True).click()
        self.assertEqual(len(self.chart_requests),before)


if __name__=='__main__':unittest.main()
