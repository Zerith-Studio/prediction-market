// Command settle-backlog inspects (and, with -mode=apply, settles) the fills
// that never got an on-chain settle_tx — the "settling…" rows in the portfolio
// left behind when the exchange ran in off-chain mirror mode.
//
// It reconstructs each unsettled fill from the orders table and, per -mode:
//
//	check    (default) read-only: derive the market/pool/vault PDAs and report
//	         which exist on devnet. Nothing is sent. This tells us whether the
//	         fills CAN be settled (mirror-only markets have no on-chain accounts).
//	apply    build + send the real settle_match tx via the same RPCSubmitter the
//	         server uses; on confirm, write settle_tx back. Reverts are harmless
//	         (no state change). Use only after `check` looks good.
//
// Config comes from the repo-root .env (DATABASE_URL, SOLANA_RPC_URL,
// OPERATOR_KEYPAIR, PROGRAM_ID, USDC_MINT) exactly like cmd/server.
package main

import (
	"bufio"
	"context"
	"encoding/hex"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Zerith-Studio/prediction-market/backend/internal/crank"
	"github.com/Zerith-Studio/prediction-market/backend/internal/matching"
	"github.com/Zerith-Studio/prediction-market/backend/internal/models"
)

type fillRow struct {
	id                 string
	marketID           [32]byte
	taker, maker       *models.Order
	takerRem, makerRem uint64
	price              uint16
	size               uint64
	matchType          models.MatchType
	ts                 time.Time
}

func main() {
	mode := flag.String("mode", "check", "check | apply")
	limit := flag.Int("limit", 0, "max fills to process (0 = all)")
	flag.Parse()
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	loadDotEnv("../.env")
	dbURL := os.Getenv("DATABASE_URL")
	rpcURL := os.Getenv("SOLANA_RPC_URL")
	if dbURL == "" || rpcURL == "" {
		log.Error("need DATABASE_URL and SOLANA_RPC_URL in ../.env")
		os.Exit(1)
	}
	programID := solana.MustPublicKeyFromBase58(envOr("PROGRAM_ID", "3fdgRPcZnwWcaGi197dkZDyq24VHoWJcGzKTVfMxNPWs"))

	ctx := context.Background()
	pool := mustDB(ctx, dbURL)
	defer pool.Close()
	client := rpc.New(rpcURL)

	fills, err := loadUnsettled(ctx, pool, *limit)
	if err != nil {
		log.Error("load unsettled fills", "err", err)
		os.Exit(1)
	}
	fmt.Printf("\n%d unsettled fill(s) (settle_tx empty)\n\n", len(fills))
	if len(fills) == 0 {
		return
	}

	switch *mode {
	case "check":
		checkOnChain(ctx, client, programID, fills)
	case "apply":
		applySettle(ctx, log, pool, client, programID, fills)
	default:
		log.Error("unknown -mode", "mode", *mode)
		os.Exit(1)
	}
}

// ---- check: read-only PDA existence probe -----------------------------------

func checkOnChain(ctx context.Context, client *rpc.Client, programID solana.PublicKey, fills []fillRow) {
	pda := func(seeds ...[]byte) solana.PublicKey {
		a, _, _ := solana.FindProgramAddress(seeds, programID)
		return a
	}
	for _, f := range fills {
		market := pda([]byte("market"), f.marketID[:])
		poolAcct := pda([]byte("pool"), f.marketID[:])
		takerVault := pda([]byte("vault"), f.taker.Maker[:])
		makerVault := pda([]byte("vault"), f.maker.Maker[:])
		keys := solana.PublicKeySlice{market, poolAcct, takerVault, makerVault}

		res, err := client.GetMultipleAccounts(ctx, keys...)
		exists := make([]bool, len(keys))
		if err == nil {
			for i, a := range res.Value {
				exists[i] = a != nil
			}
		}
		mark := func(b bool) string {
			if b {
				return "✓"
			}
			return "✗"
		}
		fmt.Printf("fill %s  %s  %d@%d¢  taker %s / maker %s\n",
			f.id[:8], matchTypeString(f.matchType), f.size, f.price,
			models.PubkeyString(f.taker.Maker)[:6], models.PubkeyString(f.maker.Maker)[:6])
		fmt.Printf("    market %s  pool %s  takerVault %s  makerVault %s",
			mark(exists[0]), mark(exists[1]), mark(exists[2]), mark(exists[3]))
		if err != nil {
			fmt.Printf("   (rpc err: %v)", err)
		}
		settleable := exists[0] && exists[1] && exists[2] && exists[3]
		if settleable {
			fmt.Printf("   → SETTLEABLE\n\n")
		} else {
			fmt.Printf("   → NOT settleable (missing on-chain accounts — mirror-only)\n\n")
		}
	}
}

