package store_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/Zerith-Studio/prediction-market/backend/internal/matching"
	"github.com/Zerith-Studio/prediction-market/backend/internal/models"
	"github.com/Zerith-Studio/prediction-market/backend/internal/store"
	"github.com/Zerith-Studio/prediction-market/backend/internal/store/storetest"
)

var ctx = context.Background()

func wallet(b byte) ([32]byte, string) {
	var pk [32]byte
	pk[0] = b
	pk[31] = 0xFF // avoid the all-zero edge in base58
	return pk, models.PubkeyString(pk)
}

func seedMarket(t *testing.T, s *store.Store, marketID [32]byte) {
	t.Helper()
	matchID, err := s.UpsertMatch(ctx, "fixture-1", "ARG", "FRA", time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("UpsertMatch: %v", err)
	}
	if err := s.CreateMarket(ctx, marketID, matchID, "match_result_home", "binary",
		"ARG to win", "Settles YES if ARG wins in regulation"); err != nil {
		t.Fatalf("CreateMarket: %v", err)
	}
}

func order(maker [32]byte, marketID [32]byte, outcome, side uint8, price uint16, size uint64, salt uint64) *models.Order {
	return &models.Order{
		Maker: maker, MarketID: marketID, Outcome: outcome, Side: side,
		Price: price, Size: size, Salt: salt,
	}
}

// TestMatchLineupsAndLiveState proves the lineups column migration plus the
// SetMatchLineups / SetMatchState / GetMatchByID round-trip against a real DB.
func TestMatchLineupsAndLiveState(t *testing.T) {
	s := storetest.Open(t)
	matchID, err := s.UpsertMatch(ctx, "fx-lineup", "Spain", "Argentina", time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("UpsertMatch: %v", err)
	}

	// A freshly-registered match has no team sheets yet: COALESCE → 'null'.
	m, err := s.GetMatchByID(ctx, matchID)
	if err != nil {
		t.Fatalf("GetMatchByID: %v", err)
	}
	if string(m.Lineups) != "null" {
		t.Fatalf("fresh match lineups = %q, want null", m.Lineups)
	}

	lineups := []byte(`{"home":{"team":"Spain","formation":"4-3-3","starters":[
		{"number":"23","name":"Simon, Unai","position":"Goalkeeper","unit":1},
		{"number":"16","name":"Rodri","position":"Central Midfielder","unit":3,"captain":true}],
		"subs":[{"number":"1","name":"Raya, David","unit":1}]},
		"away":{"team":"Argentina","formation":"4-4-2","starters":[
		{"number":"10","name":"Messi, Lionel","position":"Forward","unit":4,"captain":true}],"subs":[]}}`)
	if err := s.SetMatchLineups(ctx, "fx-lineup", lineups); err != nil {
		t.Fatalf("SetMatchLineups: %v", err)
	}

	live := []byte(`{"minute":58,"period":"2H","home_goals":1,"away_goals":1,
		"possession":{"home":54,"away":46},
		"stats":{"home":{"yellow":1,"red":0,"corners":5},"away":{"yellow":2,"red":0,"corners":3}}}`)
	if err := s.SetMatchState(ctx, "fx-lineup", "live", live); err != nil {
		t.Fatalf("SetMatchState: %v", err)
	}

	m, err = s.GetMatchByID(ctx, matchID)
	if err != nil {
		t.Fatalf("GetMatchByID (after writes): %v", err)
	}
	if m.Status != "live" {
		t.Errorf("status = %q, want live", m.Status)
	}

	var ls struct {
		Period     string `json:"period"`
		Possession struct{ Home, Away int } `json:"possession"`
		Stats      struct {
			Home struct{ Yellow, Corners int } `json:"home"`
		} `json:"stats"`
	}
	if err := json.Unmarshal(m.LiveState, &ls); err != nil {
		t.Fatalf("live_state unmarshal: %v", err)
	}
	if ls.Period != "2H" || ls.Possession.Home != 54 || ls.Stats.Home.Corners != 5 {
		t.Errorf("live_state round-trip: %+v", ls)
	}

	var lu struct {
		Home struct {
			Team      string
			Formation string
			Starters  []struct {
				Number, Name, Position string
				Unit                   int
				Captain                bool
			}
			Subs []struct{ Number, Name string }
		}
	}
	if err := json.Unmarshal(m.Lineups, &lu); err != nil {
		t.Fatalf("lineups unmarshal: %v", err)
	}
	if lu.Home.Team != "Spain" || lu.Home.Formation != "4-3-3" ||
		len(lu.Home.Starters) != 2 || len(lu.Home.Subs) != 1 {
		t.Errorf("lineups round-trip: %+v", lu.Home)
	}
	if lu.Home.Starters[1].Name != "Rodri" || !lu.Home.Starters[1].Captain {
		t.Errorf("captain flag lost: %+v", lu.Home.Starters[1])
	}

	// Fixture-keyed read must see the same sheets.
	byFix, err := s.GetMatchByFixture(ctx, "fx-lineup")
	if err != nil || string(byFix.Lineups) == "null" {
		t.Errorf("GetMatchByFixture lineups: %v / %q", err, byFix.Lineups)
	}
}

