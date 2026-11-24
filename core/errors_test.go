package core

import (
	"regexp"
	"testing"
)

func TestErrorCodesUnique(t *testing.T) {
	codeRe := regexp.MustCompile(`^E[1-6]\d{3}$`)
	codes := map[string]bool{}
	keys := map[string]bool{}
	for _, e := range allErrors {
		if !codeRe.MatchString(e.Code) {
			t.Errorf("код %q не вида E + 4 цифры из групп 1-6", e.Code)
		}
		if codes[e.Code] {
			t.Errorf("код %s повторяется", e.Code)
		}
		if keys[e.Key] {
			t.Errorf("ключ текста %s повторяется", e.Key)
		}
		codes[e.Code], keys[e.Key] = true, true
	}
}
