// Command server runs the E2 backend: REST + WS API, matching engine, Postgres
// mirror, crank, MM bot, RFQ, precision pools, feed-driven match lifecycle,
// and the optional Claude one-liner ticker.
//
// Config (env, with .env fallback at the repo root):
//
//	DATABASE_URL       required — Postgres (Neon)
//	PITCHMARKET_ADDR   listen address (default :8080)
//	SOLANA_RPC_URL     enable on-chain settlement + index (needs OPERATOR_KEYPAIR)
//	OPERATOR_KEYPAIR   path to the operator's solana keypair JSON
//	USDC_MINT          devnet USDC mint (default 4zMMC9srt5Ri5X14GAgXhaHii3GnPAEERYPJgZJDncDU)
//	PROGRAM_ID         pitchmarket program (default 3fdgRPcZnwWcaGi197dkZDyq24VHoWJcGzKTVfMxNPWs)
//	FEED_PROVIDER      replay | txodds (default replay)
//	REPLAY_DIR         recorded fixtures dir (default ./fixtures)
//	REPLAY_SPEED       playback compression (default 60 = 90-min match in ~90s)
//	TXODDS_URL/TXODDS_API_KEY  live feed credentials
//	DEMO_FIXTURE       fixture id to auto-register and stream at boot
//	MM_BOT             "off" disables the market maker (default on)
//	ANTHROPIC_API_KEY  enables the one-liner ticker
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

	// --- settlement path: RPC when configured, honest off-chain mirror otherwise
	var submitter crank.Submitter = crank.OffchainSubmitter{}
	programID := solana.MustPublicKeyFromBase58(envOr("PROGRAM_ID", "3fdgRPcZnwWcaGi197dkZDyq24VHoWJcGzKTVfMxNPWs"))
	usdcMint := solana.MustPublicKeyFromBase58(envOr("USDC_MINT", "4zMMC9srt5Ri5X14GAgXhaHii3GnPAEERYPJgZJDncDU"))
	rpcURL := os.Getenv("SOLANA_RPC_URL")
	if rpcURL != "" {
		keyPath := os.Getenv("OPERATOR_KEYPAIR")
		operator, err := solana.PrivateKeyFromSolanaKeygenFile(keyPath)
		if err != nil {
			return errors.New("SOLANA_RPC_URL set but OPERATOR_KEYPAIR unreadable: " + err.Error())
		}
		builder := &crank.TxBuilder{ProgramID: programID, USDCMint: usdcMint}
		submitter = crank.NewRPCSubmitter(rpcURL, builder, operator)
		log.Info("crank: on-chain settlement enabled", "rpc", rpcURL, "operator", operator.PublicKey())
	} else {
		log.Warn("crank: SOLANA_RPC_URL unset — fills settle in the mirror only (no on-chain txs)")
	}

	ex := exchange.New(st, hub, submitter, log)
	if err := ex.RestoreBooks(ctx); err != nil {
		return err
	}

	rfqSvc := rfq.New(st, hub, nil, log)

	// --- MM bot (fair-price quoting, RFQ answers, crowd seeding)
	var bot *mmbot.Bot
	var priceSink lifecycle.FairPriceSink
	if os.Getenv("MM_BOT") != "off" {
		_, priv, err := ed25519.GenerateKey(nil)
		if err != nil {
			return err
		}
		bot = mmbot.New(ex, st, rfqSvc, priv, rand.New(rand.NewSource(time.Now().UnixNano())), log)
		if err := bot.Fund(ctx, 100_000_000_000); err != nil { // 100k demo USDC
			return err
		}
		priceSink = bot
		go bot.PollRFQs(ctx, 2*time.Second)
		log.Info("mmbot: running", "wallet", bot.Wallet())
	}

	lc := lifecycle.New(st, hub, rfqSvc, nil, priceSink, log)

	// --- optional Claude one-liner ticker
	if key := os.Getenv("ANTHROPIC_API_KEY"); key != "" {
		ol := oneliner.New(st, hub, oneliner.NewClaude(key), log)
		go ol.Run(ctx)
		log.Info("oneliner: ticker running")
	}

	// --- optional chain index (mirror reconciliation)
	if rpcURL != "" {
		poller := index.NewRPCPoller(rpcURL, programID, log)
		proc := index.NewProcessor(st, log)
		go func() {
			if err := proc.Run(ctx, poller); err != nil && ctx.Err() == nil {
				log.Error("index: stopped", "err", err)
			}
		}()
	}

	// --- feed-driven demo match
	if fixture := os.Getenv("DEMO_FIXTURE"); fixture != "" {
		provider, err := selectFeed(log)
		if err != nil {
			return err
		}
		go runDemoFixture(ctx, lc, bot, provider, fixture, log)
	}

	srv := &http.Server{
		Addr:    envOr("PITCHMARKET_ADDR", ":8080"),
		Handler: api.WithCORS(api.New(ex, st, hub, rfqSvc, lc, log).Routes(), os.Getenv("CORS_ORIGIN")),
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

func selectFeed(log *slog.Logger) (feed.FeedProvider, error) {
	switch envOr("FEED_PROVIDER", "replay") {
	case "txodds":
		return txodds.New(os.Getenv("TXODDS_URL"), os.Getenv("TXODDS_API_KEY"))
	default:
		speed, _ := strconv.ParseFloat(envOr("REPLAY_SPEED", "60"), 64)
		return replay.New(envOr("REPLAY_DIR", "fixtures"), speed), nil
	}
}

// runDemoFixture registers the fixture (kickoff two minutes out so precision
// entries are open on camera), seeds the pools, and streams the feed.
func runDemoFixture(ctx context.Context, lc *lifecycle.Service, bot *mmbot.Bot,
	provider feed.FeedProvider, fixtureID string, log *slog.Logger) {
	home, away := envOr("DEMO_HOME", "Argentina"), envOr("DEMO_AWAY", "France")
	if err := lc.RegisterFixture(ctx, fixtureID, home, away, time.Now().Add(2*time.Minute)); err != nil {
		log.Error("demo: register fixture", "err", err)
		return
	}
	if bot != nil {
		// Crowd-seed the pools pre-kickoff (ADR 0006 C3): believable bell curves.
		seed := []struct {
			key         string
			fair, sigma float64
		}{
			{"precision_total_goals", 2.6, 1.3},
			{"precision_total_passes", 900, 120},
		}
		for _, s := range seed {
			if _, err := bot.SeedPrecision(ctx, templates.MarketID(fixtureID, s.key),
				s.fair, s.sigma, 25, 500_000, 5_000_000); err != nil {
				log.Error("demo: seed precision", "pool", s.key, "err", err)
			}
		}
	}
	if err := lc.RunFeed(ctx, provider, fixtureID); err != nil && ctx.Err() == nil {
		log.Error("demo: feed", "err", err)
	}
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
