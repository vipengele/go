package config

import (
	"reflect"
	"testing"
	"time"
)

func TestSourcesFormat(t *testing.T) {
	defaults := bindConfig{
		B: true, I: -1, I8: 8, U16: 16, F32: 0.1, F64: 1e21, S: "a b", D: 90 * time.Second, Level: 2,
		Ints: []int{1, 2}, Durs: []time.Duration{time.Second, time.Hour},
	}
	res, err := Load(defaults, &stubProvider{entries: []Entry{{Key: "strs", Items: []string{"x", "y"}, List: true, Origin: "stub"}}})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := []Source{
		{Key: "b", Value: "true", Origin: "default"},
		{Key: "i", Value: "-1", Origin: "default"},
		{Key: "i8", Value: "8", Origin: "default"},
		{Key: "u16", Value: "16", Origin: "default"},
		{Key: "f32", Value: "0.1", Origin: "default"},
		{Key: "f64", Value: "1e+21", Origin: "default"},
		{Key: "s", Value: "a b", Origin: "default"},
		{Key: "d", Value: "1m30s", Origin: "default"},
		{Key: "level", Value: "2", Origin: "default"},
		{Key: "ints", Value: "1,2", Origin: "default"},
		{Key: "strs", Value: "x,y", Origin: "stub"},
		{Key: "durs", Value: "1s,1h0m0s", Origin: "default"},
	}
	if got := res.Sources(); !reflect.DeepEqual(got, want) {
		t.Errorf("Sources() =\n%+v\nwant\n%+v", got, want)
	}
}

func TestSourcesValueReadsBack(t *testing.T) {
	defaults := bindConfig{F32: 0.1, F64: 1.0 / 3, D: 1500 * time.Millisecond, Ints: []int{-3, 7}, Strs: []string{"p", "q"}, Durs: []time.Duration{time.Minute}}
	first, err := Load(defaults)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	var entries []Entry
	for _, s := range first.Sources() {
		entries = append(entries, Entry{Key: s.Key, Value: s.Value, Origin: "sources"})
	}
	second, err := Load(bindConfig{}, &stubProvider{entries: entries})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got, want := second.Value(), first.Value(); !reflect.DeepEqual(got, want) {
		t.Errorf("Value() = %+v, want %+v", got, want)
	}
}

func TestSourcesIsACopy(t *testing.T) {
	res, err := Load(loadDefaults())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	got := res.Sources()
	got[0] = Source{Key: "changed", Value: "changed", Origin: "changed"}
	if again := res.Sources(); again[0].Key != "name" || again[0].Value != "app" || again[0].Origin != "default" {
		t.Errorf("Sources()[0] = %+v after changing an earlier copy", again[0])
	}
}
