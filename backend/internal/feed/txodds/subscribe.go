package txodds

// Self-provisioning TxLINE access (docs: txline.txodds.com/api-reference):
//
//  1. POST /auth/guest/start                      → 30-day guest JWT (free)
//  2. txoracle `subscribe(level, weeks)` on-chain → free World Cup tier charges
//     0 TxLINE; the wallet only pays the tx fee (devnet SOL)
//  3. sign "txSig:leagues:jwt" with the wallet    → detached ed25519, base64
//  4. POST /api/token/activate                    → long-lived X-Api-Token
//
// Data calls then carry BOTH `Authorization: Bearer <jwt>` and `X-Api-Token`.
// Tokens are cached on disk so this runs once per subscription period.

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/gagliardetto/solana-go"
	ata "github.com/gagliardetto/solana-go/programs/associated-token-account"
	"github.com/gagliardetto/solana-go/rpc"
)

const (
	// DevNetBase is the TxLINE test server (their program lives on Solana devnet).
	DevNetBase = "https://txline-dev.txodds.com"
	// Devnet txoracle program + TxLINE Token-2022 mint (txodds/tx-on-chain repo).
	txOracleProgram = "6pW64gN1s2uqjHkn1unFeEjAwJkPGHoppGvS715wyP2J"
	txLineMint      = "4Zao8ocPhmMgq7PdsYWyxvqySMGx7xb9cMftPMkEokRG"

	// Free World Cup tier: service level 1, minimum 4-week duration, no
	// explicit league selection (the tier defines the bundle).
	freeTierLevel = uint16(1)
	freeTierWeeks = uint8(4)
)

