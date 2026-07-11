package mmbot_test

import (
	"context"
	"crypto/ed25519"
	"log/slog"
	"math/rand"
	"testing"
	"time"

	"github.com/Zerith-Studio/prediction-market/backend/internal/crank"
	"github.com/Zerith-Studio/prediction-market/backend/internal/exchange"
	"github.com/Zerith-Studio/prediction-market/backend/internal/lifecycle"
	"github.com/Zerith-Studio/prediction-market/backend/internal/mmbot"
	"github.com/Zerith-Studio/prediction-market/backend/internal/models"
	"github.com/Zerith-Studio/prediction-market/backend/internal/rfq"
	"github.com/Zerith-Studio/prediction-market/backend/internal/store"
	"github.com/Zerith-Studio/prediction-market/backend/internal/store/storetest"
	"github.com/Zerith-Studio/prediction-market/backend/internal/templates"
	"github.com/Zerith-Studio/prediction-market/backend/internal/ws"
)

type fixture struct {
	st  *store.Store
	ex  *exchange.Exchange
	bot *mmbot.Bot
	rfq *rfq.Service
	lc  *lifecycle.Service
}

func setup(t *testing.T) fixture {
	t.Helper()
	log := slog.Default()
	st := storetest.Open(t)
	hub := ws.NewHub(log)
	ex := exchange.New(st, hub, crank.OffchainSubmitter{}, log)
	ex.SettleSync = true
	rfqSvc := rfq.New(st, hub, nil, log)
	_, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	bot := mmbot.New(ex, st, rfqSvc, priv, rand.New(rand.NewSource(42)), log)
	lc := lifecycle.New(st, hub, rfqSvc, nil, bot, log)
	if err := bot.Fund(context.Background(), 1_000_000_000); err != nil { // 1000 USDC
		t.Fatal(err)
	}
	if err := lc.RegisterFixture(context.Background(), "bot-test", "A", "B", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	return fixture{st: st, ex: ex, bot: bot, rfq: rfqSvc, lc: lc}
}

func TestRequotePostsTwoSidedMintLiquidity(t *testing.T) {
	f := setup(t)
	marketID := templates.MarketID("bot-test", "home_win")

	f.bot.OnFairPrice(marketID, 45)

	snap := f.ex.Book(marketID)
	// BUY YES @43 and BUY NO @53 (100−45−2).
	if len(snap.Bids[models.OutcomeYes]) != 1 || snap.Bids[models.OutcomeYes][0].Price != 43 {
		t.Errorf("yes bid: %+v", snap.Bids[models.OutcomeYes])
	}
	if len(snap.Bids[models.OutcomeNo]) != 1 || snap.Bids[models.OutcomeNo][0].Price != 53 {
		t.Errorf("no bid: %+v", snap.Bids[models.OutcomeNo])
	}

	// A taker BUY YES @58 crosses the bot's NO bid (58+53 ≥ 100) → MINT.
	takerPub, takerPriv, _ := ed25519.GenerateKey(nil)
	var takerPK [32]byte
	copy(takerPK[:], takerPub)
	f.st.Deposit(context.Background(), models.PubkeyString(takerPK), 100_000_000)
	o := &models.Order{
		Maker: takerPK, MarketID: marketID, Outcome: models.OutcomeYes,
		Side: models.SideBuy, Price: 58, Size: 40, Salt: 9,
	}
	models.SignOrder(o, takerPriv)
	_, fills, err := f.ex.SubmitOrder(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	if len(fills) != 1 || fills[0].MatchType != models.MatchMint {
		t.Fatalf("want MINT against bot liquidity, got %+v", fills)
	}

	// Fair moves → bot re-quotes; old orders cancelled, new prices live.
	f.bot.OnFairPrice(marketID, 60)
	snap = f.ex.Book(marketID)
	if len(snap.Bids[models.OutcomeYes]) != 1 || snap.Bids[models.OutcomeYes][0].Price != 58 {
		t.Errorf("re-quoted yes bid: %+v", snap.Bids[models.OutcomeYes])
	}
	if len(snap.Bids[models.OutcomeNo]) != 1 || snap.Bids[models.OutcomeNo][0].Price != 38 {
		t.Errorf("re-quoted no bid: %+v", snap.Bids[models.OutcomeNo])
	}
}

func TestQuoteRFQFormulaAndExpiry(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	m1 := templates.MarketID("bot-test", "home_win")
	m2 := templates.MarketID("bot-test", "over_2_5")
	f.bot.OnFairPrice(m1, 50)
	f.bot.OnFairPrice(m2, 50)

	legs := []models.Leg{
		{MarketID: m1, Outcome: models.OutcomeYes},
		{MarketID: m2, Outcome: models.OutcomeYes},
	}
	rfqID, err := f.rfq.CreateRFQ(ctx, "taker-wallet", legs, 5_000_000)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.bot.QuoteRFQ(ctx, mustRFQ(t, f.st, rfqID)); err != nil {
		t.Fatalf("QuoteRFQ: %v", err)
	}

	quotes, err := f.st.QuotesForRFQ(ctx, rfqID)
	if err != nil || len(quotes) != 1 {
		t.Fatalf("quotes: %v %v", quotes, err)
	}
	q := quotes[0]
	// p_joint = 0.25 × 1.15 = 0.2875 → fair 3.478x → margin 10% on the edge:
	// payout = 5M × (1 + 2.478×0.9) = 5M × 3.2304 ≈ 16.15 USDC.
	if q.Payout < 16_000_000 || q.Payout > 16_300_000 {
		t.Errorf("quoted payout = %d, want ≈16.15 USDC", q.Payout)
	}
	if q.Expiry.Before(time.Now()) {
		t.Error("quote must expire in the future")
	}
	// The signature must verify against the bot's pubkey (accept path re-checks).
	makerPK, err := models.ParsePubkey(q.Maker)
	if err != nil {
		t.Fatal(err)
	}
	verify := &models.ComboQuote{Maker: makerPK, Legs: q.Legs, Stake: q.Stake,
		Payout: q.Payout, Expiry: q.Expiry.Unix(), Salt: q.Salt, Sig: q.Sig}
	if !models.VerifyQuoteSig(verify) {
		t.Error("bot quote signature does not verify")
	}
}

func TestSeedPrecisionPopulatesPool(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	pool := templates.MarketID("bot-test", "precision_total_goals")

	n, err := f.bot.SeedPrecision(ctx, pool, 2.6, 1.3, 20, 500_000, 5_000_000)
	if err != nil {
		t.Fatal(err)
	}
	if n != 20 {
		t.Fatalf("seeded %d of 20", n)
	}
	entries, err := f.st.PrecisionLeaderboard(ctx, pool)
	if err != nil || len(entries) != 20 {
		t.Fatalf("entries: %d %v", len(entries), err)
	}
	distinct := map[string]bool{}
	for _, e := range entries {
		distinct[e.Wallet] = true
		if e.Stake < 500_000 || e.Stake > 5_000_000 {
			t.Errorf("stake out of range: %d", e.Stake)
		}
		if e.Guess < 0 || e.Guess > 10 {
			t.Errorf("implausible goals guess: %f", e.Guess)
		}
	}
	if len(distinct) != 20 {
		t.Errorf("personas must be distinct wallets, got %d", len(distinct))
	}
}

func mustRFQ(t *testing.T, st *store.Store, id string) store.RFQRow {
	t.Helper()
	r, err := st.GetRFQ(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return r
}
