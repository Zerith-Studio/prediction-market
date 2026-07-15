// Command server runs the E2 backend: REST + WS API, matching engine, Postgres
// mirror, crank, MM bot, RFQ, precision pools, feed-driven match lifecycle,
// and the optional Claude one-liner ticker.
//
// Config (env, with .env fallback at the repo root):
//
//	DATABASE_URL       required — Postgres (Neon)
//	PITCHMARKET_ADDR   listen address (default :8080)
//	SOLANA_RPC_URL     enable ON-CHAIN mode: markets created/resolved on devnet,
//	                   fills settled by the crank, real deposits (needs OPERATOR_KEYPAIR)
//	OPERATOR_KEYPAIR   path to the operator's solana keypair JSON
//	USDC_MINT          demo USDC mint (auto-created once and cached to
//	                   .pitchmarket-usdc when unset in on-chain mode)
//	PROGRAM_ID         pitchmarket program (default 3fdgRPcZnwWcaGi197dkZDyq24VHoWJcGzKTVfMxNPWs)
//	FEED_PROVIDER      txodds | replay (default txodds when OPERATOR_KEYPAIR is set)
//	TXODDS_URL         TxLINE server (default https://txline-dev.txodds.com)
//	TXODDS_COMPETITION competition filter for fixture discovery (default 72 = World Cup)
//	TXODDS_CACHE       credential cache path (default .txline-credentials.json)
//	REPLAY_DIR/REPLAY_SPEED/DEMO_FIXTURE  replay mode knobs
//	MM_BOT             "off" disables the market maker (default on)
//	ANTHROPIC_API_KEY  enables the one-liner ticker
//	CORS_ORIGIN        pin the browser origin (default: reflect any)
package main

