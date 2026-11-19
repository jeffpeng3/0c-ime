// P2-48：設定頁面的項目順序（ui/pages.js）。node --test platforms/settings/test/
'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');

globalThis.window = globalThis;
require('../ui/mixed.js');
require('../ui/pages.js');
const P = window.ZcPages;
const M = window.ZcMixed;

test('Mixed 頁沿用 mode 的 keys，只換標題與說明', () => {
  const plain = P.spec('mode', false);
  const mixed = P.spec('mode', true);
  assert.equal(plain.title, '語言與中英切換');
  assert.equal(mixed.title, 'Mixed 輸入');
  assert.ok(mixed.lead.length > 0);
  assert.ok(Array.isArray(mixed.keys), 'keys 必須是陣列，排序與 pageOfKey 都讀它');
  assert.deepEqual(mixed.keys, plain.keys);
  assert.equal(mixed.keys.includes('primary-language'), true);
  assert.equal(mixed.keys.includes('mixed-input'), true);
  assert.equal(P.title('mode', true), 'Mixed 輸入');
  assert.equal(P.title('mode', false), '語言與中英切換');
  assert.equal(P.title('candidates', true), '選字');
  assert.equal(P.title('nope', true), '輸入設定');
  assert.equal(P.spec('nope', true), null);
  // 每一次都是複本，畫面或測試改動不會污染模組常數。
  mixed.keys.push('oops');
  assert.equal(P.spec('mode', true).keys.includes('oops'), false);
  assert.equal(P.spec('mode', false).keys.includes('oops'), false);
});

test('Mixed 分組的每一個鍵都在 mode 頁的順序裡', () => {
  const keys = P.spec('mode', true).keys;
  for (const key of M.MIXED_KEYS) {
    assert.ok(keys.includes(key), key);
    assert.equal(P.pageOfKey(key), 'mode', key);
    assert.ok(P.order(keys, key) < keys.length, key);
  }
  // 每一次 groups() 都是複本，順序仍是畫面用的。
  assert.deepEqual(M.groups().map((group) => group.title), ['主要語言', '混合輸入', '模型', '日文']);
});

test('pageOfKey：輸出、模糊音、未知與缺少 key 都有預設頁面', () => {
  assert.equal(P.pageOfKey('output-variant'), 'output');
  assert.equal(P.pageOfKey('output-simplified'), 'output');
  assert.equal(P.pageOfKey('simplified-hotkey'), 'output');
  assert.equal(P.pageOfKey('fuzzy-z-zh'), 'candidates');
  assert.equal(P.pageOfKey('selection'), 'candidates');
  assert.equal(P.pageOfKey('mixed-input'), 'mode');
  assert.equal(P.pageOfKey('keyboard-layout'), 'keyboard');
  assert.equal(P.pageOfKey('not-in-the-contract'), 'candidates');
  // 舊資料或壞掉的項目可能沒有 key（undefined、null、數字）：照樣回一個頁面，不丟錯。
  for (const key of [undefined, null, 42, {}, []]) assert.equal(P.pageOfKey(key), 'candidates');
});

test('order：缺少 keys 或非陣列時排到最後，不丟錯', () => {
  assert.equal(P.order(['a', 'b'], 'b'), 1);
  assert.equal(P.order(['a', 'b'], 'missing'), 2);
  assert.equal(P.order(undefined, 'a'), 0);
  assert.equal(P.order(null, 'a'), 0);
  assert.equal(P.order('not-an-array', 'a'), 0);
  assert.equal(P.order([], 'a'), 0);
});

test('index.html 在 app.js 之前載入 pages.js', () => {
  const html = fs.readFileSync(path.join(__dirname, '../ui/index.html'), 'utf8');
  assert.ok(html.indexOf('src="pages.js"') > 0);
  assert.ok(html.indexOf('src="pages.js"') < html.indexOf('src="app.js"'));
});