func TestPlaceOrderLocksAndRejects(t *testing.T) {
	s := storetest.Open(t)
	var marketID [32]byte
	marketID[0] = 1
	seedMarket(t, s, marketID)
	buyerPK, buyer := wallet(1)

	// No balance → rejected.
	o := order(buyerPK, marketID, models.OutcomeYes, models.SideBuy, 60, 100, 1)
	if err := s.PlaceOrder(ctx, o); !errors.Is(err, store.ErrInsufficientFunds) {
		t.Fatalf("want ErrInsufficientFunds, got %v", err)
	}

	// Fund 100 USDC; BUY 100 shares @60¢ locks exactly 60 USDC.
	if _, err := s.Deposit(ctx, buyer, 100_000_000); err != nil {
		t.Fatal(err)
	}
	if err := s.PlaceOrder(ctx, o); err != nil {
		t.Fatalf("funded PlaceOrder: %v", err)
	}
	b, _ := s.GetBalance(ctx, buyer)
	if b.Available != 40_000_000 || b.Locked != 60_000_000 {
		t.Errorf("balance after lock: %+v", b)
	}

	// Same order again → duplicate.
	if err := s.PlaceOrder(ctx, o); !errors.Is(err, store.ErrDuplicateOrder) {
		t.Fatalf("want ErrDuplicateOrder, got %v", err)
	}

	// SELL without tokens → rejected (no naked shorts, ADR 0002).
	sell := order(buyerPK, marketID, models.OutcomeYes, models.SideSell, 70, 10, 2)
	if err := s.PlaceOrder(ctx, sell); !errors.Is(err, store.ErrInsufficientTokens) {
		t.Fatalf("want ErrInsufficientTokens, got %v", err)
	}

	// Grant tokens, then SELL locks them.
	if err := s.GrantTokens(ctx, buyer, marketID, 10, 0); err != nil {
		t.Fatal(err)
	}
	if err := s.PlaceOrder(ctx, sell); err != nil {
		t.Fatalf("SELL with tokens: %v", err)
	}
	positions, _ := s.GetPositions(ctx, buyer)
	if len(positions) != 1 || positions[0].YesLocked != 10 {
		t.Errorf("positions after SELL lock: %+v", positions)
	}
}

