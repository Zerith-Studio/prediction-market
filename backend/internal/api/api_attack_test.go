package api_test

// Attack-vector suite: every hostile or malformed input the UI (or a bad actor
// with the API contract) could send. The bar is simple — NONE of these may
// return a 500 or corrupt state; each must be rejected with a clean 4xx, and
// the exchange must keep taking good orders afterwards. Runs one seeded stack
// through many probes as subtests (shares one scratch DB).

import (
	"math"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Zerith-Studio/prediction-market/backend/internal/models"
	"github.com/Zerith-Studio/prediction-market/backend/internal/templates"
)

func TestAttackVectors(t *testing.T) {
	s := newStack(t)
	ctx := t.Context()
	const fixture = "attack"
	if err := s.lc.RegisterFixture(ctx, fixture, "ATK", "DEF", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	market := templates.MarketID(fixture, "home_win")
	marketHex := models.HashString(market)

	funded := newWallet(t)
	s.post(t, "/wallet/deposit", map[string]any{"wallet": funded.b58, "amount": 1_000_000_000})

	// helper: submit a raw order DTO, return status
	postOrder := func(dto map[string]any) int {
		resp, _ := s.post(t, "/orders", dto)
		return resp.StatusCode
	}

	t.Run("malformed JSON body", func(t *testing.T) {
		resp, err := http.Post(s.srv.URL+"/orders", "application/json", strings.NewReader("{not json"))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		assertStatus(t, resp.StatusCode, http.StatusBadRequest)
	})

	t.Run("bad base58 maker", func(t *testing.T) {
		d := signedOrderDTO(funded, market, models.OutcomeYes, models.SideBuy, 50, 10, 1)
		d["maker"] = "not-base58-!!!"
		assertStatus(t, postOrder(d), http.StatusBadRequest)
	})

	t.Run("bad hex market id", func(t *testing.T) {
		d := signedOrderDTO(funded, market, models.OutcomeYes, models.SideBuy, 50, 10, 2)
		d["market_id"] = "zzzz"
		assertStatus(t, postOrder(d), http.StatusBadRequest)
	})

	t.Run("bad hex signature", func(t *testing.T) {
		d := signedOrderDTO(funded, market, models.OutcomeYes, models.SideBuy, 50, 10, 3)
		d["sig"] = "xyz"
		assertStatus(t, postOrder(d), http.StatusBadRequest)
	})

	t.Run("forged signature (all zero)", func(t *testing.T) {
		d := signedOrderDTO(funded, market, models.OutcomeYes, models.SideBuy, 50, 10, 4)
		d["sig"] = strings.Repeat("00", 64)
		assertStatus(t, postOrder(d), http.StatusUnauthorized)
	})

	t.Run("tampered field after signing", func(t *testing.T) {
		// Sign for price 50, then submit price 51 — sig must fail to verify.
		d := signedOrderDTO(funded, market, models.OutcomeYes, models.SideBuy, 50, 10, 5)
		d["price"] = 51
		assertStatus(t, postOrder(d), http.StatusUnauthorized)
	})

	t.Run("wrong market than signed", func(t *testing.T) {
		other := templates.MarketID(fixture, "draw")
		d := signedOrderDTO(funded, market, models.OutcomeYes, models.SideBuy, 50, 10, 6)
		d["market_id"] = models.HashString(other) // sig covered `market`, not `other`
		assertStatus(t, postOrder(d), http.StatusUnauthorized)
	})

	t.Run("insufficient funds", func(t *testing.T) {
		broke := newWallet(t) // never deposited
		d := signedOrderDTO(broke, market, models.OutcomeYes, models.SideBuy, 60, 100, 7)
		assertStatus(t, postOrder(d), http.StatusPaymentRequired)
	})

	t.Run("naked short (sell without tokens)", func(t *testing.T) {
		d := signedOrderDTO(funded, market, models.OutcomeYes, models.SideSell, 70, 50, 8)
		assertStatus(t, postOrder(d), http.StatusPaymentRequired)
	})

	t.Run("price 0", func(t *testing.T) {
		d := signedOrderDTO(funded, market, models.OutcomeYes, models.SideBuy, 0, 10, 9)
		if code := postOrder(d); code == http.StatusOK || code >= 500 {
			t.Errorf("price 0 got %d, want a 4xx rejection", code)
		}
	})

	t.Run("price 100", func(t *testing.T) {
		d := signedOrderDTO(funded, market, models.OutcomeYes, models.SideBuy, 100, 10, 10)
		if code := postOrder(d); code == http.StatusOK || code >= 500 {
			t.Errorf("price 100 got %d, want a 4xx rejection", code)
		}
	})

	t.Run("size 0", func(t *testing.T) {
		d := signedOrderDTO(funded, market, models.OutcomeYes, models.SideBuy, 50, 0, 11)
		if code := postOrder(d); code == http.StatusOK || code >= 500 {
			t.Errorf("size 0 got %d, want a 4xx rejection", code)
		}
	})

	t.Run("overflow size (lock wraparound exploit)", func(t *testing.T) {
		// price·size·MicroPerCent chosen to wrap uint64 near zero: an unguarded
		// BuyCost would lock ~nothing for a colossal order that sweeps the book.
		exploit := uint64(math.MaxUint64)/(60*models.MicroPerCent) + 1
		d := signedOrderDTO(funded, market, models.OutcomeYes, models.SideBuy, 60, exploit, 12)
		if code := postOrder(d); code == http.StatusOK || code >= 500 {
			t.Errorf("overflow size got %d, want a 4xx rejection (lock underflow exploit)", code)
		}
		// The funded wallet must not have leaked a huge lock.
		b, _ := s.st.GetBalance(ctx, funded.b58)
		if b.Locked > 1_000_000_000 {
			t.Errorf("overflow order locked %d micro from a $1000 wallet — underflow exploit landed", b.Locked)
		}
	})

	t.Run("replay same order twice", func(t *testing.T) {
		d := signedOrderDTO(funded, market, models.OutcomeYes, models.SideBuy, 30, 10, 13)
		if code := postOrder(d); code != http.StatusOK {
			t.Fatalf("first submit: %d", code)
		}
		assertStatus(t, postOrder(d), http.StatusConflict)
	})

	t.Run("cancel with no maker param", func(t *testing.T) {
		resp, err := http.NewRequest(http.MethodDelete, s.srv.URL+"/orders/"+strings.Repeat("ab", 32), nil)
		if err != nil {
			t.Fatal(err)
		}
		r, err := http.DefaultClient.Do(resp)
		if err != nil {
			t.Fatal(err)
		}
		r.Body.Close()
		assertStatus(t, r.StatusCode, http.StatusBadRequest)
	})

	t.Run("cancel non-existent order", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodDelete,
			s.srv.URL+"/orders/"+strings.Repeat("cd", 32)+"?maker="+funded.b58, nil)
		r, _ := http.DefaultClient.Do(req)
		r.Body.Close()
		assertStatus(t, r.StatusCode, http.StatusNotFound)
	})

	t.Run("cancel another wallet's order", func(t *testing.T) {
		// funded rests an order; a stranger tries to cancel it.
		d := signedOrderDTO(funded, market, models.OutcomeYes, models.SideBuy, 20, 10, 14)
		resp, out := s.post(t, "/orders", d)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("rest order: %d", resp.StatusCode)
		}
		hash := out["order_hash"].(string)
		stranger := newWallet(t)
		req, _ := http.NewRequest(http.MethodDelete,
			s.srv.URL+"/orders/"+hash+"?maker="+stranger.b58, nil)
		r, _ := http.DefaultClient.Do(req)
		r.Body.Close()
		assertStatus(t, r.StatusCode, http.StatusNotFound) // not "your" order
	})

	t.Run("unknown market book", func(t *testing.T) {
		code, _ := s.get(t, "/markets/"+strings.Repeat("ff", 32)+"/book")
		assertStatus(t, code, http.StatusNotFound)
	})

	t.Run("bad hex market path", func(t *testing.T) {
		code, _ := s.get(t, "/markets/nothex/book")
		assertStatus(t, code, http.StatusBadRequest)
	})

	t.Run("deposit zero amount", func(t *testing.T) {
		resp, _ := s.post(t, "/wallet/deposit", map[string]any{"wallet": funded.b58, "amount": 0})
		assertStatus(t, resp.StatusCode, http.StatusBadRequest)
	})

	t.Run("deposit bad wallet", func(t *testing.T) {
		resp, _ := s.post(t, "/wallet/deposit", map[string]any{"wallet": "!!!", "amount": 100})
		assertStatus(t, resp.StatusCode, http.StatusBadRequest)
	})

	// --- precision attacks ---
	pool := templates.MarketID(fixture, "precision_total_goals")
	poolHex := models.HashString(pool)

	t.Run("precision double entry", func(t *testing.T) {
		p := newWallet(t)
		s.post(t, "/wallet/deposit", map[string]any{"wallet": p.b58, "amount": 50_000_000})
		if resp, _ := s.post(t, "/markets/"+poolHex+"/precision",
			map[string]any{"wallet": p.b58, "guess": 3, "stake": 2_000_000}); resp.StatusCode != http.StatusOK {
			t.Fatalf("first entry: %d", resp.StatusCode)
		}
		resp, _ := s.post(t, "/markets/"+poolHex+"/precision",
			map[string]any{"wallet": p.b58, "guess": 4, "stake": 1_000_000})
		assertStatus(t, resp.StatusCode, http.StatusConflict)
	})

	t.Run("precision bad wallet", func(t *testing.T) {
		resp, _ := s.post(t, "/markets/"+poolHex+"/precision",
			map[string]any{"wallet": "!!!", "guess": 3, "stake": 1_000_000})
		assertStatus(t, resp.StatusCode, http.StatusBadRequest)
	})

	// --- combo attacks ---
	t.Run("combo with one leg", func(t *testing.T) {
		resp, _ := s.post(t, "/combos", map[string]any{
			"taker": funded.b58,
			"legs":  []map[string]any{{"market_id": marketHex, "outcome": 1}},
			"stake": 5_000_000,
		})
		assertStatus(t, resp.StatusCode, http.StatusBadRequest)
	})

	t.Run("combo with mutex-conflicting legs", func(t *testing.T) {
		resp, _ := s.post(t, "/combos", map[string]any{
			"taker": funded.b58,
			"legs": []map[string]any{
				{"market_id": models.HashString(templates.MarketID(fixture, "home_win")), "outcome": 1},
				{"market_id": models.HashString(templates.MarketID(fixture, "draw")), "outcome": 1},
			},
			"stake": 5_000_000,
		})
		assertStatus(t, resp.StatusCode, http.StatusBadRequest)
	})

	t.Run("accept non-existent quote", func(t *testing.T) {
		resp, _ := s.post(t, "/combos/00000000-0000-0000-0000-000000000000/accept",
			map[string]any{"quote_hash": strings.Repeat("aa", 32), "taker": funded.b58})
		if resp.StatusCode >= 500 {
			t.Errorf("accept unknown quote → %d, want 4xx", resp.StatusCode)
		}
	})

	// --- the exchange still works after the whole assault ---
	t.Run("good order still accepted after assault", func(t *testing.T) {
		seller := newWallet(t)
		s.st.GrantTokens(ctx, seller.b58, market, 20, 0)
		if _, _, err := s.ex.SubmitOrder(ctx, signedModelOrder(seller, market, models.OutcomeYes, models.SideSell, 55, 20, 900)); err != nil {
			t.Fatalf("seed sell: %v", err)
		}
		d := signedOrderDTO(funded, market, models.OutcomeYes, models.SideBuy, 60, 20, 901)
		resp, out := s.post(t, "/orders", d)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("good order after assault got %d", resp.StatusCode)
		}
		if fills := out["fills"].([]any); len(fills) != 1 {
			t.Errorf("expected the good order to fill, got %v", out)
		}
	})
}

func assertStatus(t *testing.T, got, want int) {
	t.Helper()
	if got != want {
		t.Errorf("status = %d, want %d", got, want)
	}
}

// signedModelOrder builds+signs an Order (for direct exchange.SubmitOrder use).
func signedModelOrder(w wallet, marketID [32]byte, outcome, side uint8, price uint16, size, salt uint64) *models.Order {
	o := &models.Order{
		Maker: w.pk, MarketID: marketID, Outcome: outcome, Side: side,
		Price: price, Size: size, Salt: salt,
	}
	models.SignOrder(o, w.priv)
	return o
}