// ---- apply: real settlement via the server's RPCSubmitter -------------------

func applySettle(ctx context.Context, log *slog.Logger, pool *pgxpool.Pool, client *rpc.Client, programID solana.PublicKey, fills []fillRow) {
	keyPath := os.Getenv("OPERATOR_KEYPAIR")
	operator, err := solana.PrivateKeyFromSolanaKeygenFile(keyPath)
	if err != nil {
		log.Error("OPERATOR_KEYPAIR unreadable", "err", err)
		os.Exit(1)
	}
	usdcMint := os.Getenv("USDC_MINT")
	if usdcMint == "" {
		if b, e := os.ReadFile("../.pitchmarket-usdc"); e == nil {
			usdcMint = strings.TrimSpace(string(b))
		}
	}
	if usdcMint == "" {
		log.Error("USDC_MINT unset and ../.pitchmarket-usdc missing")
		os.Exit(1)
	}
	builder := &crank.TxBuilder{ProgramID: programID, USDCMint: solana.MustPublicKeyFromBase58(usdcMint)}
	sub := crank.NewRPCSubmitter(rpcURLFromEnv(), builder, operator)
	sub.Client = client
	sub.Chain = crank.NewChainOps(client, builder, operator, log)

	var ok, reverted int
	for _, f := range fills {
		fill := matching.Fill{
			MarketID:  f.marketID,
			Taker:     &matching.RestingOrder{Order: f.taker, Hash: models.OrderHash(f.taker), Remaining: f.takerRem},
			Maker:     &matching.RestingOrder{Order: f.maker, Hash: models.OrderHash(f.maker), Remaining: f.makerRem},
			Price:     f.price,
			Size:      f.size,
			MatchType: f.matchType,
		}
		sig, err := sub.SettleMatch(ctx, fill)
		if err != nil {
			reverted++
			log.Warn("reverted — left as settling", "fill", f.id[:8], "err", err)
			continue
		}
		if _, err := pool.Exec(ctx, `UPDATE fills SET settle_tx=$2 WHERE id=$1`, f.id, sig); err != nil {
			log.Error("settled on-chain but DB write failed", "fill", f.id[:8], "tx", sig, "err", err)
			continue
		}
		ok++
		log.Info("SETTLED", "fill", f.id[:8], "tx", sig)
	}
	fmt.Printf("\ndone: %d settled, %d reverted\n", ok, reverted)
}

// ---- reconstruction ---------------------------------------------------------

