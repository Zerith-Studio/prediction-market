// Command txline-provision acquires (or refreshes) TxLINE API credentials via
// the free World Cup tier: guest JWT → on-chain subscribe (devnet, 0 tokens) →
// activation. Prints the tokens and caches them for the feed provider.
//
//	go run ./cmd/txline-provision -keypair ~/.config/solana/id.json
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"

	"github.com/Zerith-Studio/prediction-market/backend/internal/feed/txodds"
)

func main() {
	keypairPath := flag.String("keypair", os.Getenv("HOME")+"/.config/solana/id.json", "wallet that pays the subscribe tx fee")
	baseURL := flag.String("base", txodds.DevNetBase, "TxLINE server")
	rpcURL := flag.String("rpc", rpc.DevNet_RPC, "Solana RPC")
	cache := flag.String("cache", ".txline-credentials.json", "credential cache path")
	flag.Parse()

	wallet, err := solana.PrivateKeyFromSolanaKeygenFile(*keypairPath)
	if err != nil {
		log.Fatalf("load wallet: %v", err)
	}
	fmt.Printf("wallet: %s\n", wallet.PublicKey())

	creds, err := txodds.EnsureCredentials(context.Background(), *baseURL, *rpcURL, *cache, wallet)
	if err != nil {
		log.Fatalf("provision failed: %v", err)
	}
	fmt.Printf("subscribe tx: %s\n", creds.SubTxSig)
	fmt.Printf("api token:    %s…\n", creds.APIToken[:min(18, len(creds.APIToken))])
	fmt.Printf("jwt:          %s…\n", creds.JWT[:min(18, len(creds.JWT))])
	fmt.Printf("cached to:    %s (valid until %s)\n", *cache, creds.ExpiresAt.Format("2006-01-02"))
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
