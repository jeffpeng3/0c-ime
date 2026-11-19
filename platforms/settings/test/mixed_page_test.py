"""Real-browser regression tests for the Mixed page (P2-48): python3 platforms/settings/test/mixed_page_test.py.

The settings window from 0.2.13-preview.1 was paired with an older 0c-update
(8a18db0). That updater's settings JSON has no features.mixed, so the page is
chosen by platform, exactly like a Windows install. Rendering used to throw
"TypeError: Cannot read properties of undefined (reading 'includes')" after the
title and lead paragraph and left the page empty.

Runs in Chromium (WebView2 on Windows) and WebKit (WebKitGTK on Linux,
WKWebView on macOS). Requires Playwright; an engine that is missing is skipped.
Mocks only the Tauri command boundary, using the actual HTML/CSS/JS.
"""
import json
from functools import partial
from http.server import ThreadingHTTPServer
from pathlib import Path
import threading
import unittest

from scroll_position_test import QuietHandler, launch_chromium, launch_webkit

# Shape returned by 0c-update 8a18db0 (0.2.13-preview.1): features has only
# vocab and personal_reset, there is no features.mixed, and profiles has no
# language array. The item list matches the contract of that release.
OLD_SETTINGS = {
    "schema": 1,
    "ok": True,
    "file": {"path": "/home/test/.local/share/0c/user.json", "state": "ok", "schema": 2, "read_only": False, "notes": []},
    "engine": {"state": "not_running"},
    "pending_restart": False,
    "profiles": {"active": "builtin-default", "running": "builtin-default", "pending": False,
                 "items": [{"id": "builtin-default", "name": "預設", "builtin": True}]},
    "features": {"vocab": True, "personal_reset": True},
    "items": [
        {"key": "primary-language", "label": "主要語言", "description": "影響先驗與輸出變體。", "values": ["zh", "ja"], "value": "zh"},
        {"key": "mixed-input", "label": "中英混打", "description": "直接打英文，不必切換。", "values": ["off", "on"], "value": "on", "languages": ["zh"]},
        {"key": "english-autocomplete", "label": "英文自動補字", "description": "英文詞打到一半時提示。", "values": ["off", "on"], "value": "on", "languages": ["zh"]},
        {"key": "cjk-latin-space", "label": "中英之間加空格", "description": "送出時自動補空白。", "values": ["off", "on"], "value": "off", "languages": ["zh"]},
        {"key": "model-mode", "label": "模型模式", "description": "要載入哪些模型。", "values": ["all", "primary", "none"], "value": "primary", "locked": True, "locked_reason": "由內建風格決定"},
        {"key": "model-residency-mb", "label": "模型常駐上限", "description": "模型最多佔用的記憶體。", "values": ["auto", "250", "400", "512", "1024"], "value": "auto"},
        {"key": "allow-placeholders", "label": "允許 placeholder", "description": "未訓練的模型預設不載入。", "values": ["off", "on"], "value": "off"},
        {"key": "ja-input-method", "label": "日文輸入方式", "description": "羅馬字或直接かな。", "values": ["romaji", "kana"], "value": "romaji", "languages": ["ja"]},
        {"key": "ja-live-conversion", "label": "ライブ変換", "description": "邊打邊轉換。", "values": ["off", "on"], "value": "off", "languages": ["ja"]},
        {"key": "ja-symbols", "label": "記号與顔文字", "description": "日文符號候選。", "values": ["off", "on"], "value": "on", "languages": ["ja"]},
        {"key": "ja-learning", "label": "日文學習", "description": "記住選過的寫法。", "values": ["off", "on"], "value": "on", "languages": ["ja"]},
        {"key": "ja-model", "label": "日文神經模型", "description": "日文推論。", "values": ["off", "on"], "value": "off", "languages": ["ja"]},
        {"key": "toggle-key", "label": "中英切換鍵", "description": "用哪個鍵切換。", "values": ["shift", "both"], "value": "shift"},
        {"key": "start-mode", "label": "開始時的中英模式", "description": "每個輸入框開始時的模式。", "values": ["zh", "en", "native"], "value": "zh"},
        {"key": "mode-memory", "label": "換程式後的中英模式", "description": "記住每個程式的模式。", "values": ["global", "field"], "value": "field"},
        {"key": "caps-lock-english", "label": "Caps Lock 開著時直接打英文", "description": "Caps Lock 時不組字。", "values": ["off", "on"], "value": "on"},
        {"key": "style-shift-letter", "label": "Shift 加字母", "description": "樣式。", "values": ["off", "on"], "value": "off"},
        {"key": "style-caps-letter", "label": "Caps Lock 加字母", "description": "樣式。", "values": ["off", "on"], "value": "off"},
        {"key": "space-selects", "label": "空白鍵選字", "description": "空白鍵送出或選字。", "values": ["off", "on"], "value": "on", "languages": ["zh"]},
        {"key": "toneless", "label": "不打聲調也能選字", "description": "聲調可以省略。", "values": ["off", "on"], "value": "off", "languages": ["zh"]},
        {"key": "zhuyin-error-hint", "label": "打錯注音時提示", "description": "提示打錯的注音。", "values": ["off", "on"], "value": "on", "languages": ["zh"]},
        {"key": "emoji-shortcut", "label": "Emoji 選單", "description": "打出 Emoji 候選。", "values": ["off", "equals"], "value": "equals", "languages": ["zh"]},
        {"key": "emoji-equals-first", "label": "將 = 放在第一項", "description": "Emoji 選單第一項是等號。", "values": ["off", "on"], "value": "on", "languages": ["zh"]},
        {"key": "keyboard-layout", "label": "鍵盤排列", "description": "注音鍵盤排列。", "values": ["standard", "eten", "hsu"], "value": "standard", "languages": ["zh"]},
        {"key": "output-variant", "label": "輸出變體", "description": "送出的字形。", "values": ["traditional", "simplified", "shinjitai"], "value": "traditional", "languages": ["zh"]},
    ],
}

