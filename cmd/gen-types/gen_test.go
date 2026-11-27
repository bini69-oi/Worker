package main

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

type Money struct {
	Kopecks int64  `json:"kopecks"`
	Label   string `json:"label,omitempty"`
}

type base struct {
	ID int64 `json:"id"`
}

type sample struct {
	base
	Name    string            `json:"name"`
	Active  bool              `json:"active"`
	Price   Money             `json:"price"`
	Old     *Money            `json:"old"`
	Tags    []string          `json:"tags"`
	Items   []Money           `json:"items"`
	Meta    map[string]string `json:"meta"`
	At      time.Time         `json:"at"`
	Left    float64           `json:"left,omitempty"`
	Any     any               `json:"any"`
	Raw     json.RawMessage   `json:"raw"`
	Blob    []byte            `json:"blob"`
	Secret  string            `json:"-"`
	private string
	Plain   string
}

func TestGenerate(t *testing.T) {
	got, err := generate([]reflect.Type{reflect.TypeOf(sample{private: "x"})})
	if err != nil {
		t.Fatal(err)
	}
	want := `// Код создан командой make types из структур Go (пакет api). Руками не править.

export interface Money {
  kopecks: number;
  label?: string;
}

export interface sample {
  id: number;
  name: string;
  active: boolean;
  price: Money;
  old: Money | null;
  tags: string[];
  items: Money[];
  meta: Record<string, string>;
  at: string;
  left?: number;
  any: unknown;
  raw: unknown;
  blob: string;
  Plain: string;
}
`
	if got != want {
		t.Fatalf("получили:\n%s\nждали:\n%s", got, want)
	}
}

func TestGenerateDeterministic(t *testing.T) {
	types := []reflect.Type{reflect.TypeOf(sample{}), reflect.TypeOf(Money{})}
	a, err := generate(types)
	if err != nil {
		t.Fatal(err)
	}
	for range 5 {
		b, _ := generate(types)
		if a != b {
			t.Fatal("вывод меняется от запуска к запуску")
		}
	}
}

func TestGenerateRejectsUnsupported(t *testing.T) {
	type bad struct {
		C chan int `json:"c"`
	}
	type badKey struct {
		M map[int]string `json:"m"`
	}
	for _, typ := range []reflect.Type{reflect.TypeOf(bad{}), reflect.TypeOf(badKey{}), reflect.TypeOf(0)} {
		if _, err := generate([]reflect.Type{typ}); err == nil {
			t.Errorf("%v: ждали ошибку", typ)
		}
	}
}

func TestGenerateNameClash(t *testing.T) {
	type Money struct{ Other int }
	_, err := generate([]reflect.Type{reflect.TypeOf(Money{}), reflect.TypeOf(sample{})})
	if err == nil || !strings.Contains(err.Error(), "Money") {
		t.Errorf("ждали ошибку про повтор имени Money: %v", err)
	}
}
