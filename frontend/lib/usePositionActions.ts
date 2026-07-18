"use client";

// Shared exit/cancel actions for a wallet's positions and open orders, used by
// both the full Portfolio page and the market-page MarketPositions panel.
// Exit signs a SELL of the whole position at the current best bid — the exact
// same signing path as the trade panel (the backend can't tell them apart).

import { useCallback, useState } from "react";
import bs58 from "bs58";
import { api } from "./api";
import { borshOrder, fromHex, randomSalt, toHex } from "./borsh";
import { notify } from "./toast";
import type { Position } from "./types";
import { usePitchWallet } from "./wallet";

export interface PosCalc {
  p: Position;
  side: "YES" | "NO";
  qty: number; // shares held (gross)
  locked: number; // shares already committed to resting SELL (exit) orders
  available: number; // qty − locked: what can still be exited right now
  entry: number; // avg cost, cents
  cur: number; // exit mark (BBP) in the held side's terms, cents
  valueMicro: number;
  unrealizedMicro: number;
}

export function calcPosition(p: Position): PosCalc {
  const side: "YES" | "NO" = p.yes > 0 ? "YES" : "NO";
  const qty = side === "YES" ? p.yes : p.no;
  const locked = side === "YES" ? p.yes_locked ?? 0 : p.no_locked ?? 0;
  const available = Math.max(0, qty - locked);
  const cur = p.current > 0 ? (side === "YES" ? p.current : 100 - p.current) : p.avg_cost;
  const valueMicro = qty * cur * 10_000;
  const costMicro = qty * p.avg_cost * 10_000;
  return { p, side, qty, locked, available, entry: p.avg_cost, cur, valueMicro, unrealizedMicro: valueMicro - costMicro };
}

export function usePositionActions(onDone: () => void) {
  const wallet = usePitchWallet();
  const [busy, setBusy] = useState<string | null>(null); // market_id or order_hash in flight
  const [error, setError] = useState<string | null>(null);

  const exit = useCallback(
    async (x: PosCalc) => {
      // Only exit the un-locked shares: the rest are already resting in a prior
      // exit order, so selling the gross qty would fail "insufficient outcome tokens".
      if (!wallet.address || x.cur <= 0 || x.available <= 0 || busy) return;
      setError(null);
      setBusy(x.p.market_id);
      try {
        const salt = randomSalt();
        const outcome = x.side === "YES" ? 1 : 0;
        const price = Math.max(1, x.side === "YES" ? x.cur : 100 - x.cur);
        const msg = borshOrder({
          maker: bs58.decode(wallet.address),
          marketId: fromHex(x.p.market_id),
          outcome,
          side: 1, // SELL
          price,
          size: BigInt(x.available),
          feeBps: 0,
          expiry: 0n,
          salt,
        });
        const sig = await wallet.signMessage(msg);
        const res = await api.postOrder({
          maker: wallet.address,
          market_id: x.p.market_id,
          outcome,
          side: 1,
          price,
          size: x.available,
          fee_bps: 0,
          expiry: 0,
          salt: Number(salt),
          sig: toHex(sig),
        });
        notify.exit(res, { outcome, size: x.qty, price });
        onDone();
      } catch (e) {
        notify.error(e, "Exit failed");
        setError(e instanceof Error ? e.message : "exit failed");
      } finally {
        setBusy(null);
      }
    },
    [wallet, busy, onDone]
  );

  const cancel = useCallback(
    async (orderHash: string) => {
      if (!wallet.address || busy) return;
      setError(null);
      setBusy(orderHash);
      try {
        await api.cancelOrder(orderHash, wallet.address);
        notify.cancelled();
        onDone();
      } catch (e) {
        notify.error(e, "Cancel failed");
        setError(e instanceof Error ? e.message : "cancel failed");
      } finally {
        setBusy(null);
      }
    },
    [wallet, busy, onDone]
  );

  // Claim winnings on a resolved binary market: user-signed on-chain redeem
  // (burn winning shares → USDC to their wallet), then the mirror follows.
  const claim = useCallback(
    async (marketId: string) => {
      if (!wallet.address || busy) return;
      setError(null);
      setBusy(marketId);
      try {
        const init = await api.redeemInit(wallet.address, marketId);
        const msg = Uint8Array.from(atob(init.message_b64), (c) => c.charCodeAt(0));
        const sig = await wallet.signMessage(msg);
        const res = await api.redeemComplete(init.redeem_id, wallet.address, marketId, toHex(sig));
        notify.claimed(res.amount);
        onDone();
      } catch (e) {
        notify.error(e, "Claim failed");
        setError(e instanceof Error ? e.message : "claim failed");
      } finally {
        setBusy(null);
      }
    },
    [wallet, busy, onDone]
  );

  return { exit, cancel, claim, busy, error };
}