# A newer contract that also reports features.mixed explicitly.
COMPLETE_SETTINGS = json.loads(json.dumps(OLD_SETTINGS))
COMPLETE_SETTINGS["features"]["mixed"] = True
COMPLETE_SETTINGS["profiles"]["language"] = [
    {"key": "ja-input-method", "label": "日文輸入方式", "values": ["romaji", "kana"], "value": "romaji"},
]

STATUS = {
    "schema": 1,
    "installed": "0.2.13-preview.1",
    "mode": "manual",
    "runtime": {"state": "not_running"},
    "update": {"state": "never_checked", "stage": "idle"},
    "features": {"vocab": True, "personal_reset": True},
    "installs": [],
}


class MixedPageTests:
    @classmethod
    def setUpClass(cls):
        try:
            from playwright.sync_api import sync_playwright
        except ImportError:
            raise unittest.SkipTest("requires Playwright")
        cls.playwright = sync_playwright().start()
        try:
            cls.browser = cls.launch(cls.playwright)
        except unittest.SkipTest:
            cls.playwright.stop()
            raise
        cls.server = ThreadingHTTPServer(("127.0.0.1", 0), partial(QuietHandler, directory=str(Path(__file__).resolve().parents[1] / "ui")))
        cls.thread = threading.Thread(target=cls.server.serve_forever, daemon=True)
        cls.thread.start()

    @classmethod
    def tearDownClass(cls):
        cls.server.shutdown()
        cls.thread.join()
        cls.server.server_close()
        cls.browser.close()
        cls.playwright.stop()

    def open_mixed(self, settings):
        page = self.browser.new_page(viewport={"width": 960, "height": 680})
        self.addCleanup(page.close)
        errors = []
        page.on("pageerror", lambda error: errors.append(str(error)))
        self.errors = errors
        page.add_init_script("""
          window.testIntervals = [];
          window.setInterval = (callback, delay) => window.testIntervals.push({callback, delay});
          window.testSettings = SETTINGS;
          window.testSettle = async () => {
            await new Promise(resolve => setTimeout(resolve, 50));
            await new Promise(requestAnimationFrame);
            await new Promise(resolve => setTimeout(resolve, 50));
          };
          window.__TAURI__ = {
            core: {invoke: async (command) => {
              if (command === 'app_info') return {platform:'windows/x86_64', version:'0.2.13-preview.1'};
              if (command === 'get_settings') return structuredClone(window.testSettings);
              if (command === 'get_status') return structuredClone(STATUS);
              throw new Error('unexpected command: ' + command);
            }}, event: {listen: async () => () => {}}
          };
        """.replace("SETTINGS", json.dumps(settings, ensure_ascii=False)).replace("STATUS", json.dumps(STATUS, ensure_ascii=False)))
        page.goto(f"http://127.0.0.1:{self.server.server_port}/index.html?page=mode")
        page.locator('#page-mode h1').wait_for(state="attached")
        page.evaluate("window.testSettle()")
        self.assertEqual(errors, [])
        self.assertEqual(page.eval_on_selector('#toast', 'node => node.hidden'), True)
        return page

    def assert_mixed_layout(self, page):
        self.assertEqual(page.locator('#page-mode h1').inner_text(), "Mixed 輸入")
        self.assertEqual(page.locator('.nav[data-page="mode"]').inner_text(), "Mixed 輸入")
        self.assertEqual(page.locator('#page-mode h2').all_inner_texts(), ["主要語言", "混合輸入", "模型", "日文", "輸入習慣"])
        self.assertEqual(page.locator('#label-primary-language').count(), 1)
        self.assertEqual(page.locator('#label-model-mode').count(), 1)
        self.assertEqual(page.locator('#label-ja-model').count(), 1)
        # mixed-input is fixed on in the Mixed profile and says so instead of showing a switch.
        self.assertIn("Mixed 模式不適用", page.locator('#label-mixed-input').inner_text())
        self.assertIn("固定開啟", page.locator('#page-mode').inner_text())
        self.assertIn("選用風格／複製成自訂", page.locator('#page-mode').inner_text())

    def test_old_updater_settings_render_the_mixed_page(self):
        # 8a18db0 has no features.mixed: the platform selects the Mixed page. P2-48 used to
        # throw here because the ad-hoc page spec had no keys array.
        self.assert_mixed_layout(self.open_mixed(OLD_SETTINGS))

    def test_settings_without_any_features_field_render(self):
        settings = json.loads(json.dumps(OLD_SETTINGS))
        settings.pop("features")
        self.assert_mixed_layout(self.open_mixed(settings))

    def test_complete_contract_renders_the_same_mixed_page(self):
        self.assert_mixed_layout(self.open_mixed(COMPLETE_SETTINGS))

    def test_item_without_a_key_does_not_break_the_page(self):
        settings = json.loads(json.dumps(OLD_SETTINGS))
        settings["items"].insert(0, {"label": "缺少 key 的舊資料"})
        self.assert_mixed_layout(self.open_mixed(settings))

    def test_other_settings_pages_still_render_with_the_old_shape(self):
        page = self.open_mixed(OLD_SETTINGS)
        for name, title in (("candidates", "選字"), ("keyboard", "鍵盤與符號"), ("output", "輸出設定"), ("overview", "總覽")):
            with self.subTest(page=name):
                page.locator(f'.nav[data-page="{name}"]').click()
                page.locator(f'#page-{name} h1').wait_for(state="attached")
                page.evaluate("window.testSettle()")
                self.assertEqual(page.locator(f'#page-{name} h1').inner_text(), title)
                self.assertEqual(page.eval_on_selector('#toast', 'node => node.hidden'), True)
                self.assertEqual(self.errors, [])


class ChromiumMixedPageTests(MixedPageTests, unittest.TestCase):
    """WebView2 (Windows) is Chromium."""

    launch = staticmethod(launch_chromium)


class WebKitMixedPageTests(MixedPageTests, unittest.TestCase):
    """WebKitGTK (Linux) and WKWebView (macOS) are WebKit."""

    launch = staticmethod(launch_webkit)


if __name__ == '__main__':
    unittest.main()
