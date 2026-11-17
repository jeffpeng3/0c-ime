//! 候選跟著游標所在片段（P2-41）。
//!
//! 引擎快照的組字區本來就是依路徑片段排列：一個詞、原樣英數或符號各是一個
//! `preedit` 項目。這裡只把這些項目換成 rune 範圍，找出游標所在的片段，並
//! 判斷哪些按鍵會移動片段游標。候選清單本身仍由引擎提供；這個模型不保存
//! 語言標籤，也不提供任何語言選擇，Windows 只有一個 Mixed profile。
use crate::session::Snapshot;

/// 組字區中的一個片段，位置以顯示字元（rune）計算，半開區間。
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct Span {
    pub start: usize,
    pub end: usize,
}

impl Span {
    pub fn contains(self, pos: usize) -> bool {
        self.start <= pos && pos < self.end
    }
}

/// 由快照推導出的片段游標狀態。
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct CandidateModel {
    pub segments: Vec<Span>,
    /// 游標所在片段；沒有片段時為 None。
    pub active: Option<usize>,
}

impl CandidateModel {
    pub fn from_snapshot(snapshot: &Snapshot) -> Self {
        let segments = preedit_spans(snapshot);
        let active = active_segment(&segments, snapshot.cursor_rune);
        Self { segments, active }
    }

    pub fn active_span(&self) -> Option<Span> {
        self.active.and_then(|i| self.segments.get(i)).copied()
    }

    /// 引擎候選覆蓋幾個片段。長詞詞庫 match 在清單中仍是一項，這裡回傳它
    /// 橫跨的片段數，供測試與診斷確認它沒有被拆成多個候選。
    pub fn coverage(&self, start: usize, length: usize) -> usize {
        covers_segments(&self.segments, start, length)
    }
}

/// 把快照的組字區項目換成片段範圍。空字串項目不佔位置。
pub fn preedit_spans(snapshot: &Snapshot) -> Vec<Span> {
    let mut at = 0usize;
    let mut out = Vec::with_capacity(snapshot.preedit.len());
    for segment in &snapshot.preedit {
        let n = segment.text.chars().count();
        if n == 0 {
            continue;
        }
        out.push(Span {
            start: at,
            end: at + n,
        });
        at += n;
    }
    out
}

/// 游標所在片段。游標在片段結尾時屬於下一個片段；在整段結尾時仍停在最後
/// 一個片段，和 `core/session` 的 `ActiveSegment` 相同。
pub fn active_segment(segments: &[Span], cursor: usize) -> Option<usize> {
    if segments.is_empty() {
        return None;
    }
    if let Some(i) = segments.iter().position(|s| s.contains(cursor)) {
        return Some(i);
    }
    let end = segments.iter().map(|s| s.end).max().unwrap_or(0);
    (cursor >= end).then(|| segments.len() - 1)
}

/// 一段候選範圍 [start, start+length) 覆蓋幾個片段。長詞是單一候選，但可能
/// 蓋過多個輸入片段；呼叫端不得因此把它拆成多列。
pub fn covers_segments(segments: &[Span], start: usize, length: usize) -> usize {
    let end = start.saturating_add(length);
    if length == 0 || end <= start {
        return 0;
    }
    segments
        .iter()
        .filter(|span| span.start < end && start < span.end)
        .count()
}

/// 會移動組字游標的按鍵。候選窗開著時左右鍵是翻頁，不會走到這裡。
pub fn moves_cursor(key: &str) -> bool {
    matches!(key, "Left" | "Right" | "Home" | "End")
}

