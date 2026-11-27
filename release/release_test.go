package release

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestSignVerify(t *testing.T) {
	priv, pub, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	sums := []byte("abc  worker-linux-amd64\n")
	sig := Sign(priv, sums)
	if len(sig) != 64 {
		t.Fatalf("подпись %d байт, нужно 64", len(sig))
	}
	if err := Verify(pub, sums, sig); err != nil {
		t.Fatalf("верная подпись не принята: %v", err)
	}
	table := []struct {
		name string
		msg  []byte
		sig  []byte
	}{
		{"список изменён", []byte("abd  worker-linux-amd64\n"), sig},
		{"подпись обрезана", sums, sig[:63]},
		{"подпись пустая", sums, nil},
		{"подпись испорчена", sums, append([]byte{sig[0] ^ 1}, sig[1:]...)},
	}
	for _, c := range table {
		if err := Verify(pub, c.msg, c.sig); !errors.Is(err, ErrBadSignature) {
			t.Errorf("%s: ждали ErrBadSignature, получили %v", c.name, err)
		}
	}
}

func TestWrongKeyRejected(t *testing.T) {
	priv, _, _ := GenerateKey()
	_, otherPub, _ := GenerateKey()
	sums := []byte("x")
	if err := Verify(otherPub, sums, Sign(priv, sums)); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("подпись чужим ключом принята: %v", err)
	}
}

func TestPlaceholderKeyRefuses(t *testing.T) {
	priv, _, _ := GenerateKey()
	sums := []byte("x")
	stub := []byte("# заглушка, ключ ещё не создан\n")
	if err := Verify(stub, sums, Sign(priv, sums)); !errors.Is(err, ErrNoKey) {
		t.Fatalf("с заглушкой проверка должна отказывать с ErrNoKey, получили %v", err)
	}
	if err := Verify(nil, sums, Sign(priv, sums)); !errors.Is(err, ErrNoKey) {
		t.Fatalf("с пустым ключом нужен ErrNoKey, получили %v", err)
	}
}

func TestPrivateKeyRoundTrip(t *testing.T) {
	priv, pub, _ := GenerateKey()
	back, err := ParsePrivate(EncodePrivate(priv))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(back, priv) {
		t.Fatal("закрытый ключ не вернулся")
	}
	sums := []byte("y")
	if err := Verify(pub, sums, Sign(back, sums)); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"", "не base64!", "AAAA"} {
		if _, err := ParsePrivate(bad); err == nil {
			t.Errorf("ParsePrivate(%q) принял мусор", bad)
		}
	}
}

func TestPrivateKeyErrorHasNoSecret(t *testing.T) {
	_, err := ParsePrivate("QUJD")
	if err == nil || strings.Contains(err.Error(), "QUJD") {
		t.Fatalf("ошибка выдаёт значение: %v", err)
	}
}
