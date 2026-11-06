package daemon

import (
	"context"
	"testing"
)

type reloadsInt struct {
	fakeRunner
	got []int
}

func (r *reloadsInt) Reload(_ context.Context, cfg int) error {
	r.got = append(r.got, cfg)
	return nil
}

type reloadsString struct {
	fakeRunner
	got []string
}

func (r *reloadsString) Reload(_ context.Context, cfg string) error {
	r.got = append(r.got, cfg)
	return nil
}

func TestNewRejectsANilReloadLoad(t *testing.T) {
	_, err := New([]Runner{&fakeRunner{name: "a"}}, WithReload[int](nil))
	if err == nil {
		t.Fatal("New with a nil load succeeded")
	}
}

func TestNewSetsTheReloadAndSignalSeams(t *testing.T) {
	s, err := New([]Runner{&fakeRunner{name: "a"}})
	if err != nil {
		t.Fatal(err)
	}
	if s.reload != nil {
		t.Error("reload is set without WithReload")
	}
	if s.newReloadContext == nil || s.notify == nil || s.unnotify == nil {
		t.Errorf("seams not defaulted: newReloadContext=%t notify=%t unnotify=%t",
			s.newReloadContext != nil, s.notify != nil, s.unnotify != nil)
	}
	ctx, cancel := s.newReloadContext(context.Background())
	defer cancel()
	if _, ok := ctx.Deadline(); !ok {
		t.Error("reload context has no deadline")
	}
}

func TestWithReloadLastOneWins(t *testing.T) {
	first := func(context.Context) (int, error) { return 1, nil }
	second := func(context.Context) (string, error) { return "two", nil }
	ints := &reloadsInt{fakeRunner: fakeRunner{name: "i"}}
	strs := &reloadsString{fakeRunner: fakeRunner{name: "s"}}
	plain := &fakeRunner{name: "p"}

	s, err := New([]Runner{ints, strs, plain}, WithReload(first), WithReload(second))
	if err != nil {
		t.Fatal(err)
	}
	if s.reload == nil {
		t.Fatal("reload not set")
	}
	offer, err := s.reload.load(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	for _, r := range []Runner{ints, strs, plain} {
		offered, err := offer(context.Background(), r)
		if err != nil {
			t.Fatalf("offer to %s: %v", r.Name(), err)
		}
		if want := r == Runner(strs); offered != want {
			t.Errorf("offered to %s = %t, want %t", r.Name(), offered, want)
		}
	}
	if len(ints.got) != 0 {
		t.Errorf("int Runner was offered %v", ints.got)
	}
	if len(strs.got) != 1 || strs.got[0] != "two" {
		t.Errorf("string Runner got %v, want [two]", strs.got)
	}
}
