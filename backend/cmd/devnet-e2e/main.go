// Command devnet-e2e proves the trustless floor on devnet with the REAL Go
// crank (progress.md §5): mock USDC → initialize_market → vaults + deposits →
// two user-signed orders crossed by the REAL matching engine → the crank's v0 +
// lookup-table settle_match → resolve_market → redeem, asserting balances at
// every step. Prints explorer links for the "Verified on Solana" story.
//
//	go run ./cmd/devnet-e2e -keypair ~/.config/solana/id.json
package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/gagliardetto/solana-go"
	ata "github.com/gagliardetto/solana-go/programs/associated-token-account"
	"github.com/gagliardetto/solana-go/programs/system"
	"github.com/gagliardetto/solana-go/programs/token"
	"github.com/gagliardetto/solana-go/rpc"

	"github.com/Zerith-Studio/prediction-market/backend/internal/crank"
	"github.com/Zerith-Studio/prediction-market/backend/internal/matching"
	"github.com/Zerith-Studio/prediction-market/backend/internal/models"
)

const (
	microUSDC   = uint64(1_000_000)
	depositAmt  = 100 * 1_000_000 // 100 USDC into each vault
	fillSize    = uint64(100)     // shares
	yesPrice    = uint16(60)
	noPrice     = uint16(40)
	solPerUser  = uint64(50_000_000) // 0.05 SOL: vault rent + fees
	explorerFmt = "https://explorer.solana.com/tx/%s?cluster=devnet"
)

type env struct {
	ctx      context.Context
	client   *rpc.Client
	builder  *crank.TxBuilder
	operator solana.PrivateKey
}

func main() {
	keypairPath := flag.String("keypair", os.Getenv("HOME")+"/.config/solana/id.json", "operator keypair (fee payer, resolver, mint authority)")
	rpcURL := flag.String("rpc", rpc.DevNet_RPC, "RPC endpoint")
	programID := flag.String("program", "3fdgRPcZnwWcaGi197dkZDyq24VHoWJcGzKTVfMxNPWs", "pitchmarket program id")
	flag.Parse()

	operator, err := solana.PrivateKeyFromSolanaKeygenFile(*keypairPath)
	if err != nil {
		log.Fatalf("load operator keypair: %v", err)
	}
	pid := solana.MustPublicKeyFromBase58(*programID)

	e := &env{
		ctx:      context.Background(),
		client:   rpc.New(*rpcURL),
		operator: operator,
	}
	if err := e.run(pid); err != nil {
		log.Fatalf("devnet-e2e FAILED: %v", err)
	}
	fmt.Println("\n✅ devnet-e2e PASSED — the Go crank settled a real match on devnet.")
}

