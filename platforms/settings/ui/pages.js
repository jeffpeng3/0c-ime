// 設定頁面的項目與順序（#207、P2-42）。純資料與字串，node 測試直接載入（test/pages_test.js）；
// app.js 依這份順序畫控制項，也是 Tab 順序。
(function () {
  'use strict';

  // 不在任何一頁的新項目（較新的 0c-update 才有的設定）放在「選字」最後，所以契約新增設定時畫面
  // 不必先改；要放別頁再加進 keys。輸出設定的三項在 OUTPUT_KEYS。
  // T27：語言選擇與日文選項放在第一頁最上面；日文模式只留通用項目與日文選項（item.languages）。
  const INPUT_PAGES = {
    mode: {
      title: '語言與中英切換', lead: '選擇主要語言、混合輸入與模型，以及中英切換鍵與換程式後回到哪個模式。Mixed profile 直接接受注音、日文與英文。',
      keys: ['primary-language', 'language', 'language-memory', 'language-hotkey', 'mixed-input', 'english-autocomplete', 'cjk-latin-space',
        'model-mode', 'model-residency-mb', 'allow-placeholders',
        'ja-input-method', 'ja-live-conversion', 'ja-symbols', 'ja-learning', 'ja-model',
        'toggle-key', 'start-mode', 'mode-memory', 'caps-lock-english', 'style-shift-letter', 'style-caps-letter'],
    },
    candidates: {
      title: '選字', lead: '候選字怎麼排、怎麼選，以及要不要列出近音的字（模糊音）。',
      // zhuyin-error-hint（#300，打錯注音時提示）跟不打聲調一樣是按聲調時的規則，接在它後面。
      keys: ['selection', 'document-context', 'space-selects', 'candidate-cursor', 'advance-after-select', 'candidate-layout', 'selection-keys', 'esc-clear', 'toneless', 'zhuyin-error-hint', 'style-tab', 'style-up', 'style-candidate-tab', 'style-candidate-space', 'style-candidate-enter', 'style-page-size', 'style-candidate-navigation'],
    },
    keyboard: {
      title: '鍵盤與符號', lead: '注音的鍵盤排列，以及標點符號和 Emoji 怎麼打。',
      // emoji-equals-first 是 Emoji 選單的子選項，畫在它下面。
      // shift-symbol-halfwidth（標點寬度，#258、#282 三選一）跟 Ctrl＋標點是一對（半形與全形），排在它下面；
      // cjk-latin-space（#273，中英文之間自動加空格）也是半形排版，接在後面。
      keys: ['keyboard-layout', 'ctrl-punctuation', 'shift-symbol-halfwidth', 'cjk-latin-space', 'punctuation', 'emoji-shortcut', 'emoji-equals-first', 'style-punctuation', 'style-shift-space', 'style-shift-delete'],
    },
  };
  const OUTPUT_KEYS = ['output-variant', 'output-simplified', 'simplified-hotkey'];

  // Mixed profile 的「Mixed 輸入」沿用 mode 頁的 keys，只換標題與說明。keys 一定要留著：
  // 排序（order）與 pageOfKey 都讀它，少了就整頁在畫完標題後丟錯。
  const MIXED_MODE = {
    title: 'Mixed 輸入',
    lead: '主要語言、混合輸入與模型設定。Mixed profile 直接接受注音、日文與英文，不需要切換輸入語言或 English profile。',
  };

  // 頁面描述；不認得的頁面回傳 null。混合頁是 mode 的複本，不是另一份沒有 keys 的物件。
  function spec(name, mixed) {
    const page = Object.prototype.hasOwnProperty.call(INPUT_PAGES, name) ? INPUT_PAGES[name] : null;
    if (!page) return null;
    return name === 'mode' && mixed ? Object.assign({}, page, MIXED_MODE, { keys: page.keys.slice() }) : page;
  }

  const title = (name, mixed) => {
    const page = spec(name, mixed);
    return page ? page.title : '輸入設定';
  };

  // 項目所在的頁面。key 不是字串（舊資料或壞掉的項目）時照樣回一個頁面，不丟錯。
  function pageOfKey(key) {
    const name = typeof key === 'string' ? key : '';
    if (OUTPUT_KEYS.includes(name)) return 'output';
    if (name.startsWith('fuzzy-')) return 'candidates';
    for (const [page, entry] of Object.entries(INPUT_PAGES)) {
      if (Array.isArray(entry.keys) && entry.keys.includes(name)) return page;
    }
    return 'candidates';
  }

  // 項目在頁面中的位置；不在清單裡（或 keys 不是陣列）排到最後。keys 缺少時回 0 筆，不丟錯。
  function order(keys, key) {
    const list = Array.isArray(keys) ? keys : [];
    const at = list.indexOf(key);
    return at < 0 ? list.length : at;
  }

  window.ZcPages = { INPUT_PAGES, OUTPUT_KEYS, spec, title, pageOfKey, order };
})();
