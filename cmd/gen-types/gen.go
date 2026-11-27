package main

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"
)

const header = "// Код создан командой make types из структур Go (пакет api). Руками не править.\n"

var (
	timeType = reflect.TypeFor[time.Time]()
	rawType  = reflect.TypeFor[json.RawMessage]()
)

// generate пишет TypeScript-интерфейсы для структур roots и всех структур, на которые они ссылаются.
// Порядок стабильный: по имени. Что не умеет, отвергает ошибкой: тихо пропущенное поле фронт увидит пустым.
//
// Срезы Go описываются как T[]: обработчик обязан отдавать пустой срез, а не nil, иначе JSON даст null.
func generate(roots []reflect.Type) (string, error) {
	g := &gen{seen: map[string]reflect.Type{}, out: map[string]string{}}
	for _, t := range roots {
		if t.Kind() != reflect.Struct {
			return "", fmt.Errorf("%v: корень должен быть структурой", t)
		}
		if err := g.add(t); err != nil {
			return "", err
		}
	}
	names := make([]string, 0, len(g.out))
	for n := range g.out {
		names = append(names, n)
	}
	slices.Sort(names)
	var sb strings.Builder
	sb.WriteString(header)
	for _, n := range names {
		sb.WriteString("\n" + g.out[n])
	}
	return sb.String(), nil
}

type gen struct {
	seen map[string]reflect.Type
	out  map[string]string
}

// add описывает структуру и тянет за собой вложенные.
func (g *gen) add(t reflect.Type) error {
	name := t.Name()
	if name == "" {
		return fmt.Errorf("безымянная структура %v: дайте ей имя", t)
	}
	if prev, ok := g.seen[name]; ok {
		if prev == t {
			return nil
		}
		return fmt.Errorf("имя %s у двух разных типов: %v и %v", name, prev, t)
	}
	g.seen[name] = t
	var sb strings.Builder
	sb.WriteString("export interface " + name + " {\n")
	if err := g.fields(&sb, t, name); err != nil {
		return err
	}
	sb.WriteString("}\n")
	g.out[name] = sb.String()
	return nil
}

func (g *gen) fields(sb *strings.Builder, t reflect.Type, owner string) error {
	for f := range t.Fields() {
		tag, hasTag := f.Tag.Lookup("json")
		jsonName, opts, _ := strings.Cut(tag, ",")
		if jsonName == "-" && opts == "" {
			continue
		}
		if f.Anonymous && jsonName == "" && f.Type.Kind() == reflect.Struct {
			// Встроенная структура без тега раскрывается в поля владельца, как в encoding/json.
			if err := g.fields(sb, f.Type, owner); err != nil {
				return err
			}
			continue
		}
		if !f.IsExported() {
			continue
		}
		if !hasTag || jsonName == "" {
			jsonName = f.Name
		}
		ts, err := g.ts(f.Type)
		if err != nil {
			return fmt.Errorf("%s.%s: %w", owner, f.Name, err)
		}
		opt := ""
		if slices.Contains(strings.Split(opts, ","), "omitempty") {
			opt = "?"
		}
		fmt.Fprintf(sb, "  %s%s: %s;\n", jsonName, opt, ts)
	}
	return nil
}

// ts отдаёт тип TypeScript для типа Go.
func (g *gen) ts(t reflect.Type) (string, error) {
	if t == timeType {
		return "string", nil
	}
	if t == rawType {
		return "unknown", nil
	}
	if t.Kind() == reflect.Slice && t.Elem().Kind() == reflect.Uint8 {
		return "string", nil // []byte в JSON это строка base64
	}
	switch t.Kind() {
	case reflect.String:
		return "string", nil
	case reflect.Bool:
		return "boolean", nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return "number", nil
	case reflect.Interface:
		return "unknown", nil
	case reflect.Pointer:
		in, err := g.ts(t.Elem())
		if err != nil {
			return "", err
		}
		return in + " | null", nil
	case reflect.Slice, reflect.Array:
		in, err := g.ts(t.Elem())
		if err != nil {
			return "", err
		}
		if strings.Contains(in, " ") {
			in = "(" + in + ")"
		}
		return in + "[]", nil
	case reflect.Map:
		if t.Key().Kind() != reflect.String {
			return "", fmt.Errorf("ключ карты %v не строка", t.Key())
		}
		in, err := g.ts(t.Elem())
		if err != nil {
			return "", err
		}
		return "Record<string, " + in + ">", nil
	case reflect.Struct:
		if err := g.add(t); err != nil {
			return "", err
		}
		return t.Name(), nil
	}
	return "", fmt.Errorf("тип %v не поддерживается", t)
}