func loadUnsettled(ctx context.Context, pool *pgxpool.Pool, limit int) ([]fillRow, error) {
	q := `
		SELECT f.id, encode(f.market_id,'hex'), f.price, f.size, f.match_type, f.ts,
		       encode(o1.market_id,'hex'), o1.maker, o1.outcome, o1.side, o1.price, o1.size, o1.fee_bps, o1.expiry, o1.salt, o1.sig, o1.remaining,
		       encode(o2.market_id,'hex'), o2.maker, o2.outcome, o2.side, o2.price, o2.size, o2.fee_bps, o2.expiry, o2.salt, o2.sig, o2.remaining
		FROM fills f
		JOIN orders o1 ON o1.order_hash = f.taker_hash
		JOIN orders o2 ON o2.order_hash = f.maker_hash
		WHERE f.settle_tx IS NULL OR f.settle_tx = ''
		ORDER BY f.ts`
	rows, err := pool.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []fillRow
	for rows.Next() {
		var (
			fr           fillRow
			fMktHex, mt  string
			fPrice       int16
			fSize        int64
			tMktHex, tMk string
			tOut, tSide  int16
			tPrice       int16
			tSize, tFee  int64
			tExpiry      *time.Time
			tSalt, tRem  int64
			tSig         []byte
			mMktHex, mMk string
			mOut, mSide  int16
			mPrice       int16
			mSize, mFee  int64
			mExpiry      *time.Time
			mSalt, mRem  int64
			mSig         []byte
		)
		if err := rows.Scan(&fr.id, &fMktHex, &fPrice, &fSize, &mt, &fr.ts,
			&tMktHex, &tMk, &tOut, &tSide, &tPrice, &tSize, &tFee, &tExpiry, &tSalt, &tSig, &tRem,
			&mMktHex, &mMk, &mOut, &mSide, &mPrice, &mSize, &mFee, &mExpiry, &mSalt, &mSig, &mRem); err != nil {
			return nil, err
		}
		fr.marketID = must32(fMktHex)
		fr.matchType = parseMatchType(mt)
		fr.price = uint16(fPrice)
		fr.size = uint64(fSize)
		fr.taker = mkOrder(tMk, tMktHex, tOut, tSide, tPrice, tSize, tFee, tExpiry, tSalt, tSig)
		fr.maker = mkOrder(mMk, mMktHex, mOut, mSide, mPrice, mSize, mFee, mExpiry, mSalt, mSig)
		fr.takerRem = uint64(tRem)
		fr.makerRem = uint64(mRem)
		out = append(out, fr)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, rows.Err()
}

func mkOrder(maker, mktHex string, outcome, side, price int16, size, fee int64, expiry *time.Time, salt int64, sig []byte) *models.Order {
	o := &models.Order{
		Maker:    [32]byte(solana.MustPublicKeyFromBase58(maker)),
		MarketID: must32(mktHex),
		Outcome:  uint8(outcome),
		Side:     uint8(side),
		Price:    uint16(price),
		Size:     uint64(size),
		FeeBps:   uint16(fee),
		Salt:     uint64(salt), // BIGINT is signed; bit-pattern preserved
	}
	if expiry != nil {
		o.Expiry = expiry.Unix()
	}
	copy(o.Sig[:], sig)
	return o
}

// ---- helpers ----------------------------------------------------------------

func mustDB(ctx context.Context, url string) *pgxpool.Pool {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		panic(err)
	}
	cfg.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
	p, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		panic(err)
	}
	if err := p.Ping(ctx); err != nil {
		panic(err)
	}
	return p
}

func matchTypeString(m models.MatchType) string {
	switch m {
	case models.MatchMint:
		return "MINT"
	case models.MatchMerge:
		return "MERGE"
	default:
		return "NORMAL"
	}
}

func parseMatchType(s string) models.MatchType {
	switch s {
	case "MINT":
		return models.MatchMint
	case "MERGE":
		return models.MatchMerge
	default:
		return models.MatchNormal
	}
}

func must32(h string) [32]byte {
	b, err := hex.DecodeString(h)
	if err != nil || len(b) != 32 {
		panic("bad 32-byte hex: " + h)
	}
	var a [32]byte
	copy(a[:], b)
	return a
}

func rpcURLFromEnv() string { return os.Getenv("SOLANA_RPC_URL") }

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func loadDotEnv(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k, v = strings.TrimSpace(k), strings.Trim(strings.TrimSpace(v), `"'`)
		if os.Getenv(k) == "" {
			os.Setenv(k, v)
		}
	}
}
