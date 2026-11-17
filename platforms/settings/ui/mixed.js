// P2-42：Mixed profile 的設定判斷。純資料與字串，node 測試直接載入（test/mixed_test.js）；
// app.js 用它收起 English profile 設定、標示 Mixed 模式不適用的注音選項，以及顯示 -model-report 的摘要。
(function () {
  'use strict';

  // 舊 English profile／分開的輸入語言設定：Mixed profile 直接接受注音、日文與英文，
  // 這些欄位不再出現在設定畫面；設定檔的舊值由 Go 端遷移後移除。
  const HIDDEN = {
    'language': 'Mixed profile 直接接受注音、日文與英文，不需要選輸入語言。',
    'language-memory': 'Mixed profile 不切換輸入語言，這項不適用。',
    'language-hotkey': 'Mixed profile 不切換輸入語言，這項不適用。',
    'start-mode': 'Mixed profile 直接從混合模式開始，不再有 English profile 的開始模式。',
    'english-profile': 'English profile 已取消；英文直接在 Mixed 內輸入。',
  };

  // 注音專屬、在 Mixed 模式已由混打固定行為取代的選項。設定檔的值保留，切到其他平台或
  // 關閉混打時照舊；Mixed 畫面把這些列標成不適用，不讓使用者以為改了有效果。
  const ZHUYIN_ONLY = {
    'mixed-input': 'Mixed profile 固定開啟英文直接輸入。',
    'space-selects': 'Mixed 模式固定用空白送出或選字，這項不作用。',
    'toneless': 'Mixed 模式不使用「不打聲調也能選字」。',
    'zhuyin-error-hint': 'Mixed 模式不使用打錯注音提示。',
  };

  const OUTPUT_VARIANTS = [
    { id: 'traditional', label: '繁體中文' },
    { id: 'simplified', label: '簡體中文' },
    { id: 'shinjitai', label: '日文新字體' },
  ];

  const GROUPS = [
    { title: '主要語言', keys: ['primary-language'] },
    { title: '混合輸入', keys: ['mixed-input', 'english-autocomplete', 'cjk-latin-space'] },
    { title: '模型', keys: ['model-mode', 'model-residency-mb', 'model-set', 'allow-placeholders'] },
    { title: '日文', keys: ['ja-input-method', 'ja-live-conversion', 'ja-symbols', 'ja-learning', 'ja-model'] },
  ];

  const MIXED_KEYS = GROUPS.flatMap((group) => group.keys);

  // Mixed profile 只在 Windows（P2-40）出現；其他平台仍用原本的語言與中英切換設定。
  // 較新的 0c-update 可以用 features.mixed 明確覆寫，測試與舊版契約不必先改。
  function isMixed(info, settings) {
    const features = settings && settings.features;
    if (features && typeof features.mixed === 'boolean') return features.mixed;
    return /^windows\//.test((info && info.platform) || '');
  }

  const hidden = (key) => Object.prototype.hasOwnProperty.call(HIDDEN, key);
  const hiddenReason = (key) => HIDDEN[key] || '';
  const zhuyinOnly = (key) => Object.prototype.hasOwnProperty.call(ZHUYIN_ONLY, key);
  const zhuyinReason = (key) => ZHUYIN_ONLY[key] || '';
  const visible = (key) => !hidden(key);
  const keys = () => MIXED_KEYS.slice();
  const groups = () => GROUPS.map((group) => ({ title: group.title, keys: group.keys.slice() }));

  function label(list, value) {
    const found = list.find((entry) => entry.id === value);
    return found ? found.label : (value || '');
  }
  const outputVariantLabel = (value) => label(OUTPUT_VARIANTS, value);

  const MODEL_SETS = [
    { id: 'public-core', label: 'Public Core 公開核心' },
    { id: 'cc-by-sa', label: 'CC BY-SA 實驗' },
  ];
  const modelSetLabel = (value) => label(MODEL_SETS, value);

  const mb = (bytes) => (Math.max(0, bytes || 0) / (1024 * 1024)).toLocaleString('zh-TW', { maximumFractionDigits: 1 }) + ' MB';

  // -model-report 的摘要（P2-33／P2-27）。resident_bytes 是模型檔常駐量；placeholder
  // 一定要寫明未訓練，cc-by-sa 目前整組都是 placeholder。
  function reportFacts(report) {
    if (!report) return [];
    if (report.ok === false) {
      const message = (report.error && (report.error.message || report.error.code)) || '未知錯誤';
      return [{ label: '模型報告', value: '無法讀取：' + message, tone: 'warn' }];
    }
    const set = report.model_set || {};
    const facts = [];
    if (!set.active) {
      facts.push({ label: '使用中的模型組', value: '未選定' + (set.reason ? '（' + set.reason + '）' : ''), tone: 'muted' });
    } else {
      const variant = set.variant ? modelSetLabel(set.variant) : '未知變體';
      facts.push({ label: '使用中的模型組', value: (set.set_id || '未命名') + '（' + variant + '）', tone: 'ok' });
      facts.push({ label: '模型組變體', value: variant + (set.version ? ' · ' + set.version : ''), tone: 'ok' });
    }
    facts.push({
      label: 'placeholder',
      value: set.placeholder ? '是（未訓練，預設不會載入）' : (set.active ? '否' : '未知'),
      tone: set.placeholder ? 'warn' : 'ok',
    });
    const resident = { label: '模型常駐', value: mb(report.resident_bytes), tone: 'muted' };
    if (report.budget_bytes > 0) {
      resident.value += ' / 預算 ' + mb(report.budget_bytes);
      resident.tone = report.within_budget === false ? 'warn' : 'ok';
    }
    facts.push(resident);
    if (set.active && set.valid === false) {
      facts.push({ label: '模型組檢查', value: set.reason || '沒有通過核對', tone: 'warn' });
    } else if (set.active && set.placeholder && !report.allow_placeholders) {
      facts.push({ label: '載入政策', value: 'placeholder 未允許，使用統計轉換', tone: 'warn' });
    }
    return facts;
  }

  window.ZcMixed = {
    HIDDEN, ZHUYIN_ONLY, OUTPUT_VARIANTS, MODEL_SETS, GROUPS, MIXED_KEYS,
    isMixed, hidden, hiddenReason, visible, zhuyinOnly, zhuyinReason, keys, groups,
    outputVariantLabel, modelSetLabel, reportFacts, mb,
  };
})();
