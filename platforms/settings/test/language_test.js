'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');

globalThis.window = globalThis;
globalThis.document = { querySelector: () => null };
require('../ui/mixed.js');
require('../ui/language.js');
const logic = window.ZcLanguage;

const settings = (value, items = []) => ({ profiles: { language: [{ key: 'language', value }] }, items });

test('側欄帶到語言頁，mixed.js 與 language.js 都在 app.js 之前載入', () => {
  const html = fs.readFileSync(path.join(__dirname, '../ui/index.html'), 'utf8');
  assert.match(html, /data-page="mode"/);
  assert.ok(html.indexOf('src="mixed.js"') < html.indexOf('src="app.js"'));
  assert.ok(html.indexOf('src="language.js"') < html.indexOf('src="app.js"'));
});

test('Mixed 設定在 mode 頁，P2-42 的鍵都在頁面順序裡', () => {
  const pages = fs.readFileSync(path.join(__dirname, '../ui/pages.js'), 'utf8');
  const mode = /mode: \{[\s\S]*?keys: \[([\s\S]*?)\]/.exec(pages);
  assert.ok(mode);
  const keys = [...mode[1].matchAll(/'([a-z-]+)'/g)].map((m) => m[1]);
  for (const key of ['primary-language', 'mixed-input', 'english-autocomplete', 'cjk-latin-space', 'model-mode', 'model-residency-mb', 'allow-placeholders', 'ja-input-method', 'ja-model']) {
    assert.ok(keys.includes(key), key);
  }
  // English profile 與分開的語言切換不出現在 Mixed 畫面。
  assert.equal(window.ZcMixed.hidden('language'), true);
  assert.equal(window.ZcMixed.hidden('language-hotkey'), true);
  const app = fs.readFileSync(path.join(__dirname, '../ui/app.js'), 'utf8');
  assert.match(app, /capabilityCard\(\)/);
  for (const cap of logic.CAPABILITIES) assert.ok(app.includes('cap.id'), '能力卡要列出旗標 id');
});

test('主要語言讀 primary-language；舊契約的 language 仍讀得到', () => {
  assert.equal(logic.activeLanguage({ items: [] }), 'zh');
  assert.equal(logic.activeLanguage({ profiles: {}, items: [] }), 'zh');
  assert.equal(logic.activeLanguage(null), 'zh');
  assert.equal(logic.activeLanguage(settings('zh')), 'zh');
  assert.equal(logic.activeLanguage(settings('ja')), 'ja');
  assert.equal(logic.activeLanguage({ profiles: { language: [{ key: 'language', value: 'ko' }] } }), 'zh');
  // 新契約：主要語言在 items。
  assert.equal(logic.activeLanguage({ items: [{ key: 'primary-language', value: 'ja' }] }), 'ja');
  assert.equal(logic.activeLanguage({ items: [{ key: 'primary-language', value: 'zh' }] }), 'zh');
  // 舊版把語言選項放在 items 時也讀得到。
  assert.equal(logic.activeLanguage({ items: [{ key: 'language', value: 'ja' }] }), 'ja');
  assert.equal(logic.languageLabel('ja'), '日文');
});

test('語言選項從 profiles.language 讀，缺少時退回 items 或空清單', () => {
  assert.equal(logic.languageItems(settings('ja'))[0].key, 'language');
  assert.deepEqual(logic.languageItems({ profiles: {}, items: [{ key: 'language', value: 'zh' }, { key: 'selection', value: 'neural' }] }).map((item) => item.key), ['language']);
  assert.deepEqual(logic.languageItems({ items: [{ key: 'selection' }] }), []);
  assert.deepEqual(logic.languageItems(null), []);
});

test('item.languages 決定適用的語言；沒有 languages 的通用項目兩種模式都出現', () => {
  const zhuyin = { key: 'keyboard-layout', languages: ['zh'] };
  const japanese = { key: 'ja-model', languages: ['ja'] };
  const common = { key: 'selection' };
  assert.equal(logic.applies(zhuyin, 'zh'), true);
  assert.equal(logic.applies(zhuyin, 'ja'), false);
  assert.equal(logic.applies(japanese, 'zh'), false);
  assert.equal(logic.applies(japanese, 'ja'), true);
  assert.equal(logic.applies(common, 'ja'), true);
  assert.equal(logic.applies({ key: 'x', languages: [] }, 'ja'), true);
  assert.deepEqual(logic.visible([zhuyin, japanese, common], 'ja').map((item) => item.key), ['ja-model', 'selection']);
});

test('注音專屬選項（排列、模糊音、聲調、標點）在日文模式全部收起', () => {
  const zhOnly = [
    'keyboard-layout', 'fuzzy-z-zh', 'fuzzy-c-ch', 'toneless', 'zhuyin-error-hint',
    'simplified-hotkey', 'space-selects', 'ctrl-punctuation',
    'shift-symbol-halfwidth', 'caps-lock-english',
    'emoji-shortcut', 'emoji-equals-first', 'style-tab',
  ].map((key) => ({ key, languages: ['zh'] }));
  assert.deepEqual(logic.visible(zhOnly, 'ja'), []);
  assert.equal(logic.visible(zhOnly, 'zh').length, zhOnly.length);
  for (const key of logic.LANGUAGE_KEYS) assert.equal(logic.applies({ key, languages: undefined }, 'ja'), true);
  for (const key of logic.JAPANESE_KEYS) assert.equal(logic.applies({ key, languages: ['ja'] }, 'ja'), true);
});

test('能力旗標名稱與 core/proto 的 Cap* 相同，四項都列得出來', () => {
  assert.deepEqual(logic.CAPABILITIES.map((cap) => cap.id),
    ['japanese', 'convert_keys', 'clause_display', 'live_conversion']);
  assert.equal(logic.CAPABILITIES.every((cap) => cap.label && !cap.label.includes('_')), true);
});
