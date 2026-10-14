package db

import (
	"context"
	"testing"
)

func TestDailyChallengeCountDefaultsAndClamps(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)

	n, err := store.DailyChallengeCount(ctx)
	if err != nil || n != DefaultDailyChallenges {
		t.Fatalf("default = %d (%v), want %d", n, err, DefaultDailyChallenges)
	}

	if err := store.SetDailyChallengeCount(ctx, 4); err != nil {
		t.Fatal(err)
	}
	if n, _ := store.DailyChallengeCount(ctx); n != 4 {
		t.Errorf("saved count = %d, want 4", n)
	}

	if err := store.SetDailyChallengeCount(ctx, 42); err != nil {
		t.Fatal(err)
	}
	if n, _ := store.DailyChallengeCount(ctx); n != MaxDailyChallenges {
		t.Errorf("count should clamp to %d, got %d", MaxDailyChallenges, n)
	}
}
