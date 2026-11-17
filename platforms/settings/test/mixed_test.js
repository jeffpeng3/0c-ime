// P2-42：Mixed profile 的設定畫面判斷（ui/mixed.js）。node --test platforms/settings/test/
'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');

globalThis.window = globalThis;
require('../ui/mixed.js');
const M = window.ZcMixed;

test('只有 Windows 的 Mixed profile 收起 English profile；其他平台照舊', () => {
  assert.equal(M.isMixed({ platform: 'windows/x86_64' }, {}), true);
  assert.equal(M.isMixed({ platform: 'linux/x86_64' }, {}), false);
  assert.equal(M.isMixed({ platform: 'macos/arm64' }, {}), false);
  // 較新的 0c-update 可以明確覆寫；false 不會被平台蓋掉。
  assert.equal(M.isMixed({ platform: 'linux/x86_64' }, { features: { mixed: true } }), true);
  assert.equal(M.isMixed({ platform: 'windows/x86_64' }, { features: { mixed: false } }), false);
});

test('English profile 與分開的輸入語言不顯示，並保留遷移說明', () => {
  for (const key of ['language', 'language-memory', 'language-hotkey', 'start-mode', 'english-profile']) {
    assert.equal(M.hidden(key), true, key);
    assert.equal(M.visible(key), false, key);
    assert.ok(M.hiddenReason(key).length > 0, key);
  }
  for (const key of ['primary-language', 'mixed-input', 'english-autocomplete', 'cjk-latin-space', 'model-mode', 'model-set']) {
    assert.equal(M.hidden(key), false, key);
    assert.equal(M.visible(key), true, key);
  }
});

test('Mixed 模式不適用的注音選項要標示原因', () => {
  for (const key of ['mixed-input', 'space-selects', 'toneless', 'zhuyin-error-hint']) {
    assert.equal(M.zhuyinOnly(key), true, key);
    assert.ok(M.zhuyinReason(key).length > 0, key);
  }
  assert.equal(M.zhuyinOnly('keyboard-layout'), false);
  assert.equal(M.zhuyinReason('keyboard-layout'), '');
});

test('Mixed 頁分成主要語言、混合輸入、模型與日文，順序固定', () => {
  assert.deepEqual(M.groups().map((g) => g.title), ['主要語言', '混合輸入', '模型', '日文']);
  assert.deepEqual(M.groups()[1].keys, ['mixed-input', 'english-autocomplete', 'cjk-latin-space']);
  assert.deepEqual(M.groups()[2].keys, ['model-mode', 'model-residency-mb', 'model-set', 'allow-placeholders']);
  assert.equal(M.keys().includes('primary-language'), true);
  assert.equal(M.keys().includes('ja-model'), true);
  // 每一次回傳都是複本，畫面改動不會污染模組常數。
  M.groups()[0].keys.push('oops');
  assert.equal(M.groups()[0].keys.includes('oops'), false);
});

test('輸出變體與模型組的名稱', () => {
  assert.equal(M.outputVariantLabel('traditional'), '繁體中文');
  assert.equal(M.outputVariantLabel('simplified'), '簡體中文');
  assert.equal(M.outputVariantLabel('shinjitai'), '日文新字體');
  assert.equal(M.outputVariantLabel(''), '');
  assert.equal(M.modelSetLabel('public-core'), 'Public Core 公開核心');
  assert.equal(M.modelSetLabel('cc-by-sa'), 'CC BY-SA 實驗');
});

test('-model-report：公開核心、非 placeholder、常駐位元組與預算', () => {
  const facts = M.reportFacts({
    resident_bytes: 314572800, budget_bytes: 419430400, within_budget: true, allow_placeholders: false,
    model_set: { active: true, set_id: 'ja-public-core', variant: 'public-core', version: '1', placeholder: false, valid: true },
  });
  const by = Object.fromEntries(facts.map((f) => [f.label, f]));
  assert.equal(by['使用中的模型組'].value, 'ja-public-core（Public Core 公開核心）');
  assert.equal(by['模型組變體'].value, 'Public Core 公開核心 · 1');
  assert.equal(by.placeholder.value, '否');
  assert.equal(by['模型常駐'].value, '300 MB / 預算 400 MB');
  assert.equal(by['模型常駐'].tone, 'ok');
});

test('-model-report：cc-by-sa placeholder 要清楚標示未訓練，不假裝可用', () => {
  const facts = M.reportFacts({
    resident_bytes: 0, budget_bytes: 0, allow_placeholders: false,
    model_set: { active: true, set_id: 'ja-cc-by-sa', variant: 'cc-by-sa', placeholder: true, valid: true },
  });
  const by = Object.fromEntries(facts.map((f) => [f.label, f]));
  assert.equal(by.placeholder.value, '是（未訓練，預設不會載入）');
  assert.equal(by.placeholder.tone, 'warn');
  assert.equal(by['載入政策'].value, 'placeholder 未允許，使用統計轉換');
  assert.equal(by['模型常駐'].value, '0 MB');
});

test('-model-report：未選定模型組、報告失敗與超預算', () => {
  const none = Object.fromEntries(M.reportFacts({
    resident_bytes: 1, budget_bytes: 0, model_set: { active: false, reason: 'not_configured' },
  }).map((f) => [f.label, f]));
  assert.equal(none['使用中的模型組'].value, '未選定（not_configured）');
  assert.equal(none.placeholder.value, '未知');

  const failed = M.reportFacts({ ok: false, error: { code: 'io', message: '找不到 0c-server' } });
  assert.equal(failed[0].value, '無法讀取：找不到 0c-server');
  assert.equal(failed[0].tone, 'warn');
  assert.deepEqual(M.reportFacts(null), []);

  const over = Object.fromEntries(M.reportFacts({
    resident_bytes: 500 << 20, budget_bytes: 400 << 20, within_budget: false,
    model_set: { active: true, set_id: 'ja-public-core', variant: 'public-core' },
  }).map((f) => [f.label, f]));
  assert.equal(over['模型常駐'].tone, 'warn');
});