// The core ledger test: a NORMAL fill mirrors lib.rs settle_normal — buyer's
// lock pays the seller at the fill price, tokens change hands, taker
// improvement refunds.
func TestApplyFillNormal(t *testing.T) {
	s := storetest.Open(t)
	var marketID [32]byte
	marketID[0] = 2
	seedMarket(t, s, marketID)
	sellerPK, seller := wallet(1)
	buyerPK, buyer := wallet(2)

	s.Deposit(ctx, buyer, 100_000_000)
	s.GrantTokens(ctx, seller, marketID, 50, 0)

	maker := order(sellerPK, marketID, models.OutcomeYes, models.SideSell, 60, 50, 1)
	taker := order(buyerPK, marketID, models.OutcomeYes, models.SideBuy, 65, 30, 2)
	if err := s.PlaceOrder(ctx, maker); err != nil {
		t.Fatal(err)
	}
	if err := s.PlaceOrder(ctx, taker); err != nil {
		t.Fatal(err)
	}

	book := matching.NewBook(marketID)
	book.LoadResting(maker, 50)
	fills, err := book.Submit(taker)
	if err != nil || len(fills) != 1 {
		t.Fatalf("engine: %v %v", fills, err)
	}
	// Engine sees taker as new — but we already placed its row; the store path
	// used by the API places the row first, then applies fills. Replicate that.
	fillID, err := s.ApplyFill(ctx, fills[0])
	if err != nil {
		t.Fatalf("ApplyFill: %v", err)
	}

	// Buyer: locked 65×30 = 19.5 USDC for this fill... entry locked 65×30 = 19.5;
	// executed at 60 → 18 USDC spent, 1.5 refunded. Started with 100, locked 19.5.
	b, _ := s.GetBalance(ctx, buyer)
	if b.Available != 100_000_000-18_000_000-0 { // 19.5 locked − 1.5 refund = available 82
		t.Errorf("buyer available = %d, want 82000000", b.Available)
	}
	if b.Locked != 0 {
		t.Errorf("buyer locked = %d, want 0 (fully filled)", b.Locked)
	}
	// Seller: +18 USDC, −30 YES.
	sb, _ := s.GetBalance(ctx, seller)
	if sb.Available != 18_000_000 {
		t.Errorf("seller available = %d, want 18000000", sb.Available)
	}
	sp, _ := s.GetPositions(ctx, seller)
	if sp[0].Yes != 20 || sp[0].YesLocked != 20 {
		t.Errorf("seller position: %+v (want yes=20 locked=20)", sp[0])
	}
	bp, _ := s.GetPositions(ctx, buyer)
	if bp[0].Yes != 30 {
		t.Errorf("buyer position: %+v (want yes=30)", bp[0])
	}

	// Revert (settle_match failed on-chain) → everything restores.
	if err := s.RevertFill(ctx, fillID, fills[0]); err != nil {
		t.Fatalf("RevertFill: %v", err)
	}
	b, _ = s.GetBalance(ctx, buyer)
	if b.Available != 100_000_000-19_500_000 || b.Locked != 19_500_000 {
		t.Errorf("buyer after revert: %+v", b)
	}
	sb, _ = s.GetBalance(ctx, seller)
	if sb.Available != 0 {
		t.Errorf("seller after revert: %+v", sb)
	}
	sp, _ = s.GetPositions(ctx, seller)
	if sp[0].Yes != 50 || sp[0].YesLocked != 50 {
		t.Errorf("seller position after revert: %+v", sp[0])
	}
}

