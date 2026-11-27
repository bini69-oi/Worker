package install

import (
	"errors"
	"testing"

	"github.com/bini69-oi/worker/release"
)

// Пока владелец не положил настоящий ключ, вшитый ключ заглушка и проверка отказывает.
// Когда ключ появится, тест перестанет видеть заглушку и сверит, что ключ читается.
func TestEmbeddedKey(t *testing.T) {
	_, err := release.ParsePublic(ReleasePub)
	if errors.Is(err, release.ErrNoKey) {
		if err := release.Verify(ReleasePub, []byte("x"), make([]byte, 64)); !errors.Is(err, release.ErrNoKey) {
			t.Fatalf("с заглушкой проверка должна отказывать: %v", err)
		}
		return
	}
	if err != nil {
		t.Fatalf("release.pub не читается: %v", err)
	}
}
