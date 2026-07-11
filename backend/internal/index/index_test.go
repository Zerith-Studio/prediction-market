package index_test

import (
	"context"
	"crypto/ed25519"
	"log/slog"
	"testing"
	"time"

	"github.com/Zerith-Studio/prediction-market/backend/internal/index"
	"github.com/Zerith-Studio/prediction-market/backend/internal/models"
	"github.com/Zerith-Studio/prediction-market/backend/internal/store/storetest"
)

type fakeSource struct{ events []index.OrderStatusEvent }

func (f *fakeSource) Events(context.Context) (<-chan index.OrderStatusEvent, error) {
	ch := make(chan index.OrderStatusEvent, len(f.events))
	for _, ev := range f.events {
		ch <- ev
	}
	close(ch)
	return ch, nil
}

// The chain wins: an observed OrderStatus overwrites the mirror's remaining.
func TestProcessorSyncsMirrorToChain(t *testing.T) {
	ctx := context.Background()
	st := storetest.Open(t)

	matchID, err := st.UpsertMatch(ctx, "idx-1", "A", "B", time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	var marketID [32]byte
	marketID[0] = 0x1D
	if err := st.CreateMarket(ctx, marketID, matchID, "home_win", "binary", "t", "r"); err != nil {
		t.Fatal(err)
	}

	pub, priv, _ := ed25519.GenerateKey(nil)
	var pk [32]byte
	copy(pk[:], pub)
	st.Deposit(ctx, models.PubkeyString(pk), 100_000_000)
	o := &models.Order{Maker: pk, MarketID: marketID, Outcome: models.OutcomeYes,
		Side: models.SideBuy, Price: 50, Size: 100, Salt: 1}
	models.SignOrder(o, priv)
	if err := st.PlaceOrder(ctx, o); err != nil {
		t.Fatal(err)
	}
	hash := models.OrderHash(o)

	// Chain says 40 remaining and closed (partial fill then cancel on-chain).
	proc := index.NewProcessor(st, slog.Default())
	if err := proc.Run(ctx, &fakeSource{events: []index.OrderStatusEvent{
		{OrderHash: hash, Remaining: 40, Closed: true},
	}}); err != nil {
		t.Fatal(err)
	}

	rows, err := st.OrdersByMaker(ctx, models.PubkeyString(pk))
	if err != nil || len(rows) != 1 {
		t.Fatalf("orders: %v %v", rows, err)
	}
	if rows[0].Remaining != 40 || rows[0].Status != "cancelled" {
		t.Errorf("mirror after sync: remaining=%d status=%s, want 40/cancelled",
			rows[0].Remaining, rows[0].Status)
	}
}
