package input

// NeutralLast reports whether the sequence worker adds the neutral-tone branch
// for the last syllable. It is a direct port of the decision in
// sequence_runtime.Runtime.score, shared by the native backend and its tests.
//
// expand > 0 (manifest neutral_expand_penalty) matches the last syllable's
// base against tolerant; otherwise the exact syllable must be listed.
func NeutralLast(mode, last string, expand bool, tolerant []string) bool {
	if mode != "" || last == "" {
		return false
	}
	runes := []rune(last)
	lastRune := runes[len(runes)-1]
	base := last
	if _, ok := toneMarks[lastRune]; ok {
		base = string(runes[:len(runes)-1])
	}
	if expand {
		if lastRune == '˙' {
			return false
		}
		for _, y := range tolerant {
			if baseOf(y) == base {
				return true
			}
		}
		return false
	}
	for _, y := range tolerant {
		if y == last {
			return true
		}
	}
	return false
}

func baseOf(y string) string {
	runes := []rune(y)
	if len(runes) == 0 {
		return y
	}
	if _, ok := toneMarks[runes[len(runes)-1]]; ok {
		return string(runes[:len(runes)-1])
	}
	return y
}
