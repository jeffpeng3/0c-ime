// 按鍵名稱的轉換中不需要 Fcitx 的部分。
#pragma once

#include <cstdint>
#include <string>

namespace zuin {

// vocabKey 把按著 Shift 的 ←→、Delete、Backspace 換成 0.2.0 的新鍵名（#123）：組字中 Shift＋←／→ 標示要加入詞庫的範圍，
// 候選窗的 Shift＋Delete／⌫ 刪除自訂詞或忘記學到的詞。key 是不看 Shift 的原本鍵名（toKey 的結果）；
// shiftOnly 是只按著 Shift（沒有 Ctrl／Alt／Super）；supported() 回報引擎是否認得新鍵名（open 回覆的 vocab_keys），
// 只在真的要換名時才問，其他按鍵不多一次連線。
// 舊引擎不認得新鍵名，沒回報支援時照舊送原本的鍵；新引擎在沒有對應功能時把新鍵名當原本的鍵處理，
// 所以外殼不必追蹤組字或候選窗的狀態。
template <class Supported>
std::string vocabKey(const std::string &key, bool shiftOnly, Supported &&supported) {
    if (!shiftOnly) { return key; }
    const char *renamed = key == "Left" ? "ShiftLeft" : key == "Right" ? "ShiftRight"
                        : key == "Delete" ? "ShiftDelete" : key == "Backspace" ? "ShiftBackspace" : nullptr;
    if (!renamed || !supported()) { return key; }
    return renamed;
}

template <class Supported>
std::string styleKey(const std::string &key, bool shift, Supported &&supported) {
    return key == " " && shift && supported() ? "ShiftSpace" : key;
}

// foldCaps：中文模式不理 Caps Lock（#168，同常見注音輸入法與 ibus-chewing），ASCII 字母換回沒開 Caps Lock 時的大小寫：
// 沒按 Shift 是小寫注音鍵，Shift＋字母照舊是大寫。caps、shift 要看 rawKey（Fcitx 正規化按鍵時拿掉 CapsLock 與字母的 Shift）。
// 英文模式不呼叫，照 Caps Lock 打大寫。
inline uint32_t foldCaps(uint32_t sym, bool caps, bool shift) {
    bool lower = sym >= 'a' && sym <= 'z', upper = sym >= 'A' && sym <= 'Z';
    if (!caps || !(lower || upper)) { return sym; }
    return shift ? (sym & ~0x20u) : (sym | 0x20u);
}

inline uint32_t foldStyleCaps(uint32_t sym, bool caps, bool shift, bool english, bool lower) {
    return !english || lower ? foldCaps(sym, caps, shift) : sym;
}


// ctrlPunctKey：只按著 Ctrl（可以加 Shift，沒有 Alt／Super／Hyper）的 , . ' ; ? ! 與 : ( )（#231）換成協定的 Ctrl＋標點鍵名（#168）。
// unicode 是已經套用 Shift 的字元（Ctrl+Shift+/ 是 ?），其他鍵回傳空字串。引擎認不認得見 ctrlPunctKnown。
inline std::string ctrlPunctKey(uint32_t unicode, bool ctrl, bool otherModifier) {
    if (!ctrl || otherModifier) { return ""; }
    switch (unicode) {
    case ',': case '.': case '\'': case ';': case '?': case '!': case ':': case '(': case ')':
        return std::string("Ctrl+") + static_cast<char>(unicode);
    default:
        return "";
    }
}

// ctrlPunctKnown：引擎認得這個 Ctrl＋標點鍵名。最早的六個鍵（#168）只要引擎回報 ctrl_punctuation；Ctrl+: ( )（#231）
// 要引擎的 ctrl_punctuation_keys（engineKeys）列了那個字元，舊引擎沒有這一項，照舊交給應用程式。
inline bool ctrlPunctKnown(const std::string &key, const std::string &engineKeys) {
    if (key.size() != 6 || key.rfind("Ctrl+", 0) != 0) { return false; }
    const char c = key[5];
    return std::string(",.';?!").find(c) != std::string::npos || engineKeys.find(c) != std::string::npos;
}

// simplifiedHotkey：切換「輸出簡體字」的 Ctrl＋Shift＋F11（#192，settings.SimplifiedHotkeyLabel）。只按著 Ctrl 與 Shift，
// 有 Alt／Super／Hyper 就不是；Caps Lock、Num Lock 不影響。sym 是 X keysym（F11 是 0xffc8）。
// 要不要攔由呼叫端看引擎回報的 simplified_hotkey。
constexpr uint32_t keyF11 = 0xffc8;
inline bool simplifiedHotkey(uint32_t sym, bool ctrl, bool shift, bool otherModifier) {
    return sym == keyF11 && ctrl && shift && !otherModifier;
}

// 日文具名鍵（T15）的 X keysym。使用數字而不是 FcitxKey_*，讓按鍵轉換可以脫離 Fcitx 標頭測試；
// 協定鍵名照 core/proto（Convert、NonConvert、F6–F10），外殼不自創。
constexpr uint32_t keyMuhenkan = 0xff22; // 無変換
constexpr uint32_t keyHenkan = 0xff23;   // 変換
constexpr uint32_t keyF6 = 0xffc3;       // ひらがな
constexpr uint32_t keyF7 = 0xffc4;       // カタカナ
constexpr uint32_t keyF8 = 0xffc5;       // 半角カナ
constexpr uint32_t keyF9 = 0xffc6;       // 全角英數
constexpr uint32_t keyF10 = 0xffc7;      // 半角英數

// japaneseKey：把實體変換／無変換鍵與 F6–F10 換成協定鍵名（T23）。supported 要同時是引擎回報 japanese 與
// convert_keys；舊引擎沒有這兩項，按鍵照舊交還應用程式。按著 Shift 的 F 鍵與変換鍵是應用程式快捷鍵，不接手；
// Ctrl／Alt／Super 由呼叫端先排除。其他鍵回傳空字串。
inline std::string japaneseKey(uint32_t sym, bool shift, bool supported) {
    if (!supported || shift) { return ""; }
    switch (sym) {
    case keyHenkan: return "Convert";
    case keyMuhenkan: return "NonConvert";
    case keyF6: return "F6";
    case keyF7: return "F7";
    case keyF8: return "F8";
    case keyF9: return "F9";
    case keyF10: return "F10";
    default: return "";
    }
}

} // namespace zuin
