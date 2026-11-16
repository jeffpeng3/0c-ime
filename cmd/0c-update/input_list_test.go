package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// #191：加入語言清單用的識別必須是 DLL 註冊的那一組（改了 CLSID 等於換一個輸入法）。
func TestInputTipMatchesTSFRegistration(t *testing.T) {
	b, err := os.ReadFile("../../platforms/windows/tsf/src/guids.rs")
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	guid := func(name string) string {
		m := regexp.MustCompile(`pub const ` + name + `: GUID = GUID::from_u128\(0x([0-9a-f]{8})_([0-9a-f]{4})_([0-9a-f]{4})_([0-9a-f]{4})_([0-9a-f]{12})\);`).FindStringSubmatch(src)
		if m == nil {
			t.Fatalf("guids.rs 沒有 %s", name)
		}
		return strings.ToUpper(fmt.Sprintf("{%s-%s-%s-%s-%s}", m[1], m[2], m[3], m[4], m[5]))
	}
	lang := func(name string) string {
		m := regexp.MustCompile(`pub const ` + name + `: u16 = 0x([0-9a-fA-F]{4});`).FindStringSubmatch(src)
		if m == nil {
			t.Fatalf("guids.rs 沒有 %s", name)
		}
		return m[1]
	}
	clsid := guid("CLSID_ZUIN")
	for _, c := range []struct {
		lang, profile, tip string
	}{
		{lang("LANGID"), guid("GUID_PROFILE"), inputTipZh},
		{lang("LANGID_JA"), guid("GUID_PROFILE_JA"), inputTipJa},
	} {
		if want := c.lang + ":" + clsid + c.profile; c.tip != want {
			t.Fatalf("%s 對應的 input tip = %s，DLL 註冊的是 %s", c.lang, c.tip, want)
		}
	}
	if inputTip != inputTipZh {
		t.Fatalf("inputTip 應保留為 zh-TW 的識別，得到 %s", inputTip)
	}
	if len(inputTips) != 2 || inputTips[0] != inputTipZh || inputTips[1] != inputTipJa {
		t.Fatalf("inputTips 應固定為 zh-TW、ja-JP 兩個：%v", inputTips)
	}
}

// #191：第一次安裝（或從沒有這一步的舊版更新）加入一次；之後使用者自己移除的，更新不再放回去；失敗下次再試。
func TestListInputMethodOnce(t *testing.T) {
	base := t.TempDir()
	yes := func() bool { return true }
	var calls []string
	fail := errors.New("input.dll 失敗")
	add := func(tip string) error {
		calls = append(calls, tip)
		if len(calls) == 1 {
			return fail
		}
		return nil
	}
	if err := listInputMethodOnce(base, yes, add); !errors.Is(err, fail) {
		t.Fatalf("第一次失敗應回報錯誤，得到 %v", err)
	}
	if _, err := os.Stat(inputListMarker(base)); !os.IsNotExist(err) {
		t.Fatal("加入失敗不能留下記號，否則之後不會再試")
	}
	if err := listInputMethodOnce(base, yes, add); err != nil || len(calls) != 3 {
		t.Fatalf("下一次安裝要重試兩個語言：err=%v calls=%v", err, calls)
	}
	want := strings.Join(inputTips, "\n") + "\n"
	if b, err := os.ReadFile(inputListMarker(base)); err != nil || string(b) != want {
		t.Fatalf("記號內容不對：%q %v，want %q", b, err, want)
	}
	for i := 0; i < 3; i++ { // 之後的更新
		if err := listInputMethodOnce(base, yes, add); err != nil {
			t.Fatal(err)
		}
	}
	if len(calls) != 3 {
		t.Fatalf("加入過之後不能再改使用者的清單，呼叫了 %v", calls)
	}
}