func (e *env) run(programID solana.PublicKey) error {
	fmt.Printf("operator: %s\n", e.operator.PublicKey())
	bal, err := e.client.GetBalance(e.ctx, e.operator.PublicKey(), rpc.CommitmentConfirmed)
	if err != nil {
		return err
	}
	fmt.Printf("operator balance: %.3f SOL\n", float64(bal.Value)/1e9)
	if bal.Value < 200_000_000 {
		return fmt.Errorf("operator needs at least 0.2 SOL (have %.3f) — fund %s via faucet.solana.com", float64(bal.Value)/1e9, e.operator.PublicKey())
	}

	// --- 1. mock USDC mint (operator is authority; devnet Circle USDC can't be minted by us)
	usdcMint, err := e.createMint()
	if err != nil {
		return fmt.Errorf("create mock USDC: %w", err)
	}
	fmt.Printf("mock USDC mint: %s\n", usdcMint)
	e.builder = &crank.TxBuilder{ProgramID: programID, USDCMint: usdcMint}

	// --- 2. two traders, funded with SOL and wallet USDC
	alice, bob := solana.NewWallet(), solana.NewWallet()
	fmt.Printf("alice (YES buyer): %s\nbob (NO buyer):    %s\n", alice.PublicKey(), bob.PublicKey())
	if err := e.fundUsers(usdcMint, alice, bob); err != nil {
		return fmt.Errorf("fund users: %w", err)
	}

	// --- 3. market
	var marketID [32]byte
	if _, err := rand.Read(marketID[:]); err != nil {
		return err
	}
	initIx, err := e.builder.InitializeMarketIx(marketID, 0, e.operator.PublicKey(), e.operator.PublicKey())
	if err != nil {
		return err
	}
	sig, err := e.send([]solana.Instruction{initIx}, e.operator)
	if err != nil {
		return fmt.Errorf("initialize_market: %w", err)
	}
	fmt.Printf("initialize_market: "+explorerFmt+"\n", sig)
	markets, err := e.builder.MarketAccounts(marketID)
	if err != nil {
		return err
	}

	// --- 4. vaults + deposits (user-signed) + vault outcome ATAs
	for _, u := range []*solana.Wallet{alice, bob} {
		if err := e.initVaultAndDeposit(u); err != nil {
			return fmt.Errorf("vault setup %s: %w", u.PublicKey(), err)
		}
	}
	if err := e.ensureVaultOutcomeATAs(markets, alice, bob); err != nil {
		return err
	}

	// --- 5. two user-signed orders, crossed by the REAL engine
	fill, err := e.crossOrders(marketID, alice, bob)
	if err != nil {
		return err
	}
	fmt.Printf("engine fill: %s %d @ taker limit %d¢\n", "MINT", fill.Size, fill.Price)

	// --- 6. the Go crank settles it on devnet (v0 + lookup table)
	submitter := crank.NewRPCSubmitter("", e.builder, e.operator)
	submitter.Client = e.client
	submitter.Tables = crank.NewLUTManager(e.client, e.builder, e.operator)
	submitter.ConfirmTimeout = 150 * time.Second
	settleSig, err := submitter.SettleMatch(e.ctx, fill)
	if err != nil {
		return fmt.Errorf("settle_match: %w", err)
	}
	fmt.Printf("settle_match (Go crank, v0+ALT): "+explorerFmt+"\n", settleSig)

	// --- 7. assert on-chain balances match the CTF math
	aliceVault, _ := e.builder.VaultPDA(alice.PublicKey())
	bobVault, _ := e.builder.VaultPDA(bob.PublicKey())
	checks := []struct {
		name  string
		owner solana.PublicKey
		mint  solana.PublicKey
		want  uint64
	}{
		{"alice vault USDC (paid 60)", aliceVault, usdcMint, depositAmt - uint64(yesPrice)*fillSize*10_000},
		{"bob vault USDC (paid 40)", bobVault, usdcMint, depositAmt - uint64(noPrice)*fillSize*10_000},
		{"alice YES shares", aliceVault, markets.YesMint, fillSize},
		{"bob NO shares", bobVault, markets.NoMint, fillSize},
	}
	for _, c := range checks {
		got, err := e.tokenBalance(c.owner, c.mint)
		if err != nil {
			return fmt.Errorf("%s: %w", c.name, err)
		}
		if got != c.want {
			return fmt.Errorf("%s = %d, want %d", c.name, got, c.want)
		}
		fmt.Printf("  ✓ %s = %d\n", c.name, got)
	}
	pool, err := e.tokenBalanceAt(markets.PoolUSDC)
	if err != nil || pool != fillSize*1_000_000 {
		return fmt.Errorf("pool collateral = %d (err %v), want %d", pool, err, fillSize*1_000_000)
	}
	fmt.Printf("  ✓ pool fully collateralized = %d micro\n", pool)

	// --- 8. resolve YES + alice redeems
	resolveIx, err := e.builder.ResolveMarketIx(marketID, models.OutcomeYes, e.operator.PublicKey())
	if err != nil {
		return err
	}
	sig, err = e.send([]solana.Instruction{resolveIx}, e.operator)
	if err != nil {
		return fmt.Errorf("resolve_market: %w", err)
	}
	fmt.Printf("resolve_market(YES): "+explorerFmt+"\n", sig)

	redeemIx, err := e.builder.RedeemIx(marketID, alice.PublicKey(), models.OutcomeYes, fillSize)
	if err != nil {
		return err
	}
	sig, err = e.sendWithSigners([]solana.Instruction{redeemIx}, e.operator, alice)
	if err != nil {
		return fmt.Errorf("redeem: %w", err)
	}
	fmt.Printf("redeem (alice, 100 YES → 100 USDC): "+explorerFmt+"\n", sig)

	walletUSDC, err := e.tokenBalance(alice.PublicKey(), usdcMint)
	if err != nil {
		return err
	}
	// alice started with 1000, deposited 100 → 900 in wallet; redeem pays 100 back.
	if want := uint64(1000)*microUSDC - depositAmt + fillSize*microUSDC; walletUSDC != want {
		return fmt.Errorf("alice wallet USDC after redeem = %d, want %d", walletUSDC, want)
	}
	fmt.Printf("  ✓ alice wallet USDC after redeem = %d (won her 100 back 1:1)\n", walletUSDC)
	return nil
}

