package db

import (
	"context"
	"testing"
)

func TestSettingsRoundTrip(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)

	if _, ok, err := store.GetSetting(ctx, "theme"); err != nil || ok {
		t.Fatalf("missing key: ok=%v err=%v", ok, err)
	}
	if err := store.SetSetting(ctx, "theme", "dark"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetSetting(ctx, "theme", "light"); err != nil {
		t.Fatal(err)
	}
	if v, ok, err := store.GetSetting(ctx, "theme"); err != nil || !ok || v != "light" {
		t.Errorf("got %q ok=%v err=%v, want the latest value", v, ok, err)
	}
}

func TestSelectedModelRoundTrip(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)

	if name, err := store.GetSelectedModel(ctx); err != nil || name != "" {
		t.Fatalf("nothing saved: got %q err %v", name, err)
	}
	if err := store.SetSelectedModel(ctx, "deepseek-v2:latest"); err != nil {
		t.Fatal(err)
	}
	if name, err := store.GetSelectedModel(ctx); err != nil || name != "deepseek-v2:latest" {
		t.Errorf("got %q err %v, want the saved model", name, err)
	}
}