// MINT mirrors lib.rs settle_mint: both buyers pay their OWN limit into the
// pool and each receives freshly minted shares of their outcome.
func TestApplyFillMint(t *testing.T) {
	s := storetest.Open(t)
	var marketID [32]byte
	marketID[0] = 3
	seedMarket(t, s, marketID)
	yesPK, yesBuyer := wallet(1)
	noPK, noBuyer := wallet(2)

	s.Deposit(ctx, yesBuyer, 100_000_000)
	s.Deposit(ctx, noBuyer, 100_000_000)

	makerNo := order(noPK, marketID, models.OutcomeNo, models.SideBuy, 45, 40, 1)
	takerYes := order(yesPK, marketID, models.OutcomeYes, models.SideBuy, 65, 40, 2)
	if err := s.PlaceOrder(ctx, makerNo); err != nil {
		t.Fatal(err)
	}
	if err := s.PlaceOrder(ctx, takerYes); err != nil {
		t.Fatal(err)
	}

	book := matching.NewBook(marketID)
	book.LoadResting(makerNo, 40)
	fills, _ := book.Submit(takerYes)
	if len(fills) != 1 || fills[0].MatchType != models.MatchMint {
		t.Fatalf("want MINT fill, got %+v", fills)
	}
	if _, err := s.ApplyFill(ctx, fills[0]); err != nil {
		t.Fatalf("ApplyFill: %v", err)
	}

	// YES buyer paid own limit 65×40 = 26 USDC (no refund — lib.rs charges limit).
	yb, _ := s.GetBalance(ctx, yesBuyer)
	if yb.Available != 74_000_000 || yb.Locked != 0 {
		t.Errorf("yes buyer: %+v", yb)
	}
	nb, _ := s.GetBalance(ctx, noBuyer)
	if nb.Available != 82_000_000 || nb.Locked != 0 { // 45×40 = 18 spent
		t.Errorf("no buyer: %+v", nb)
	}
	yp, _ := s.GetPositions(ctx, yesBuyer)
	np, _ := s.GetPositions(ctx, noBuyer)
	if yp[0].Yes != 40 || np[0].No != 40 {
		t.Errorf("minted positions: yes=%+v no=%+v", yp[0], np[0])
	}
}

// MERGE mirrors lib.rs settle_merge: both sellers' tokens burn and each
// receives their own limit price from the pool.
func TestApplyFillMerge(t *testing.T) {
	s := storetest.Open(t)
	var marketID [32]byte
	marketID[0] = 4
	seedMarket(t, s, marketID)
	yesPK, yesSeller := wallet(1)
	noPK, noSeller := wallet(2)

	s.GrantTokens(ctx, yesSeller, marketID, 25, 0)
	s.GrantTokens(ctx, noSeller, marketID, 0, 25)

	makerNo := order(noPK, marketID, models.OutcomeNo, models.SideSell, 35, 25, 1)
	takerYes := order(yesPK, marketID, models.OutcomeYes, models.SideSell, 60, 25, 2)
	if err := s.PlaceOrder(ctx, makerNo); err != nil {
		t.Fatal(err)
	}
	if err := s.PlaceOrder(ctx, takerYes); err != nil {
		t.Fatal(err)
	}

	book := matching.NewBook(marketID)
	book.LoadResting(makerNo, 25)
	fills, _ := book.Submit(takerYes)
	if len(fills) != 1 || fills[0].MatchType != models.MatchMerge {
		t.Fatalf("want MERGE fill, got %+v", fills)
	}
	if _, err := s.ApplyFill(ctx, fills[0]); err != nil {
		t.Fatalf("ApplyFill: %v", err)
	}

	yb, _ := s.GetBalance(ctx, yesSeller)
	nb, _ := s.GetBalance(ctx, noSeller)
	if yb.Available != 15_000_000 { // 60×25
		t.Errorf("yes seller: %+v", yb)
	}
	if nb.Available != 8_750_000 { // 35×25
		t.Errorf("no seller: %+v", nb)
	}
	yp, _ := s.GetPositions(ctx, yesSeller)
	np, _ := s.GetPositions(ctx, noSeller)
	if yp[0].Yes != 0 || np[0].No != 0 {
		t.Errorf("burned positions: yes=%+v no=%+v", yp, np)
	}
}

func TestCancelReleasesLock(t *testing.T) {
	s := storetest.Open(t)
	var marketID [32]byte
	marketID[0] = 5
	seedMarket(t, s, marketID)
	pk, w := wallet(1)
	s.Deposit(ctx, w, 50_000_000)

	o := order(pk, marketID, models.OutcomeYes, models.SideBuy, 50, 80, 1)
	if err := s.PlaceOrder(ctx, o); err != nil {
		t.Fatal(err)
	}
	hash := models.OrderHash(o)
	if err := s.CancelOrder(ctx, hash, pk); err != nil {
		t.Fatalf("CancelOrder: %v", err)
	}
	b, _ := s.GetBalance(ctx, w)
	if b.Available != 50_000_000 || b.Locked != 0 {
		t.Errorf("after cancel: %+v", b)
	}
	// Cancel again → not found (already cancelled).
	if err := s.CancelOrder(ctx, hash, pk); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	// Wrong maker → not found (no cross-wallet cancels).
	o2 := order(pk, marketID, models.OutcomeYes, models.SideBuy, 50, 10, 2)
	if err := s.PlaceOrder(ctx, o2); err != nil {
		t.Fatal(err)
	}
	other, _ := wallet(9)
	_ = other
	otherPK, _ := wallet(9)
	if err := s.CancelOrder(ctx, models.OrderHash(o2), otherPK); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cross-wallet cancel must fail, got %v", err)
	}
}

