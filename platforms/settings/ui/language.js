// T27／P2-42：語言選項與日文選項的畫面邏輯。
//
// 設定程式只讀 0c-update settings -json 的快照：日文選項在 profiles.language（value 是目前設定），
// 其餘設定項目的 item.languages 是這一項適用的語言（沒有表示不分語言），適合日文的功能另有
// item.capabilities 能力旗標。P2-42 的 Mixed profile 直接混合注音、日文與英文，主要語言是
// items 裡的 primary-language；這裡只保留舊契約的讀法與日文能力旗標，Mixed 的畫面切換在 mixed.js。
(function () {
  'use strict';

  const LANGUAGES = [
    { id: 'zh', label: '注音' },
    { id: 'ja', label: '日文' },
  ];

  // 與 core/proto 的 Cap* 相同：設定程式顯示這些旗標，實際能不能用由引擎的 health／open 回報。
  const CAPABILITIES = [
    { id: 'japanese', label: '日文輸入' },
    { id: 'convert_keys', label: '変換鍵（変換／無変換、F6–F10）' },
    { id: 'clause_display', label: '文節顯示' },
    { id: 'live_conversion', label: 'ライブ変換' },
  ];

  // P2-42 起 Mixed profile 不再有 language／language-memory／language-hotkey；保留空清單
  // 讓舊版 0c-update 的契約與舊測試仍可讀。日文選項照舊。
  const LANGUAGE_KEYS = [];
  const JAPANESE_KEYS = ['ja-input-method', 'ja-live-conversion', 'ja-symbols', 'ja-learning', 'ja-model'];
  const LANGUAGE_PAGE_KEYS = LANGUAGE_KEYS.concat(JAPANESE_KEYS);
  // 舊契約（T27）的語言鍵：只在讀舊版 0c-update 的快照時使用，不再顯示。
  const LEGACY_LANGUAGE_KEYS = ['language', 'language-memory', 'language-hotkey'];

  // 語言選項由 0c-update 的 settings 契約放在 profiles.language（ProfileView.Language）；
  // 舊版 0c-update 沒有這個欄位，若哪天改回 items 也照樣讀得到。
  const languageItems = (settings) => {
    if (settings && settings.profiles && Array.isArray(settings.profiles.language)) return settings.profiles.language;
    if (settings && Array.isArray(settings.items)) {
      return settings.items.filter((item) => LANGUAGE_PAGE_KEYS.includes(item.key) || LEGACY_LANGUAGE_KEYS.includes(item.key));
    }
    return [];
  };

  const itemOf = (list, key) => (Array.isArray(list) ? list.find((item) => item.key === key) : null);

  // 目前的主要語言；新契約讀 primary-language，舊契約或舊版 0c-update 仍讀 language。
  // 還沒讀到或讀不懂時一律當注音，舊版行為不變。
  const activeLanguage = (settings) => {
    const legacy = itemOf(languageItems(settings), 'language');
    if (legacy && legacy.value === 'ja') return 'ja';
    const primary = itemOf(settings && settings.items, 'primary-language');
    if (primary && primary.value === 'ja') return 'ja';
    return 'zh';
  };

  const languageLabel = (id) => (LANGUAGES.find((language) => language.id === id) || LANGUAGES[0]).label;

  // 這一項在這個語言模式要不要出現。languages 空陣列或缺少時表示通用。
  const applies = (item, language) => {
    const languages = item && item.languages;
    return !Array.isArray(languages) || languages.length === 0 || languages.includes(language);
  };

  const visible = (items, language) => (items || []).filter((item) => applies(item, language));

  const isLanguagePageItem = (item) => !!item && LANGUAGE_PAGE_KEYS.includes(item.key);

  window.ZcLanguage = {
    LANGUAGES, CAPABILITIES, LANGUAGE_KEYS, JAPANESE_KEYS, LANGUAGE_PAGE_KEYS,
    languageItems, activeLanguage, languageLabel, applies, visible, isLanguagePageItem,
  };
})();