/// 這一鍵之後是否要為新的片段自動重開候選。
///
/// 只在中文組字、按鍵確實移動游標、而且這次快照沒有候選時才成立：候選窗
/// 開著時的方向鍵由引擎處理翻頁，不會移動片段。
pub fn follows_cursor(snapshot: &Snapshot, key: &str) -> bool {
    if !snapshot.ok || !snapshot.handled || snapshot.commit != "" {
        return false;
    }
    if snapshot.english || snapshot.japanese || !snapshot.composing() {
        return false;
    }
    // 未完成的注音／かな還在組字區時，核心不會移動游標；不要因此跳出候選。
    if snapshot
        .preedit
        .iter()
        .any(|segment| segment.style == "pending")
    {
        return false;
    }
    if !snapshot.candidates.is_empty() || !moves_cursor(key) {
        return false;
    }
    CandidateModel::from_snapshot(snapshot).active.is_some()
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::session::Segment;

    fn snapshot(preedit: &[(&str, &str)], cursor: usize) -> Snapshot {
        Snapshot {
            ok: true,
            handled: true,
            preedit: preedit
                .iter()
                .map(|(text, style)| Segment {
                    text: (*text).to_string(),
                    style: (*style).to_string(),
                })
                .collect(),
            cursor_rune: cursor,
            ..Default::default()
        }
    }

    #[test]
    fn active_segment_follows_the_composition_cursor() {
        let model = CandidateModel::from_snapshot(&snapshot(
            &[("我", "normal"), ("風景", "normal"), ("abc", "normal")],
            0,
        ));
        assert_eq!(
            model.segments,
            vec![
                Span { start: 0, end: 1 },
                Span { start: 1, end: 3 },
                Span { start: 3, end: 6 }
            ]
        );
        assert_eq!(model.active, Some(0), "游標在開頭");
        assert_eq!(model.active_span(), Some(Span { start: 0, end: 1 }));
        for (cursor, want) in [(1, 0), (2, 1), (3, 1), (4, 2), (6, 2)] {
            let model = CandidateModel::from_snapshot(&snapshot(
                &[("我", "normal"), ("風景", "normal"), ("abc", "normal")],
                cursor,
            ));
            assert_eq!(model.active, Some(want), "游標 {cursor}");
        }
    }

    #[test]
    fn long_word_is_one_segment_and_one_candidate_range() {
        // 詞庫長詞 match 在組字區就是一個片段；候選範圍也蓋住整段，不會拆成
        // 「風」「景」兩個候選。
        let model = CandidateModel::from_snapshot(&snapshot(&[("風景", "normal")], 1));
        assert_eq!(model.segments, vec![Span { start: 0, end: 2 }]);
        assert_eq!(model.active, Some(0));
        assert_eq!(model.coverage(0, 2), 1, "長詞候選是一項，不是兩個片段候選");
        assert_eq!(model.coverage(0, 1), 1);
        // 若引擎在尚未合併的路徑上提供長詞候選，它仍然是一項，但覆蓋多個片段。
        let split =
            CandidateModel::from_snapshot(&snapshot(&[("風", "normal"), ("景", "normal")], 0));
        assert_eq!(split.coverage(0, 2), 2);
        assert_eq!(split.coverage(0, 1), 1);
    }

    #[test]
    fn cursor_movement_reopens_candidates_only_when_none_are_shown() {
        let mut s = snapshot(&[("我", "normal"), ("風景", "normal")], 3);
        assert!(follows_cursor(&s, "Left"));
        assert!(follows_cursor(&s, "Home"));
        assert!(follows_cursor(&s, "End"));
        assert!(!follows_cursor(&s, "Down"), "下鍵由引擎開候選，不再重送");
        assert!(!follows_cursor(&s, "a"), "打字不重開候選");
        s.candidates = vec![(1, "風景".into())];
        assert!(!follows_cursor(&s, "Left"), "候選開著時左右鍵是翻頁");
        s.candidates.clear();
        s.english = true;
        assert!(!follows_cursor(&s, "Left"), "英文模式不自動開中文候選");
        s.english = false;
        s.japanese = true;
        assert!(!follows_cursor(&s, "Left"), "日文模式由文節候選處理");
        s.japanese = false;
        s.commit = "風景".into();
        assert!(!follows_cursor(&s, "Left"), "已提交就不再開候選");
        let pending = snapshot(&[("風景", "normal"), ("ㄒ", "pending")], 3);
        assert!(!follows_cursor(&pending, "Left"), "未完成的注音不移動游標");
    }

    #[test]
    fn empty_preedit_has_no_active_segment() {
        let model = CandidateModel::from_snapshot(&snapshot(&[], 0));
        assert!(model.segments.is_empty());
        assert_eq!(model.active, None);
        assert_eq!(model.coverage(0, 0), 0);
    }
}