func (e *env) createMint() (solana.PublicKey, error) {
	mint := solana.NewWallet()
	rentExempt, err := e.client.GetMinimumBalanceForRentExemption(e.ctx, token.MINT_SIZE, rpc.CommitmentConfirmed)
	if err != nil {
		return solana.PublicKey{}, err
	}
	create := system.NewCreateAccountInstruction(rentExempt, token.MINT_SIZE,
		solana.TokenProgramID, e.operator.PublicKey(), mint.PublicKey()).Build()
	initMint := token.NewInitializeMint2Instruction(6, e.operator.PublicKey(), solana.PublicKey{},
		mint.PublicKey()).Build()
	_, err = e.sendWithSigners([]solana.Instruction{create, initMint}, e.operator, mint)
	return mint.PublicKey(), err
}

func (e *env) fundUsers(usdcMint solana.PublicKey, users ...*solana.Wallet) error {
	var ixs []solana.Instruction
	for _, u := range users {
		ixs = append(ixs,
			system.NewTransferInstruction(solPerUser, e.operator.PublicKey(), u.PublicKey()).Build(),
			ata.NewCreateInstruction(e.operator.PublicKey(), u.PublicKey(), usdcMint).Build(),
		)
		userATA, _, err := solana.FindAssociatedTokenAddress(u.PublicKey(), usdcMint)
		if err != nil {
			return err
		}
		ixs = append(ixs, token.NewMintToInstruction(1000*microUSDC, usdcMint, userATA,
			e.operator.PublicKey(), nil).Build())
	}
	_, err := e.send(ixs, e.operator)
	return err
}

func (e *env) initVaultAndDeposit(u *solana.Wallet) error {
	initIx, err := e.builder.InitVaultIx(u.PublicKey())
	if err != nil {
		return err
	}
	depIx, err := e.builder.DepositIx(u.PublicKey(), depositAmt)
	if err != nil {
		return err
	}
	_, err = e.sendWithSigners([]solana.Instruction{initIx, depIx}, e.operator, u)
	return err
}

// ensureVaultOutcomeATAs pre-creates the vault-owned outcome token accounts —
// settle_match's associated_token constraints require them to exist.
func (e *env) ensureVaultOutcomeATAs(m crank.MarketAccounts, alice, bob *solana.Wallet) error {
	aliceVault, err := e.builder.VaultPDA(alice.PublicKey())
	if err != nil {
		return err
	}
	bobVault, err := e.builder.VaultPDA(bob.PublicKey())
	if err != nil {
		return err
	}
	ixs := []solana.Instruction{
		ata.NewCreateInstruction(e.operator.PublicKey(), aliceVault, m.YesMint).Build(),
		ata.NewCreateInstruction(e.operator.PublicKey(), bobVault, m.NoMint).Build(),
	}
	_, err = e.send(ixs, e.operator)
	return err
}

