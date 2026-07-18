// Centralized action toasts (sonner). One place for the copy + semantics so
// every surface — TradePanel, MarketPositions, Portfolio, deposit — signals the
// same way. Success is green, errors red, self-trade-prevention amber; copy is
// short and concrete (what happened + the numbers), never decorative.

import { toast } from "sonner";

const outcomeLabel = (outcome: number) => (outcome === 1 ? "YES" : "NO");

type Trade = { side: "buy" | "sell"; outcome: number; size: number; price: number };

export const notify = {
  /**
   * The result of a placed order: a fill and/or a resting order, plus any of the
   * trader's OWN resting orders the backend cancelled to avoid a self-trade.
   */
  order(
    res: { fills: unknown[]; self_trade_prevented?: string[] },
    t: Trade
  ) {
    const noun = outcomeLabel(t.outcome);
    if (res.fills.length) {
      toast.success("Filled", {
        description: `${t.side === "buy" ? "Bought" : "Sold"} ${t.size} ${noun} @ ${t.price}¢`,
      });
    } else {
      toast("Order resting", {
        description: `${t.size} ${noun} @ ${t.price}¢ · waiting to match`,
      });
    }
    const stp = res.self_trade_prevented?.length ?? 0;
    if (stp > 0) notify.selfTrade(stp);
  },

  /** A position exit (a SELL of the whole position at best bid). */
  exit(res: { fills: unknown[] }, x: { outcome: number; size: number; price: number }) {
    const noun = outcomeLabel(x.outcome);
    if (res.fills.length) {
      toast.success("Position closed", { description: `Sold ${x.size} ${noun} @ ${x.price}¢` });
    } else {
      toast("Exit posted", { description: `Sell ${x.size} ${noun} @ ${x.price}¢ · waiting to match` });
    }
  },

  /** Self-trade prevention cancelled the trader's own resting order(s). */
  selfTrade(count: number) {
    toast.warning("Self-trade prevented", {
      description:
        count === 1
          ? "Cancelled one of your own resting orders so you don't trade with yourself."
          : `Cancelled ${count} of your own resting orders so you don't trade with yourself.`,
    });
  },

  cancelled() {
    toast("Order cancelled", { description: "Removed from the book · collateral released" });
  },

  claimed(amountMicro: number) {
    toast.success("Winnings claimed", {
      description: `$${(amountMicro / 1_000_000).toLocaleString()} redeemed on-chain to your wallet`,
    });
  },

  deposited(amountMicro: number) {
    toast.success("Deposit complete", {
      description: `$${(amountMicro / 1_000_000).toLocaleString()} added to your vault`,
    });
  },

  error(e: unknown, fallback = "Something went wrong") {
    toast.error(e instanceof Error ? e.message : typeof e === "string" ? e : fallback);
  },
};