// T28：標記分語言。一個語言加入失敗時，已加入的另一個要記住，下次只重試缺的那一個。
func TestListInputMethodOnceTracksEachLanguage(t *testing.T) {
	base := t.TempDir()
	yes := func() bool { return true }
	jaErr := errors.New("ja 失敗")
	add := func(tip string) error {
		if tip == inputTipJa {
			return jaErr
		}
		return nil
	}
	if err := listInputMethodOnce(base, yes, add); !errors.Is(err, jaErr) {
		t.Fatalf("ja 失敗應回報，得到 %v", err)
	}
	recorded, err := readInputListMarker(base)
	if err != nil {
		t.Fatal(err)
	}
	if !recorded[inputTipZh] || recorded[inputTipJa] {
		t.Fatalf("只應記住已加入的 zh：%v", recorded)
	}
	var retried []string
	if err := listInputMethodOnce(base, yes, func(tip string) error {
		retried = append(retried, tip)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(retried) != 1 || retried[0] != inputTipJa {
		t.Fatalf("下次只重試 ja，得到 %v", retried)
	}
	recorded, err = readInputListMarker(base)
	if err != nil || !recorded[inputTipZh] || !recorded[inputTipJa] {
		t.Fatalf("兩個語言都要記住：%v %v", recorded, err)
	}
}

// T28：舊版留下只含 zh 的記號時，更新要補上 ja；使用者自己移除的 ja 之後不會被加回。
func TestListInputMethodOnceUpgradesLegacyMarker(t *testing.T) {
	base := t.TempDir()
	marker := inputListMarker(base)
	if err := os.MkdirAll(filepath.Dir(marker), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(marker, []byte(inputTipZh+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var added []string
	add := func(tip string) error { added = append(added, tip); return nil }
	if err := listInputMethodOnce(base, func() bool { return true }, add); err != nil {
		t.Fatal(err)
	}
	if len(added) != 1 || added[0] != inputTipJa {
		t.Fatalf("舊記號只缺 ja，得到 %v", added)
	}
	if err := listInputMethodOnce(base, func() bool { return true }, add); err != nil {
		t.Fatal(err)
	}
	if len(added) != 1 {
		t.Fatalf("之後的更新不能再加回使用者移除的語言：%v", added)
	}
}

// T28：回復舊版只留 zh 記號；解除安裝清掉記號，重新安裝才會再加兩個語言。
func TestInputListMarkerForgetAndClear(t *testing.T) {
	base := t.TempDir()
	if err := writeInputListMarker(base, map[string]bool{inputTipZh: true, inputTipJa: true}); err != nil {
		t.Fatal(err)
	}
	if err := forgetInputListTips(base, inputTipJa); err != nil {
		t.Fatal(err)
	}
	recorded, err := readInputListMarker(base)
	if err != nil || !recorded[inputTipZh] || recorded[inputTipJa] {
		t.Fatalf("回復舊版後只留 zh：%v %v", recorded, err)
	}
	if err := clearInputListMarker(base); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(inputListMarker(base)); !os.IsNotExist(err) {
		t.Fatal("解除安裝要清掉記號")
	}
	if err := clearInputListMarker(base); err != nil {
		t.Fatalf("重複清除不算錯：%v", err)
	}
}

// #191 發版前 code review：已加入過的帳號先看記號就結束，不查身分（token、帳號名稱、工作階段的使用者）。
// 登入工作與每日檢查每次都會走到這裡，網域電腦連不到網域控制站時查帳號可能卡住，拖慢引擎啟動。
// 不是工作階段的使用者時不加入、不留記號，那個帳號之後自己安裝或登入時仍會加入。
func TestListInputMethodOnceChecksMarkerBeforeIdentity(t *testing.T) {
	base := t.TempDir()
	asked, added := 0, 0
	add := func(string) error { added++; return nil }
	notOwner := func() bool { asked++; return false }
	if err := listInputMethodOnce(base, notOwner, add); err != nil || asked != 1 || added != 0 {
		t.Fatalf("不是工作階段的使用者：err=%v asked=%d added=%d", err, asked, added)
	}
	if _, err := os.Stat(inputListMarker(base)); !os.IsNotExist(err) {
		t.Fatal("沒有加入不能留下記號")
	}
	owner := func() bool { asked++; return true }
	if err := listInputMethodOnce(base, owner, add); err != nil || asked != 2 || added != len(inputTips) {
		t.Fatalf("工作階段的使用者：err=%v asked=%d added=%d", err, asked, added)
	}
	for i := 0; i < 3; i++ { // 之後的登入與每日檢查
		if err := listInputMethodOnce(base, owner, add); err != nil {
			t.Fatal(err)
		}
	}
	if asked != 2 || added != len(inputTips) {
		t.Fatalf("有記號之後不能再查身分：asked=%d added=%d", asked, added)
	}
}

// #191 code review：在 UAC 對話框輸入另一個系統管理員帳號（over-the-shoulder）時，提權的安裝程式是那個管理員，
// 不能替它把靈犀注音加進清單（加進去的是管理員的 HKCU，桌面使用者的清單不變）。
// 發版前 code review：以 SID 比對，不比「網域\帳號」字串；Azure AD 與 Microsoft 帳號的電腦上，token 查到的名稱與
// 工作階段的使用者名稱寫法可能不同，同一位使用者自己提權也會判成不同人，清單就永遠不加。
// 第三輪：工作階段使用者的 SID 改取自桌面 shell 的擁有者，不再由名稱查（Azure AD 帳號的名稱常查不到 SID，原本就退回比名稱）；
// 一般權限的程序就是它自己的帳號，不查身分，查不到 SID、名稱寫法不同也加入。
func TestOwnsDesktop(t *testing.T) {
	const amy, admin = "S-1-5-21-1-2-3-1001", "S-1-5-21-1-2-3-500"
	const aad = "S-1-12-1-111-222-333-444"
	for _, c := range []struct {
		name             string
		process, session account
		err              error
		elevated, want   bool
	}{
		{"同一位使用者以系統管理員權限安裝", account{amy, `PC\amy`}, account{amy, `PC\amy`}, nil, true, true},
		{"登入工作與每日檢查（一般權限）", account{amy, `PC\amy`}, account{amy, `PC\amy`}, nil, false, true},
		{"Azure AD 帳號：名稱寫法不同、SID 相同", account{aad, `AzureAD\AmyLin`}, account{aad, `AzureAD\amy.lin`}, nil, true, true},
		{"Microsoft 帳號：名稱寫法不同、SID 相同", account{amy, `PC\amy`}, account{amy, `PC\amy_000`}, nil, true, true},
		{"SID 大小寫不同", account{"s-1-5-21-1-2-3-1001", ""}, account{amy, `PC\amy`}, nil, true, true},
		{"以另一個管理員帳號確認 UAC", account{admin, `PC\admin`}, account{amy, `PC\amy`}, nil, true, false},
		{"名稱相同但 SID 不同（不同電腦或網域的同名帳號）", account{admin, `PC\amy`}, account{amy, `PC\amy`}, nil, true, false},
		{"另一個帳號的一般權限程序（runas）：加進的是它自己的清單", account{admin, `PC\bob`}, account{amy, `PC\amy`}, nil, false, true},
		{"Azure AD 的登入工作：工作階段 SID 查不到、名稱寫法不同", account{aad, `AzureAD\AmyLin`}, account{"", `AzureAD\amy.lin`}, nil, false, true},
		{"Azure AD 自己提權：SID 取自桌面 shell", account{aad, `AzureAD\AmyLin`}, account{aad, `AzureAD\amy.lin`}, nil, true, true},
		{"Azure AD 帳號的電腦上以另一個管理員確認 UAC", account{admin, `PC\admin`}, account{aad, `AzureAD\amy.lin`}, nil, true, false},
		{"工作階段使用者的 SID 查不到：退回比名稱", account{amy, `PC\amy`}, account{"", `pc\Amy`}, nil, true, true},
		{"工作階段使用者的 SID 查不到：名稱不同", account{admin, `PC\admin`}, account{"", `PC\amy`}, nil, true, false},
		{"沒有登入使用者的工作階段（服務、SSH）", account{amy, `PC\amy`}, account{}, nil, true, false},
		{"沒有登入使用者的工作階段：一般權限是自己的帳號", account{amy, `PC\amy`}, account{}, nil, false, true},
		{"查不到工作階段：系統管理員不加", account{amy, `PC\amy`}, account{}, errors.New("wts"), true, false},
		{"查不到工作階段：一般權限是自己的帳號", account{amy, `PC\amy`}, account{}, errors.New("wts"), false, true},
	} {
		if got := ownsDesktop(c.process, c.session, c.err, c.elevated); got != c.want {
			t.Errorf("%s：ownsDesktop = %v，預期 %v", c.name, got, c.want)
		}
	}
}