// crossOrders builds two signed orders and runs them through the real matching
// engine — the fill the crank settles is EXACTLY what production would produce.
func (e *env) crossOrders(marketID [32]byte, alice, bob *solana.Wallet) (matching.Fill, error) {
	mkOrder := func(w *solana.Wallet, outcome uint8, price uint16, salt uint64) *models.Order {
		var maker [32]byte
		copy(maker[:], w.PublicKey().Bytes())
		o := &models.Order{
			Maker: maker, MarketID: marketID, Outcome: outcome, Side: models.SideBuy,
			Price: price, Size: fillSize, Expiry: time.Now().Add(time.Hour).Unix(), Salt: salt,
		}
		models.SignOrder(o, ed25519.PrivateKey(w.PrivateKey))
		if !models.VerifyOrderSig(o) {
			panic("order signature does not verify")
		}
		return o
	}
	book := matching.NewBook(marketID)
	if _, _, err := book.Submit(mkOrder(bob, models.OutcomeNo, noPrice, 1)); err != nil {
		return matching.Fill{}, err
	}
	fills, _, err := book.Submit(mkOrder(alice, models.OutcomeYes, yesPrice, 2))
	if err != nil {
		return matching.Fill{}, err
	}
	if len(fills) != 1 || fills[0].MatchType != models.MatchMint {
		return matching.Fill{}, fmt.Errorf("expected 1 MINT fill, got %+v", fills)
	}
	return fills[0], nil
}

func (e *env) send(ixs []solana.Instruction, payer solana.PrivateKey) (string, error) {
	return e.sendWithSigners(ixs, payer)
}

func (e *env) sendWithSigners(ixs []solana.Instruction, payer solana.PrivateKey, extra ...interface{ PublicKey() solana.PublicKey }) (string, error) {
	recent, err := e.client.GetLatestBlockhash(e.ctx, rpc.CommitmentFinalized)
	if err != nil {
		return "", err
	}
	tx, err := solana.NewTransaction(ixs, recent.Value.Blockhash, solana.TransactionPayer(payer.PublicKey()))
	if err != nil {
		return "", err
	}
	keys := map[solana.PublicKey]solana.PrivateKey{payer.PublicKey(): payer}
	for _, s := range extra {
		if w, ok := s.(*solana.Wallet); ok {
			keys[w.PublicKey()] = w.PrivateKey
		}
	}
	if _, err := tx.Sign(func(key solana.PublicKey) *solana.PrivateKey {
		if k, ok := keys[key]; ok {
			return &k
		}
		return nil
	}); err != nil {
		return "", err
	}
	maxRetries := uint(8)
	sig, err := e.client.SendTransactionWithOpts(e.ctx, tx, rpc.TransactionOpts{
		PreflightCommitment: rpc.CommitmentConfirmed,
		MaxRetries:          &maxRetries,
	})
	if err != nil {
		return "", err
	}
	if err := e.confirm(sig); err != nil {
		return "", err
	}
	return sig.String(), nil
}

func (e *env) confirm(sig solana.Signature) error {
	// Generous window + history search: devnet status caches lag, and a tx
	// frequently lands seconds after a naive poll gives up.
	deadline := time.Now().Add(180 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		st, err := e.client.GetSignatureStatuses(e.ctx, true, sig)
		lastErr = err
		if err == nil && len(st.Value) > 0 && st.Value[0] != nil {
			if st.Value[0].Err != nil {
				return fmt.Errorf("tx %s reverted: %v", sig, st.Value[0].Err)
			}
			if st.Value[0].ConfirmationStatus == rpc.ConfirmationStatusConfirmed ||
				st.Value[0].ConfirmationStatus == rpc.ConfirmationStatusFinalized {
				return nil
			}
		}
		time.Sleep(2500 * time.Millisecond)
	}
	return fmt.Errorf("tx %s not confirmed in time (last poll err: %v)", sig, lastErr)
}

func (e *env) tokenBalance(owner, mint solana.PublicKey) (uint64, error) {
	addr, _, err := solana.FindAssociatedTokenAddress(owner, mint)
	if err != nil {
		return 0, err
	}
	return e.tokenBalanceAt(addr)
}

func (e *env) tokenBalanceAt(addr solana.PublicKey) (uint64, error) {
	res, err := e.client.GetTokenAccountBalance(e.ctx, addr, rpc.CommitmentConfirmed)
	if err != nil {
		return 0, err
	}
	var out uint64
	_, err = fmt.Sscan(res.Value.Amount, &out)
	return out, err
}