// Credentials is the cached access state.
type Credentials struct {
	JWT       string    `json:"jwt"`
	APIToken  string    `json:"api_token"`
	SubTxSig  string    `json:"subscribe_tx"`
	IssuedAt  time.Time `json:"issued_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

// EnsureCredentials returns valid TxLINE credentials, provisioning them
// (guest JWT + on-chain free-tier subscribe + activation) when the cache at
// cachePath is missing or expired. wallet pays the subscribe tx fee.
func EnsureCredentials(ctx context.Context, baseURL, rpcURL, cachePath string, wallet solana.PrivateKey) (*Credentials, error) {
	if c := loadCache(cachePath); c != nil && time.Now().Before(c.ExpiresAt.Add(-24*time.Hour)) {
		return c, nil
	}

	jwt, err := guestJWT(ctx, baseURL)
	if err != nil {
		return nil, err
	}

	txSig, err := subscribeOnChain(ctx, rpcURL, wallet)
	if err != nil {
		return nil, fmt.Errorf("txodds: on-chain subscribe: %w", err)
	}

	apiToken, err := activate(ctx, baseURL, jwt, txSig, wallet)
	if err != nil {
		return nil, fmt.Errorf("txodds: activate: %w", err)
	}

	c := &Credentials{
		JWT:      jwt,
		APIToken: apiToken,
		SubTxSig: txSig,
		IssuedAt: time.Now().UTC(),
		// JWT lives 30 days, subscription 4 weeks — refresh at the earlier one.
		ExpiresAt: time.Now().UTC().Add(27 * 24 * time.Hour),
	}
	if cachePath != "" {
		if raw, err := json.MarshalIndent(c, "", " "); err == nil {
			_ = os.WriteFile(cachePath, raw, 0o600)
		}
	}
	return c, nil
}

// RenewJWT refreshes only the session JWT (the API token outlives it).
func (c *Credentials) RenewJWT(ctx context.Context, baseURL string) error {
	jwt, err := guestJWT(ctx, baseURL)
	if err != nil {
		return err
	}
	c.JWT = jwt
	return nil
}

func loadCache(path string) *Credentials {
	if path == "" {
		return nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var c Credentials
	if json.Unmarshal(raw, &c) != nil || c.APIToken == "" || c.JWT == "" {
		return nil
	}
	return &c
}

func guestJWT(ctx context.Context, baseURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/auth/guest/start", nil)
	if err != nil {
		return "", err
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("txodds: guest session: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(res.Body)
		return "", fmt.Errorf("txodds: guest session status %d: %s", res.StatusCode, body)
	}
	var out struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		return "", err
	}
	return out.Token, nil
}

// subscribeOnChain sends txoracle.subscribe(freeTierLevel, freeTierWeeks).
// Account order per the published IDL; the user token account is a Token-2022
// ATA that is created first when missing (the free tier moves 0 tokens but the
// account must exist).
func subscribeOnChain(ctx context.Context, rpcURL string, wallet solana.PrivateKey) (string, error) {
	client := rpc.New(rpcURL)
	program := solana.MustPublicKeyFromBase58(txOracleProgram)
	mint := solana.MustPublicKeyFromBase58(txLineMint)
	owner := wallet.PublicKey()

	userATA, _, err := solana.FindAssociatedTokenAddressWithProgram(owner, mint, solana.Token2022ProgramID)
	if err != nil {
		return "", err
	}
	pricingMatrix, _, err := solana.FindProgramAddress([][]byte{[]byte("pricing_matrix")}, program)
	if err != nil {
		return "", err
	}
	treasuryPDA, _, err := solana.FindProgramAddress([][]byte{[]byte("token_treasury_v2")}, program)
	if err != nil {
		return "", err
	}
	treasuryVault, _, err := solana.FindAssociatedTokenAddressWithProgram(treasuryPDA, mint, solana.Token2022ProgramID)
	if err != nil {
		return "", err
	}

	var ixs []solana.Instruction
	if info, err := client.GetAccountInfo(ctx, userATA); err != nil || info == nil || info.Value == nil {
		createATA := ata.NewCreateInstructionBuilder().
			SetPayer(owner).
			SetWallet(owner).
			SetMint(mint).
			SetTokenProgram(solana.Token2022ProgramID).
			Build()
		ixs = append(ixs, createATA)
	}

	// subscribe discriminator + service_level_id u16 LE + weeks u8 (IDL).
	data := []byte{254, 28, 191, 138, 156, 179, 183, 53}
	data = binary.LittleEndian.AppendUint16(data, freeTierLevel)
	data = append(data, freeTierWeeks)
	ixs = append(ixs, solana.NewInstruction(program, solana.AccountMetaSlice{
		solana.Meta(owner).WRITE().SIGNER(),
		solana.Meta(pricingMatrix),
		solana.Meta(mint),
		solana.Meta(userATA).WRITE(),
		solana.Meta(treasuryVault).WRITE(),
		solana.Meta(treasuryPDA),
		solana.Meta(solana.Token2022ProgramID), // TxLINE is a Token-2022 mint
		solana.Meta(solana.SystemProgramID),
		solana.Meta(solana.SPLAssociatedTokenAccountProgramID),
	}, data))

	recent, err := client.GetLatestBlockhash(ctx, rpc.CommitmentFinalized)
	if err != nil {
		return "", err
	}
	tx, err := solana.NewTransaction(ixs, recent.Value.Blockhash, solana.TransactionPayer(owner))
	if err != nil {
		return "", err
	}
	if _, err := tx.Sign(func(key solana.PublicKey) *solana.PrivateKey {
		if key.Equals(owner) {
			return &wallet
		}
		return nil
	}); err != nil {
		return "", err
	}
	sig, err := client.SendTransactionWithOpts(ctx, tx, rpc.TransactionOpts{
		PreflightCommitment: rpc.CommitmentConfirmed,
	})
	if err != nil {
		return "", err
	}

	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		st, err := client.GetSignatureStatuses(ctx, true, sig)
		if err == nil && len(st.Value) > 0 && st.Value[0] != nil {
			if st.Value[0].Err != nil {
				return "", fmt.Errorf("subscribe tx %s reverted: %v", sig, st.Value[0].Err)
			}
			if st.Value[0].ConfirmationStatus == rpc.ConfirmationStatusConfirmed ||
				st.Value[0].ConfirmationStatus == rpc.ConfirmationStatusFinalized {
				return sig.String(), nil
			}
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	return "", fmt.Errorf("subscribe tx %s not confirmed in time", sig)
}

// activate exchanges the confirmed subscribe tx for a long-lived API token.
// The wallet signs the strict binding "txSig:leagues:jwt" (leagues empty for
// the free tier's fixed bundle).
func activate(ctx context.Context, baseURL, jwt, txSig string, wallet solana.PrivateKey) (string, error) {
	msg := fmt.Sprintf("%s:%s:%s", txSig, "", jwt)
	sig := ed25519.Sign(ed25519.PrivateKey(wallet), []byte(msg))

	body, _ := json.Marshal(map[string]any{
		"txSig":           txSig,
		"walletSignature": base64.StdEncoding.EncodeToString(sig),
		"leagues":         []int{},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		baseURL+"/api/token/activate", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+jwt)
	req.Header.Set("Content-Type", "application/json")

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("status %d: %s", res.StatusCode, raw)
	}
	// The endpoint returns either a bare token string or {"token": "..."}.
	var obj struct {
		Token string `json:"token"`
	}
	if json.Unmarshal(raw, &obj) == nil && obj.Token != "" {
		return obj.Token, nil
	}
	var s string
	if json.Unmarshal(raw, &s) == nil && s != "" {
		return s, nil
	}
	return string(bytes.TrimSpace(raw)), nil
}