import (
	"bufio"
	"context"
	"crypto/ed25519"
	"errors"
	"log/slog"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/programs/system"
	"github.com/gagliardetto/solana-go/programs/token"
	"github.com/gagliardetto/solana-go/rpc"

	"github.com/Zerith-Studio/prediction-market/backend/internal/api"
	"github.com/Zerith-Studio/prediction-market/backend/internal/crank"
	"github.com/Zerith-Studio/prediction-market/backend/internal/exchange"
	"github.com/Zerith-Studio/prediction-market/backend/internal/feed"
	"github.com/Zerith-Studio/prediction-market/backend/internal/feed/replay"
	"github.com/Zerith-Studio/prediction-market/backend/internal/feed/txodds"
	"github.com/Zerith-Studio/prediction-market/backend/internal/index"
	"github.com/Zerith-Studio/prediction-market/backend/internal/lifecycle"
	"github.com/Zerith-Studio/prediction-market/backend/internal/mmbot"
	"github.com/Zerith-Studio/prediction-market/backend/internal/oneliner"
	"github.com/Zerith-Studio/prediction-market/backend/internal/rfq"
	"github.com/Zerith-Studio/prediction-market/backend/internal/store"
	"github.com/Zerith-Studio/prediction-market/backend/internal/templates"
	"github.com/Zerith-Studio/prediction-market/backend/internal/ws"
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	if err := run(log); err != nil {
		log.Error("server exited", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	loadDotEnv()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		return errors.New("DATABASE_URL is required (see .env.example)")
	}
	st, err := store.Open(ctx, dbURL)
	if err != nil {
		return err
	}
	defer st.Close()
	if err := st.Bootstrap(ctx); err != nil {
		return err
	}
	log.Info("postgres ready")

	hub := ws.NewHub(log)

	// --- on-chain mode: real settlement, market lifecycle, and deposits
	var (
		submitter crank.Submitter = crank.OffchainSubmitter{}
		chainOps  *crank.ChainOps
		operator  solana.PrivateKey
		rpcURL    = os.Getenv("SOLANA_RPC_URL")
	)
	programID := solana.MustPublicKeyFromBase58(envOr("PROGRAM_ID", "3fdgRPcZnwWcaGi197dkZDyq24VHoWJcGzKTVfMxNPWs"))
	if keyPath := os.Getenv("OPERATOR_KEYPAIR"); keyPath != "" {
		operator, err = solana.PrivateKeyFromSolanaKeygenFile(keyPath)
		if err != nil {
			return errors.New("OPERATOR_KEYPAIR unreadable: " + err.Error())
		}
	}
	if rpcURL != "" {
		if operator == nil {
			return errors.New("SOLANA_RPC_URL set but OPERATOR_KEYPAIR missing")
		}
		client := rpc.New(rpcURL)
		usdcMint, err := ensureUSDCMint(ctx, client, operator, os.Getenv("USDC_MINT"), log)
		if err != nil {
			return err
		}
		builder := &crank.TxBuilder{ProgramID: programID, USDCMint: usdcMint}
		chainOps = crank.NewChainOps(client, builder, operator, log)
		rpcSub := crank.NewRPCSubmitter(rpcURL, builder, operator)
		rpcSub.Client = client
		rpcSub.Chain = chainOps
		submitter = rpcSub
		log.Info("ON-CHAIN mode", "rpc", rpcURL, "operator", operator.PublicKey(), "usdc_mint", usdcMint)
	} else {
		log.Warn("off-chain mirror mode — set SOLANA_RPC_URL + OPERATOR_KEYPAIR for real settlement")
	}

	ex := exchange.New(st, hub, submitter, log)
	if err := ex.RestoreBooks(ctx); err != nil {
		return err
	}

	rfqSvc := rfq.New(st, hub, nil, log)

	// --- MM bot
	var bot *mmbot.Bot
	var priceSink lifecycle.FairPriceSink
	if os.Getenv("MM_BOT") != "off" {
		_, priv, err := ed25519.GenerateKey(nil)
		if err != nil {
			return err
		}
		bot = mmbot.New(ex, st, rfqSvc, priv, rand.New(rand.NewSource(time.Now().UnixNano())), log)
		const botBank = uint64(100_000_000_000) // 100k demo USDC
		if chainOps != nil {
			// The bot trades on-chain like everyone else: real vault, real USDC.
			if tx, err := chainOps.DepositWithKey(ctx, solana.PrivateKey(priv), botBank); err != nil {
				return errors.New("bot on-chain deposit: " + err.Error())
			} else {
				log.Info("mmbot: on-chain vault funded", "tx", tx)
			}
		}
		if err := bot.Fund(ctx, botBank); err != nil {
			return err
		}
		priceSink = bot
		go bot.PollRFQs(ctx, 2*time.Second)
		log.Info("mmbot: running", "wallet", bot.Wallet())
	}

	lc := lifecycle.New(st, hub, rfqSvc, chainResolver(chainOps), priceSink, log)
	if chainOps != nil {
		lc.Creator = chainOps
	}

	// --- one-liners (Gemini preferred when configured, else Claude)
	var gen oneliner.Generator
	if key := os.Getenv("GEMINI_API_KEY"); key != "" {
		gen = oneliner.NewGemini(key, os.Getenv("GEMINI_MODEL"))
		log.Info("oneliner: ticker running", "model", envOr("GEMINI_MODEL", "gemini-3.0-flash"))
	} else if key := os.Getenv("ANTHROPIC_API_KEY"); key != "" {
		gen = oneliner.NewClaude(key)
		log.Info("oneliner: ticker running", "model", "claude-haiku-4-5")
	}
	if gen != nil {
		go oneliner.New(st, hub, gen, log).Run(ctx)
	}

	// --- chain index (mirror reconciliation)
	if rpcURL != "" {
		poller := index.NewRPCPoller(rpcURL, programID, log)
		proc := index.NewProcessor(st, log)
		go func() {
			if err := proc.Run(ctx, poller); err != nil && ctx.Err() == nil {
				log.Error("index: stopped", "err", err)
			}
		}()
	}

	// --- feed: TxLINE (real) or replay (recorded)
	switch feedMode(operator) {
	case "txodds":
		provider, err := txodds.New(
			envOr("TXODDS_URL", txodds.DevNetBase),
			envOr("SOLANA_RPC_URL", rpc.DevNet_RPC),
			envOr("TXODDS_CACHE", ".txline-credentials.json"),
			operator, log)
		if err != nil {
			return errors.New("txodds provider: " + err.Error())
		}
		comp, _ := strconv.Atoi(envOr("TXODDS_COMPETITION", "72"))
		go discoverFixtures(ctx, provider, lc, bot, comp, log)
		log.Info("feed: TxLINE live", "competition", comp)
	case "replay":
		if fixture := os.Getenv("DEMO_FIXTURE"); fixture != "" {
			speed, _ := strconv.ParseFloat(envOr("REPLAY_SPEED", "60"), 64)
			provider := replay.New(envOr("REPLAY_DIR", "fixtures"), speed)
			go runReplayFixture(ctx, lc, bot, provider, fixture, log)
			log.Info("feed: replay", "fixture", fixture)
		}
	}

	srv := &http.Server{
		Addr: envOr("PITCHMARKET_ADDR", ":8080"),
		Handler: api.WithCORS(
			api.New(ex, st, hub, rfqSvc, lc, log).WithChain(chainOps).Routes(),
			os.Getenv("CORS_ORIGIN")),
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()
	log.Info("listening", "addr", srv.Addr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func feedMode(operator solana.PrivateKey) string {
	if v := os.Getenv("FEED_PROVIDER"); v != "" {
		return v
	}
	if operator != nil {
		return "txodds"
	}
	return "replay"
}

func chainResolver(c *crank.ChainOps) lifecycle.ChainResolver {
	if c == nil {
		return nil
	}
	return c
}

// discoverFixtures polls TxLINE for upcoming fixtures in the competition,
// registers each (creating its markets, on-chain when enabled), seeds the
// precision pools pre-kickoff, and streams its feed until full time.
func discoverFixtures(ctx context.Context, provider *txodds.Provider, lc *lifecycle.Service,
	bot *mmbot.Bot, competition int, log *slog.Logger) {
	running := map[int64]bool{}
	tick := time.NewTicker(5 * time.Minute)
	defer tick.Stop()
	for {
		fixtures, err := provider.Fixtures(ctx, competition)
		if err != nil {
			log.Error("txodds: fixture discovery", "err", err)
		}
		for _, f := range fixtures {
			if running[f.ID] {
				continue
			}
			// Cover fixtures from 48h before kickoff until it starts (running
			// feeds carry them through to full time).
			if time.Until(f.Kickoff) > 48*time.Hour || time.Since(f.Kickoff) > 4*time.Hour {
				continue
			}
			running[f.ID] = true
			fixtureID := strconv.FormatInt(f.ID, 10)
			if err := lc.RegisterFixture(ctx, fixtureID, f.Home, f.Away, f.Kickoff); err != nil {
				log.Error("txodds: register fixture", "fixture", fixtureID, "err", err)
				continue
			}
			log.Info("fixture live", "id", fixtureID, "match", f.Home+" vs "+f.Away,
				"kickoff", f.Kickoff.Format(time.RFC3339), "competition", f.Competition)
			if bot != nil && time.Until(f.Kickoff) > 5*time.Minute {
				for _, seed := range []struct {
					key         string
					fair, sigma float64
				}{{"precision_total_goals", 2.6, 1.3}, {"precision_total_passes", 900, 120}} {
					if _, err := bot.SeedPrecision(ctx, templates.MarketID(fixtureID, seed.key),
						seed.fair, seed.sigma, 25, 500_000, 5_000_000); err != nil {
						log.Warn("txodds: seed precision", "pool", seed.key, "err", err)
					}
				}
			}
			go func() {
				if err := lc.RunFeed(ctx, provider, fixtureID); err != nil && ctx.Err() == nil {
					log.Error("txodds: feed", "fixture", fixtureID, "err", err)
				}
			}()
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

func runReplayFixture(ctx context.Context, lc *lifecycle.Service, bot *mmbot.Bot,
	provider feed.FeedProvider, fixtureID string, log *slog.Logger) {
	home, away := envOr("DEMO_HOME", "Argentina"), envOr("DEMO_AWAY", "France")
	if err := lc.RegisterFixture(ctx, fixtureID, home, away, time.Now().Add(2*time.Minute)); err != nil {
		log.Error("replay: register fixture", "err", err)
		return
	}
	if bot != nil {
		for _, s := range []struct {
			key         string
			fair, sigma float64
		}{{"precision_total_goals", 2.6, 1.3}, {"precision_total_passes", 900, 120}} {
			if _, err := bot.SeedPrecision(ctx, templates.MarketID(fixtureID, s.key),
				s.fair, s.sigma, 25, 500_000, 5_000_000); err != nil {
				log.Error("replay: seed precision", "pool", s.key, "err", err)
			}
		}
	}
	if err := lc.RunFeed(ctx, provider, fixtureID); err != nil && ctx.Err() == nil {
		log.Error("replay: feed", "err", err)
	}
}

// ensureUSDCMint returns the demo USDC mint, creating it once (operator is
// mint authority) and caching the address when USDC_MINT is unset.
func ensureUSDCMint(ctx context.Context, client *rpc.Client, operator solana.PrivateKey,
	fromEnv string, log *slog.Logger) (solana.PublicKey, error) {
	if fromEnv != "" {
		return solana.MustPublicKeyFromBase58(fromEnv), nil
	}
	const cacheFile = ".pitchmarket-usdc"
	if raw, err := os.ReadFile(cacheFile); err == nil {
		if pk, err := solana.PublicKeyFromBase58(strings.TrimSpace(string(raw))); err == nil {
			return pk, nil
		}
	}
	mint := solana.NewWallet()
	rentExempt, err := client.GetMinimumBalanceForRentExemption(ctx, token.MINT_SIZE, rpc.CommitmentConfirmed)
	if err != nil {
		return solana.PublicKey{}, err
	}
	ixs := []solana.Instruction{
		system.NewCreateAccountInstruction(rentExempt, token.MINT_SIZE,
			solana.TokenProgramID, operator.PublicKey(), mint.PublicKey()).Build(),
		token.NewInitializeMint2Instruction(6, operator.PublicKey(), solana.PublicKey{},
			mint.PublicKey()).Build(),
	}
	recent, err := client.GetLatestBlockhash(ctx, rpc.CommitmentFinalized)
	if err != nil {
		return solana.PublicKey{}, err
	}
	tx, err := solana.NewTransaction(ixs, recent.Value.Blockhash, solana.TransactionPayer(operator.PublicKey()))
	if err != nil {
		return solana.PublicKey{}, err
	}
	keys := map[solana.PublicKey]solana.PrivateKey{
		operator.PublicKey(): operator,
		mint.PublicKey():     mint.PrivateKey,
	}
	if _, err := tx.Sign(func(key solana.PublicKey) *solana.PrivateKey {
		if k, ok := keys[key]; ok {
			return &k
		}
		return nil
	}); err != nil {
		return solana.PublicKey{}, err
	}
	if _, err := client.SendTransactionWithOpts(ctx, tx, rpc.TransactionOpts{
		PreflightCommitment: rpc.CommitmentConfirmed,
	}); err != nil {
		return solana.PublicKey{}, err
	}
	_ = os.WriteFile(cacheFile, []byte(mint.PublicKey().String()), 0o644)
	log.Info("chain: demo USDC mint created", "mint", mint.PublicKey())
	return mint.PublicKey(), nil
}

// loadDotEnv fills os.Environ from the nearest .env up the tree (dev nicety —
// real deployments set the environment).
func loadDotEnv() {
	dir, _ := os.Getwd()
	for i := 0; i < 6; i++ {
		f, err := os.Open(filepath.Join(dir, ".env"))
		if err == nil {
			sc := bufio.NewScanner(f)
			for sc.Scan() {
				line := strings.TrimSpace(sc.Text())
				if line == "" || strings.HasPrefix(line, "#") {
					continue
				}
				if k, v, ok := strings.Cut(line, "="); ok && os.Getenv(k) == "" {
					os.Setenv(k, strings.Trim(v, `"'`))
				}
			}
			f.Close()
			return
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return
		}
		dir = parent
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
