//! Windows 語言 profile（P2-40）：整個輸入法只有一個 Mixed profile。
//!
//! 引擎 API 是 profile-free：open 不帶語言，同一個 session 直接接受注音、日文與英文。
//! TSF 仍然需要一個語言 profile 才能出現在 Windows 的輸入法清單；這裡沿用既有的
//! zh-TW 語言 ID 與 `GUID_PROFILE`，只把顯示名稱改成 Mixed。升級時不必重新加入輸入法，
//! 使用者的 per-app 記憶也留著。
//!
//! 舊版（T28）另外註冊了 ja-JP 的「靈犀日本語」。那個 profile 不再建立，只留在
//! [`LEGACY_PROFILES`] 供註冊與解除安裝時清除。
use windows_core::GUID;

use crate::guids::{DESCRIPTION, GUID_PROFILE, LANGID};

/// 一個 TSF 語言 profile：Windows 需要的語言 ID、profile GUID 與顯示名稱。
///
/// 沒有語言欄位：profile 只是 Windows 的註冊身分，不是引擎的輸入語言。
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct Profile {
    pub langid: u16,
    pub guid: GUID,
    pub description: &'static str,
}

pub const PROFILE_MIXED: Profile = Profile {
    langid: LANGID,
    guid: GUID_PROFILE,
    description: DESCRIPTION,
};

/// 註冊、取消註冊與狀態查詢都走同一份清單；Mixed 是唯一的一個。
pub const PROFILES: [Profile; 1] = [PROFILE_MIXED];

/// 舊版留下的 ja-JP profile（語言 ID 與 GUID）。只為了移除，不再註冊。
pub const LEGACY_PROFILES: [(u16, GUID); 1] = [(crate::guids::LANGID_JA, crate::guids::GUID_PROFILE_JA)];

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn exactly_one_profile_is_registered_and_it_is_mixed() {
        assert_eq!(PROFILES.len(), 1);
        assert_eq!(PROFILES[0], PROFILE_MIXED);
        assert_eq!(PROFILE_MIXED.description, "Mixed");
        assert_eq!(PROFILE_MIXED.langid, 0x0404);
        assert_eq!(PROFILE_MIXED.guid, GUID_PROFILE);
        assert_ne!(PROFILE_MIXED.guid, GUID::zeroed());
    }

    #[test]
    fn no_language_specific_profiles_are_registered() {
        // English-only 已取消；日文也不再是另一個 profile。
        for p in PROFILES {
            assert!(!p.description.to_ascii_lowercase().contains("english"));
            assert!(!p.description.contains("日本語"));
        }
        assert!(LEGACY_PROFILES.iter().all(|(langid, guid)| *langid != LANGID || *guid != GUID_PROFILE));
    }

    #[test]
    fn legacy_japanese_profile_is_only_removed() {
        assert_eq!(LEGACY_PROFILES, [(0x0411, crate::guids::GUID_PROFILE_JA)]);
        assert!(PROFILES.iter().all(|p| p.guid != crate::guids::GUID_PROFILE_JA));
    }
}
