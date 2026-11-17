package main

import (
	"testing"
)

// #118／P2-42：輸出變體經 settings --json 給設定程式；引擎載入的簡體值不同時標示待套用。
func TestOutputVariantContractReportsPendingAndUnknownRuntime(t *testing.T) {
	base := t.TempDir()
	writeSettings(t, base, `{"output_simplified":true}`)
	for _, tc := range []struct{ runtime, want string }{{"true", "simplified"}, {"false", "traditional"}, {"", ""}} {
		v, ok := runningValues(map[string]string{"output_simplified": tc.runtime})["output-variant"]
		if v != tc.want || ok != (tc.runtime != "") {
			t.Fatalf("runtime %q: %q %v", tc.runtime, v, ok)
		}
		doc := buildSettingsDoc(base, modelStatusDial(map[string]string{"output_simplified": tc.runtime}, true))
		it := item(t, doc, "output-variant")
		if it.Value != "simplified" || it.Pending != (tc.runtime == "false") || doc.PendingRestart != it.Pending || (it.Running != nil) != ok {
			t.Fatalf("pending contract: %+v", it)
		}
	}
	if o, err := parseSetArgs([]string{"output-simplified", "on", "-apply", "-json"}); err != nil || len(o.pairs) != 1 || !o.apply {
		t.Fatalf("%+v %v", o, err)
	}
}