func TestComboLifecycle(t *testing.T) {
	s := storetest.Open(t)
	var m1, m2 [32]byte
	m1[0], m2[0] = 6, 7
	seedMarket(t, s, m1)
	matchID, _ := s.UpsertMatch(ctx, "fixture-2", "BRA", "GER", time.Now().Add(time.Hour))
	s.CreateMarket(ctx, m2, matchID, "over_2_5", "binary", "Over 2.5 goals", "rule")

	makerPK, maker := wallet(1)
	takerPK, taker := wallet(2)
	_ = takerPK
	s.Deposit(ctx, maker, 100_000_000)
	s.Deposit(ctx, taker, 20_000_000)

	legs := []models.Leg{{MarketID: m1, Outcome: 1}, {MarketID: m2, Outcome: 1}}
	rfqID, err := s.CreateRFQ(ctx, taker, legs, 5_000_000)
	if err != nil {
		t.Fatalf("CreateRFQ: %v", err)
	}

	q := &models.ComboQuote{
		Maker: makerPK, Legs: legs,
		Stake: 5_000_000, Payout: 18_000_000,
		Expiry: time.Now().Add(time.Minute).Unix(), Salt: 42,
	}
	if err := s.InsertQuote(ctx, q, rfqID); err != nil {
		t.Fatalf("InsertQuote: %v", err)
	}
	rfq, _ := s.GetRFQ(ctx, rfqID)
	if rfq.Status != "quoted" {
		t.Errorf("rfq status = %s, want quoted", rfq.Status)
	}

	qh := models.QuoteHash(q)
	if err := s.AcceptQuote(ctx, qh, taker, "demo-tx"); err != nil {
		t.Fatalf("AcceptQuote: %v", err)
	}
	// Double-accept must fail — single-use quote (ADR 0004).
	if err := s.AcceptQuote(ctx, qh, taker, ""); !errors.Is(err, store.ErrQuoteNotOpen) {
		t.Fatalf("double accept must fail, got %v", err)
	}

	// Stake 5 left taker; risk 13 left maker.
	tb, _ := s.GetBalance(ctx, taker)
	mb, _ := s.GetBalance(ctx, maker)
	if tb.Available != 15_000_000 || mb.Available != 87_000_000 {
		t.Errorf("post-accept balances: taker=%+v maker=%+v", tb, mb)
	}

	// All legs win → taker gets the pot.
	if err := s.ResolveEscrow(ctx, qh, "won", "resolve-tx"); err != nil {
		t.Fatalf("ResolveEscrow: %v", err)
	}
	tb, _ = s.GetBalance(ctx, taker)
	if tb.Available != 33_000_000 { // 15 + 18
		t.Errorf("taker after win: %+v", tb)
	}
	escrows, _ := s.EscrowsForWallet(ctx, taker)
	if len(escrows) != 1 || escrows[0].Status != "won" || escrows[0].ResolveTx != "resolve-tx" {
		t.Errorf("escrow: %+v", escrows)
	}
}

