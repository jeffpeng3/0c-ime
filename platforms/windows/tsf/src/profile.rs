//! Windows 語言 profile（T28）：注音與日文共用一個 TSF text service，語言由目前啟動的 profile 決定。
//!
//! Windows 會替每個程式記住上次使用的 profile，切換程式時 TSF 以 Activate 重新啟用本輸入法；
//! 外殼在這裡查 GetActiveProfile，把 profile 對應的語言交給引擎，新 session 就從該語言開始。
use windows::Win32::UI::TextServices::{
    ITfInputProcessorProfileMgr, ITfThreadMgr, GUID_TFCAT_TIP_KEYBOARD, TF_INPUTPROCESSORPROFILE,
};
use windows_core::{GUID, Interface};

use crate::guids::{
    CLSID_ZUIN, DESCRIPTION, DESCRIPTION_JA, GUID_PROFILE, GUID_PROFILE_JA, LANGID, LANGID_JA,
};

/// 引擎協定使用的語言識別（core/lang）：注音是 "zh"，日文是 "ja"。
pub const LANGUAGE_ZH: &str = "zh";
pub const LANGUAGE_JA: &str = "ja";

/// 一個 TSF 語言 profile：語言 ID、profile GUID、顯示名稱與引擎語言。
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct Profile {
    pub langid: u16,
    pub guid: GUID,
    pub description: &'static str,
    pub language: &'static str,
}

pub const PROFILE_ZH: Profile = Profile {
    langid: LANGID,
    guid: GUID_PROFILE,
    description: DESCRIPTION,
    language: LANGUAGE_ZH,
};

pub const PROFILE_JA: Profile = Profile {
    langid: LANGID_JA,
    guid: GUID_PROFILE_JA,
    description: DESCRIPTION_JA,
    language: LANGUAGE_JA,
};

/// 註冊、取消註冊與狀態查詢都走同一份清單。
pub const PROFILES: [Profile; 2] = [PROFILE_ZH, PROFILE_JA];

/// 以語言 ID 找 profile；找不到（例如 Windows 的英文鍵盤）回傳 None。
pub fn for_langid(langid: u16) -> Option<Profile> {
    PROFILES.iter().copied().find(|p| p.langid == langid)
}

/// 以引擎語言字串找 profile。
#[allow(dead_code)] // 外殼目前由 profile GUID 反查；留給設定／診斷呼叫與測試。
pub fn for_language(language: &str) -> Option<Profile> {
    PROFILES.iter().copied().find(|p| p.language == language)
}

/// 由 profile GUID 找 language ID；不是本輸入法的 profile 回傳 None。
#[allow(dead_code)] // 診斷與測試用；註冊與啟用路徑直接比對 Profile。
pub fn language_of(langid: u16, guid: &GUID) -> Option<&'static str> {
    PROFILES
        .iter()
        .find(|p| p.langid == langid && p.guid == *guid)
        .map(|p| p.language)
}

/// 目前執行緒（程式）啟動的 profile。Windows 的 per-app 記憶就在這裡：
/// 使用者用 Win＋Space 切到哪個 profile，GetActiveProfile 就回哪一個。
pub fn active_profile(tm: &ITfThreadMgr) -> Option<Profile> {
    let manager: ITfInputProcessorProfileMgr = tm.cast().ok()?;
    let mut profile = TF_INPUTPROCESSORPROFILE::default();
    unsafe {
        manager
            .GetActiveProfile(&GUID_TFCAT_TIP_KEYBOARD, &mut profile)
            .ok()?;
    }
    if profile.clsid != CLSID_ZUIN {
        return None;
    }
    for_langid(profile.langid).filter(|p| p.guid == profile.guidProfile)
}

/// 目前啟動的 profile 語言 ID（0x0404／0x0411）；不是本輸入法的 profile 或查詢失敗時回傳 None。
#[allow(dead_code)] // 外殼傳給引擎的是語言字串；留給需要原始 LANGID 的診斷與測試。
pub fn active_langid(tm: &ITfThreadMgr) -> Option<u16> {
    active_profile(tm).map(|p| p.langid)
}

/// 目前啟動的 profile 語言（"zh"／"ja"）。不是本輸入法的 profile 或查詢失敗時回傳 None，
/// 呼叫端照舊讓引擎回報開始時的模式。
pub fn active_language(tm: &ITfThreadMgr) -> Option<&'static str> {
    active_profile(tm).map(|p| p.language)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn profiles_cover_both_windows_languages() {
        assert_eq!(PROFILE_ZH.langid, 0x0404);
        assert_eq!(PROFILE_JA.langid, 0x0411);
        assert_eq!(PROFILE_ZH.language, "zh");
        assert_eq!(PROFILE_JA.language, "ja");
        assert_eq!(PROFILE_ZH.description, "靈犀注音");
        assert_eq!(PROFILE_JA.description, "靈犀日本語");
    }

    #[test]
    fn profiles_use_distinct_guids() {
        assert_ne!(PROFILE_ZH.guid, PROFILE_JA.guid);
        assert_ne!(PROFILE_ZH.guid, GUID::zeroed());
        assert_ne!(PROFILE_JA.guid, GUID::zeroed());
        assert_eq!(PROFILE_ZH.guid, GUID_PROFILE, "既有的 zh-TW GUID 不能改");
    }

    #[test]
    fn language_lookup_checks_both_langid_and_profile() {
        assert_eq!(language_of(0x0404, &GUID_PROFILE), Some("zh"));
        assert_eq!(language_of(0x0411, &GUID_PROFILE_JA), Some("ja"));
        // 語言 ID 與 profile 必須成對：拿錯的 GUID 不能當成另一個語言。
        assert_eq!(language_of(0x0404, &GUID_PROFILE_JA), None);
        assert_eq!(language_of(0x0411, &GUID_PROFILE), None);
        assert_eq!(language_of(0x0409, &GUID_PROFILE), None);
    }

    #[test]
    fn lookup_helpers_agree_with_the_registration_list() {
        for p in PROFILES {
            assert_eq!(for_langid(p.langid), Some(p));
            assert_eq!(for_language(p.language), Some(p));
        }
        assert_eq!(for_langid(0x0409), None);
        assert_eq!(for_language("en"), None);
    }
}
