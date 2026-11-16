package main

import (
	"os"
	"path/filepath"
	"strings"
)

// 靈犀在 Windows 語言清單裡的識別：語言（zh-TW／ja-JP）、TSF 的 CLSID 與語言 profile，
// 與 platforms/windows/tsf/src/guids.rs 的 LANGID、LANGID_JA、CLSID_ZUIN、GUID_PROFILE、GUID_PROFILE_JA 相同。
const (
	inputTipZh = "0404:{138C35D0-7F08-4CEF-BA90-C34C16301528}{EF62F743-9855-4C94-A658-1F0F98B7B840}"
	inputTipJa = "0411:{138C35D0-7F08-4CEF-BA90-C34C16301528}{3D6A8B21-5C47-4F9E-B0D2-71E4A9C8F653}"

	// inputTip 是注音的識別，保留給既有呼叫端與測試；新程式碼走 inputTips。
	inputTip = inputTipZh
)

// inputTips 是安裝時加入使用者語言清單的兩個 profile，順序固定（zh-TW 在前）。
var inputTips = []string{inputTipZh, inputTipJa}

// inputListMarker 記下已替這個帳號把哪些語言加入過清單（#191、T28）。內容一行一個 tip，標記分語言：
// 其中一個加入失敗時另一個仍然記住，下一次只重試缺的那一個。
func inputListMarker(base string) string { return filepath.Join(base, "update", "input-list") }

// readInputListMarker 讀回已記錄的 tip；檔案不存在時回傳空集合。
func readInputListMarker(base string) (map[string]bool, error) {
	recorded := map[string]bool{}
	b, err := os.ReadFile(inputListMarker(base))
	if os.IsNotExist(err) {
		return recorded, nil
	}
	if err != nil {
		return nil, err
	}
	for _, line := range strings.Split(string(b), "\n") {
		if tip := strings.TrimSpace(line); tip != "" {
			recorded[tip] = true
		}
	}
	return recorded, nil
}

// writeInputListMarker 依 inputTips 的固定順序重寫記號，內容穩定、方便測試與排查。
func writeInputListMarker(base string, recorded map[string]bool) error {
	marker := inputListMarker(base)
	var lines []string
	for _, tip := range inputTips {
		if recorded[tip] {
			lines = append(lines, tip)
		}
	}
	if len(lines) == 0 {
		if err := os.Remove(marker); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(marker), 0o700); err != nil {
		return err
	}
	return os.WriteFile(marker, []byte(strings.Join(lines, "\n")+"\n"), 0o600)
}

// listInputMethodOnce 第一次安裝（或從還沒有這一步的舊版更新）時把兩個語言加入這個帳號的輸入法清單，
// 之後不再加：使用者自己從清單移除的，更新不會再放回去。add 失敗的語言不留記號，下次安裝或更新只重試它。
// 先看記號再查身分（owns，見 ownsDesktop）：登入工作與每日檢查每次都會走到這裡，已加入過的帳號不必查 token 與
// 帳號名稱（網域電腦連不到網域控制站時查帳號可能要等很久，會拖慢引擎啟動）。owns 為 false 時不加入、不留記號。
func listInputMethodOnce(base string, owns func() bool, add func(tip string) error) error {
	recorded, err := readInputListMarker(base)
	if err != nil {
		return err
	}
	var missing []string
	for _, tip := range inputTips {
		if !recorded[tip] {
			missing = append(missing, tip)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	if !owns() {
		return nil
	}
	for _, tip := range missing {
		if err := add(tip); err != nil {
			return err
		}
		recorded[tip] = true
		// 每加入一個就寫一次：另一個失敗時，已加入的不用重做。
		if err := writeInputListMarker(base, recorded); err != nil {
			return err
		}
	}
	return nil
}

// forgetInputListTips 從記號移除指定的語言（回復到不支援它們的舊版時用）；其他語言保留。
func forgetInputListTips(base string, tips ...string) error {
	recorded, err := readInputListMarker(base)
	if err != nil {
		return err
	}
	for _, tip := range tips {
		delete(recorded, tip)
	}
	return writeInputListMarker(base, recorded)
}

// clearInputListMarker 在解除安裝時清掉記號，重新安裝才會再次加入兩個語言。
func clearInputListMarker(base string) error {
	err := os.Remove(inputListMarker(base))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// account 是一個 Windows 帳號：SID 與「網域\帳號」名稱，查不到的留空字串。
type account struct{ sid, name string }

// ownsDesktop 判斷這個程序能不能替它的帳號加入輸入法清單（#191）：InstallLayoutOrTip 寫的是這個程序的 HKCU，記號寫在它的
// LOCALAPPDATA，只有這個程序就是工作階段登入的那位使用者時才加入。在 UAC 對話框輸入另一個系統管理員帳號
// （over-the-shoulder）時，提權的安裝程式是那個管理員：加進去的會是管理員的清單、桌面使用者的清單不變，還可能在桌面使用者的
// 工作階段裡切換鍵盤。這時不加入，只提示。不會有之後補上的時候：提權的安裝程式本來就把整套安裝（%LOCALAPPDATA%\Zuin 的
// 程式與收據、自動更新與登入工作）都寫在那個管理員帳號，桌面使用者沒有登入工作；要以桌面使用者自己的帳號確認系統管理員權限
// 重新安裝才會裝到它那邊（並加入它的清單）。安裝程式在開始安裝前就擋下這種情形（#199，見 installsForOtherAccount），
// 這裡只剩查不到桌面 shell、以名稱比對才認出不同帳號的少數情形。
// 一般權限的程序（登入工作、每日檢查，以 runas 執行的也一樣）一律加入、不查身分：InstallLayoutOrTip 與記號都落在它自己的帳號，
// 只有提權時 UAC 可能換成另一個帳號。
// 提權時以 SID 比對：工作階段使用者的 SID 取自桌面 shell 的擁有者（見 sessionAccount），不靠名稱；Azure AD 與 Microsoft 帳號的
// 電腦上，token 查到的名稱與工作階段的使用者名稱寫法可能不同。拿不到工作階段使用者的 SID 時才退回比名稱。session 兩者皆空是
// 沒有登入使用者（服務、SSH），不加入；查不到工作階段（err）時，提權的程序不加入。
func ownsDesktop(process, session account, err error, elevated bool) bool {
	if !elevated {
		return true
	}
	if err != nil {
		return false
	}
	if process.sid != "" && session.sid != "" {
		return strings.EqualFold(process.sid, session.sid)
	}
	return session.name != "" && strings.EqualFold(process.name, session.name)
}