func TestPrecisionLifecycle(t *testing.T) {
	s := storetest.Open(t)
	var marketID [32]byte
	marketID[0] = 8
	matchID, _ := s.UpsertMatch(ctx, "fixture-3", "ESP", "ITA", time.Now().Add(time.Hour))
	s.CreateMarket(ctx, marketID, matchID, "total_passes", "precision", "Total passes", "rule")

	_, w1 := wallet(1)
	_, w2 := wallet(2)
	_, w3 := wallet(3)
	for _, w := range []string{w1, w2, w3} {
		s.Deposit(ctx, w, 10_000_000)
	}

	if _, err := s.EnterPrecision(ctx, marketID, w1, 850, 1_000_000); err != nil {
		t.Fatalf("enter w1: %v", err)
	}
	if _, err := s.EnterPrecision(ctx, marketID, w2, 900, 1_000_000); err != nil {
		t.Fatalf("enter w2: %v", err)
	}
	if _, err := s.EnterPrecision(ctx, marketID, w3, 1100, 2_000_000); err != nil {
		t.Fatalf("enter w3: %v", err)
	}
	// One entry per wallet (ADR 0006 C1).
	if _, err := s.EnterPrecision(ctx, marketID, w1, 999, 1_000_000); !errors.Is(err, store.ErrAlreadyEntered) {
		t.Fatalf("want ErrAlreadyEntered, got %v", err)
	}

	outcome, _ := json.Marshal(map[string]any{"actual": 880})
	sum, err := s.SettlePrecision(ctx, marketID, 880, 100, 2, 200, outcome) // 2% rake
	if err != nil {
		t.Fatalf("SettlePrecision: %v", err)
	}
	if sum.Entries != 3 {
		t.Errorf("settlement: %+v", sum)
	}
	if sum.Pool != 3_920_000 { // 4M stake − 2%
		t.Errorf("pool = %d, want 3920000", sum.Pool)
	}

	lb, err := s.PrecisionLeaderboard(ctx, marketID)
	if err != nil {
		t.Fatal(err)
	}
	// w2 (off by 20) beats w1 (off by 30) beats w3 (off by 220).
	if lb[0].Wallet != w2 || lb[1].Wallet != w1 || lb[2].Wallet != w3 {
		t.Errorf("leaderboard order: %s %s %s", lb[0].Wallet, lb[1].Wallet, lb[2].Wallet)
	}
	var paid uint64
	for _, e := range lb {
		if e.Payout != nil {
			paid += *e.Payout
		}
	}
	if paid > sum.Pool || sum.Pool-paid > 3 { // integer floor dust only
		t.Errorf("paid %d of pool %d", paid, sum.Pool)
	}

	// Entry after settle → market not open.
	_, w4 := wallet(4)
	s.Deposit(ctx, w4, 10_000_000)
	if _, err := s.EnterPrecision(ctx, marketID, w4, 800, 1_000_000); !errors.Is(err, store.ErrMarketNotOpen) {
		t.Fatalf("want ErrMarketNotOpen, got %v", err)
	}
}

func TestPrecisionKickoffLock(t *testing.T) {
	s := storetest.Open(t)
	var marketID [32]byte
	marketID[0] = 9
	// Kickoff already passed → entries closed (ADR 0006 C2).
	matchID, _ := s.UpsertMatch(ctx, "fixture-4", "POR", "NED", time.Now().Add(-time.Minute))
	s.CreateMarket(ctx, marketID, matchID, "total_goals", "precision", "Total goals", "rule")
	_, w := wallet(1)
	s.Deposit(ctx, w, 10_000_000)
	if _, err := s.EnterPrecision(ctx, marketID, w, 3, 1_000_000); !errors.Is(err, store.ErrMarketNotOpen) {
		t.Fatalf("post-kickoff entry must fail, got %v", err)
	}
}

func TestOnelinersAndFills(t *testing.T) {
	s := storetest.Open(t)
	var marketID [32]byte
	marketID[0] = 10
	seedMarket(t, s, marketID)

	lines := []string{"ARG lead the xG battle", "FRA pressing high"}
	if err := s.InsertOneliners(ctx, marketID, lines); err != nil {
		t.Fatal(err)
	}
	got, _, err := s.LatestOneliners(ctx, marketID)
	if err != nil || len(got) != 2 || got[0] != lines[0] {
		t.Fatalf("oneliners: %v %v", got, err)
	}
}
