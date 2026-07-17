"use client";

import { useEffect, useMemo, useState } from "react";
import { AnimatePresence, motion } from "framer-motion";
import { Check, Loader2 } from "lucide-react";
import bs58 from "bs58";
import type { MarketStatus, Side } from "@/lib/types";
import { buyCostMicro, maxPayoutMicro, usd } from "@/lib/format";
import { api, ApiError } from "@/lib/api";
import { borshOrder, fromHex, randomSalt, toHex } from "@/lib/borsh";
import { usePitchWallet } from "@/lib/wallet";

type SubmitState = "idle" | "signing" | "placed";

export function TradePanel({
  marketId,
  marketTitle,
  yesPrice,
  balanceMicro,
  marketStatus,
  initialOutcome = 1,
  onPlaced,
}: {
  marketId: string;
  marketTitle: string;
  yesPrice: number;
  balanceMicro: number;
  marketStatus: MarketStatus;
  initialOutcome?: 0 | 1; // 1 = YES, 0 = NO (from a card's Yes/No button)
  onPlaced?: () => void;
}) {
  const wallet = usePitchWallet();
  const [outcome, setOutcome] = useState<0 | 1>(initialOutcome);
  const [side, setSide] = useState<Side>("buy");
  // The ladder is quoted in YES cents; NO trades at the complement.
  const outcomePrice = outcome === 1 ? yesPrice : 100 - yesPrice;
  const [price, setPrice] = useState(String(outcomePrice));
  const [size, setSize] = useState("500");
  const [submit, setSubmit] = useState<SubmitState>("idle");
  const [placedLabel, setPlacedLabel] = useState("Resting on book");
  const [serverError, setServerError] = useState<string | null>(null);
  const [touchedPrice, setTouchedPrice] = useState(false);
  const [funding, setFunding] = useState(false);

  useEffect(() => {
    if (!touchedPrice) setPrice(String(outcomePrice));
  }, [outcomePrice, touchedPrice]);

  // Switching YES↔NO resnaps the limit price to that outcome's quote.
  function selectOutcome(o: 0 | 1) {
    setOutcome(o);
    setTouchedPrice(false);
    setServerError(null);
  }

  const locked = marketStatus !== "open";
  const connected = !!wallet.address;
  const p = clampInt(price, 1, 99);
  const n = Math.max(0, Math.floor(Number(size) || 0));
  const costMicro = buyCostMicro(p, n);
  const payoutMicro = maxPayoutMicro(n);
  const insufficient = side === "buy" && connected && costMicro > balanceMicro;

  const error = useMemo(() => {
    if (locked) return null;
    if (serverError) return serverError;
    if (!connected) return null; // the button becomes "Connect wallet"
    if (n <= 0) return "Enter a size to trade.";
    if (side === "buy" && balanceMicro === 0) return "Vault is empty.";
    if (insufficient) return "Insufficient vault balance.";
    return null;
  }, [locked, connected, n, insufficient, serverError]);

  const canSubmit = !locked && submit === "idle" && (!connected || !error);

  async function place() {
    if (!canSubmit) return;
    if (!connected) {
      wallet.connect();
      return;
    }
    setServerError(null);
    setSubmit("signing");

    try {
      const salt = randomSalt();
      const msg = borshOrder({
        maker: bs58.decode(wallet.address!),
        marketId: fromHex(marketId),
        outcome,
        side: side === "buy" ? 0 : 1,
        price: p,
        size: BigInt(n),
        feeBps: 0,
        expiry: 0n,
        salt,
      });
      const sig = await wallet.signMessage(msg);
      const res = await api.postOrder({
        maker: wallet.address!,
        market_id: marketId,
        outcome,
        side: side === "buy" ? 0 : 1,
        price: p,
        size: n,
        fee_bps: 0,
        expiry: 0,
        salt: Number(salt),
        sig: toHex(sig),
      });
      setPlacedLabel(res.fills.length ? "Filled" : "Resting on book");
      setSubmit("placed");
      onPlaced?.();
      window.setTimeout(() => setSubmit("idle"), 2600);
    } catch (e) {
      setSubmit("idle");
      setServerError(placeErrorMessage(e, side));
    }
  }

  // Real deposit: the server builds an operator-cosigned devnet tx; the wallet
  // signs its message bytes (the product's one signing moment). Falls back to
  // the mirror faucet when the server runs off-chain.
  async function fund() {
    if (!wallet.address || funding) return;
    setFunding(true);
    try {
      const amount = 1_000_000_000; // 1,000 demo USDC
      const init = await api.depositInit(wallet.address, amount);
      if (init) {
        const msg = Uint8Array.from(atob(init.message_b64), (c) => c.charCodeAt(0));
        const sig = await wallet.signMessage(msg);
        await api.depositComplete(init.deposit_id, wallet.address, amount, toHex(sig));
      } else {
        await api.depositMirror(wallet.address, amount);
      }
      setServerError(null);
      onPlaced?.();
    } catch (e) {
      setServerError(e instanceof Error ? e.message : "Deposit failed.");
    } finally {
      setFunding(false);
    }
  }

  const maxShares = Math.floor(balanceMicro / (p * 10_000));

  // Quick-add chips nudge the existing size — a faster way to set the same
  // `size` state, no new trade inputs.
  const addSize = (d: number) => {
    setServerError(null);
    setSize(String(Math.max(0, n + d)));
  };

  return (
    <div className="border border-line p-5 sm:p-6">
      {/* header — selected market */}
      <div className="mb-6">
        <div className="eyebrow">Selected</div>
        <h2 className="mt-1 truncate text-[15px] font-semibold text-ink">{marketTitle}</h2>
      </div>

      {/* buy / sell */}
      <div className="mb-5 grid grid-cols-2">
        <SideTab
          active={side === "buy"}
          tone="up"
          underlineId="trade-side-underline"
          onClick={() => setSide("buy")}
        >
          Buy
        </SideTab>
        <SideTab
          active={side === "sell"}
          tone="down"
          underlineId="trade-side-underline"
          onClick={() => setSide("sell")}
        >
          Sell
        </SideTab>
      </div>

      {/* yes / no outcome pills */}
      <div className="mb-6 grid grid-cols-2 gap-2.5">
        <OutcomePill
          active={outcome === 1}
          tone="up"
          label="Yes"
          price={yesPrice}
          onClick={() => selectOutcome(1)}
        />
        <OutcomePill
          active={outcome === 0}
          tone="down"
          label="No"
          price={100 - yesPrice}
          onClick={() => selectOutcome(0)}
        />
      </div>

      {/* amount (size) — the prominent number, mirroring the reference */}
      <div className="mb-1 flex items-baseline justify-between">
        <span className="eyebrow">Size</span>
        <span className="font-mono text-[11px] text-dim">
          {connected ? `max ${maxShares.toLocaleString()}` : ""}
        </span>
      </div>
      <div
        className={`flex items-baseline gap-2 border-b border-line2 pb-1.5 transition-colors focus-within:border-accent ${
          locked ? "opacity-40" : ""
        }`}
      >
        <input
          inputMode="numeric"
          value={size}
          disabled={locked}
          onChange={(e) => {
            setServerError(null);
            setSize(e.target.value.replace(/[^0-9]/g, ""));
          }}
          className="w-full bg-transparent font-mono text-[40px] font-light leading-none text-ink outline-none tnum sm:text-[46px]"
        />
        <span className="font-mono text-[13px] text-dim">shares</span>
      </div>

      {/* quick-add chips */}
      <div className="mt-3.5 flex flex-wrap gap-2">
        <Chip disabled={locked} onClick={() => addSize(100)}>
          +100
        </Chip>
        <Chip disabled={locked} onClick={() => addSize(500)}>
          +500
        </Chip>
        <Chip disabled={locked} onClick={() => addSize(1000)}>
          +1k
        </Chip>
        <Chip
          disabled={locked || !connected || maxShares <= 0}
          onClick={() => setSize(String(maxShares))}
        >
          MAX
        </Chip>
      </div>

      {/* limit price — compact secondary control */}
      <div
        className={`mt-5 flex items-center justify-between border-b border-line2 pb-2 transition-colors focus-within:border-accent ${
          locked ? "opacity-40" : ""
        }`}
      >
        <span className="eyebrow">Limit price</span>
        <div className="flex items-baseline gap-1.5">
          <input
            inputMode="numeric"
            value={price}
            disabled={locked}
            onChange={(e) => {
              setTouchedPrice(true);
              setServerError(null);
              setPrice(e.target.value.replace(/[^0-9]/g, ""));
            }}
            className="w-14 bg-transparent text-right font-mono text-[18px] font-light text-ink outline-none tnum"
          />
          <span className="font-mono text-[12px] text-dim">¢</span>
        </div>
      </div>

      <AnimatePresence>
        {error && (
          <motion.p
            initial={{ opacity: 0, height: 0, marginTop: 0 }}
            animate={{ opacity: 1, height: "auto", marginTop: 16 }}
            exit={{ opacity: 0, height: 0, marginTop: 0, transition: { duration: 0.12 } }}
            transition={{ duration: 0.18, ease: [0.23, 1, 0.32, 1] }}
            className="overflow-hidden font-mono text-[12px] text-down"
            role="alert"
          >
            {error}
            {side === "buy" && (insufficient || balanceMicro === 0) && (
              <button
                onClick={fund}
                disabled={funding}
                className="ml-2 text-accent underline underline-offset-2 hover:brightness-110"
              >
                {funding ? "Funding…" : "Fund 1,000 demo USDC"}
              </button>
            )}
          </motion.p>
        )}
      </AnimatePresence>

      {/* primary CTA — neutral, like the reference's prominent action button */}
      <button
        onClick={place}
        disabled={!canSubmit}
        className={`mt-6 w-full px-5 py-3.5 text-[14px] font-semibold tracking-tight transition-[transform,filter,background-color,color] duration-150 ease-out-strong disabled:cursor-not-allowed enabled:active:scale-[0.98] ${
          locked
            ? "bg-line2 text-dim"
            : "bg-ink text-bg hover:brightness-90 disabled:bg-line2 disabled:text-dim"
        }`}
      >
        {/* keyed crossfade: the state morph is the feedback (rare, deliberate
            action — worth animating); blur masks the two-states overlap */}
        <AnimatePresence mode="popLayout" initial={false}>
          <motion.span
            key={buttonKey(locked, connected, submit, side, outcome)}
            initial={{ opacity: 0, y: 5, filter: "blur(2px)" }}
            animate={{ opacity: 1, y: 0, filter: "blur(0px)" }}
            exit={{ opacity: 0, y: -5, filter: "blur(2px)", transition: { duration: 0.1 } }}
            transition={{ duration: 0.13, ease: [0.23, 1, 0.32, 1] }}
            className="flex items-center justify-center gap-2"
          >
            {submit === "signing" && <Loader2 size={15} className="animate-spin" />}
            {submit === "placed" && <Check size={15} />}
            {buttonLabel(locked, connected, submit, side, outcome, placedLabel)}
          </motion.span>
        </AnimatePresence>
      </button>

      {/* order summary + payout */}
      <dl className="mt-6 space-y-2.5 font-mono text-[12.5px]">
        <div className="flex items-baseline justify-between">
          <dt className="text-dim">Shares</dt>
          <dd className="text-ink tnum">{n.toLocaleString()}</dd>
        </div>
        <div className="flex items-baseline justify-between">
          <dt className="text-dim">{side === "buy" ? "Cost" : "Proceeds"}</dt>
          <dd className="text-ink tnum">{usd(costMicro)}</dd>
        </div>
      </dl>

      <div className="mt-4 flex items-end justify-between rule-t pt-4">
        <div>
          <div className="text-[13px] font-semibold text-accent">
            {side === "buy" ? "To win" : "Max payout"}
          </div>
          <div className="mt-1 font-mono text-[11px] text-dim tnum">Avg. price {p}¢</div>
        </div>
        <div className="font-mono text-[26px] font-light leading-none text-accent tnum sm:text-[30px]">
          {usd(payoutMicro)}
        </div>
      </div>

      <p className="mt-5 font-mono text-[11px] leading-relaxed text-dim">
        {locked
          ? "Market closed at kickoff."
          : wallet.isDemo && connected
            ? "Demo wallet (local key) — orders sign and settle on the real exchange."
            : "Order signed in your wallet. Gasless — settled on-chain by the crank."}
      </p>
    </div>
  );
}

function buttonKey(
  locked: boolean,
  connected: boolean,
  submit: SubmitState,
  side: Side,
  outcome: 0 | 1
) {
  if (locked) return "locked";
  if (!connected) return "connect";
  return `${submit}-${submit === "idle" ? `${side}-${outcome}` : ""}`;
}

function buttonLabel(
  locked: boolean,
  connected: boolean,
  submit: SubmitState,
  side: Side,
  outcome: 0 | 1,
  placedLabel: string
) {
  if (locked) return "Trading closed";
  if (!connected) return "Connect wallet";
  if (submit === "signing") return "Signing…";
  if (submit === "placed") return placedLabel;
  const o = outcome === 1 ? "YES" : "NO";
  return side === "buy" ? `Sign & Buy ${o}` : `Sign & Sell ${o}`;
}

function placeErrorMessage(e: unknown, side: Side): string {
  if (e instanceof ApiError) {
    switch (e.status) {
      case 401:
        return "Signature rejected — reconnect your wallet and retry.";
      case 402:
        return side === "buy"
          ? "Insufficient vault balance."
          : "Not enough YES shares to sell.";
      case 409:
        return "Duplicate order — try again.";
      default:
        return e.message || "Order rejected.";
    }
  }
  return e instanceof Error ? e.message : "Order failed.";
}

function SideTab({
  active,
  tone,
  underlineId,
  onClick,
  children,
}: {
  active: boolean;
  tone: "up" | "down";
  underlineId: string;
  onClick: () => void;
  children: React.ReactNode;
}) {
  return (
    <button
      onClick={onClick}
      aria-pressed={active}
      className={`relative border-b-2 border-line pb-2.5 text-[13px] font-semibold transition-colors duration-150 ${
        active ? "text-ink" : "text-dim hover:text-muted"
      }`}
    >
      {children}
      {active && (
        /* one underline shared across each tab pair — the slide states the
           segmented relationship; a jump reads as two unrelated buttons */
        <motion.span
          layoutId={underlineId}
          transition={{ duration: 0.2, ease: [0.23, 1, 0.32, 1] }}
          className={`absolute inset-x-0 -bottom-0.5 h-0.5 ${tone === "up" ? "bg-accent" : "bg-down"}`}
          aria-hidden
        />
      )}
    </button>
  );
}

// Big filled Yes / No selector, styled after the reference: the chosen outcome
// is a solid colour, the other a translucent tint of its own colour.
function OutcomePill({
  active,
  tone,
  label,
  price,
  onClick,
}: {
  active: boolean;
  tone: "up" | "down";
  label: string;
  price: number;
  onClick: () => void;
}) {
  const isUp = tone === "up";
  return (
    <button
      onClick={onClick}
      aria-pressed={active}
      className={`flex items-center justify-center gap-2 border py-3.5 text-[14px] font-semibold transition-[background-color,border-color,color] duration-150 ${
        active
          ? isUp
            ? "border-accent bg-accent text-bg"
            : "border-down bg-down text-bg"
          : isUp
            ? "border-transparent bg-accent/10 text-accent hover:bg-accent/[0.16]"
            : "border-transparent bg-down/10 text-down hover:bg-down/[0.16]"
      }`}
    >
      <span>{label}</span>
      <span className="font-mono tnum">{price}¢</span>
    </button>
  );
}

// Quick-add amount chip (the reference's +$1/+$20/…/MAX row).
function Chip({
  onClick,
  disabled,
  children,
}: {
  onClick: () => void;
  disabled?: boolean;
  children: React.ReactNode;
}) {
  return (
    <button
      onClick={onClick}
      disabled={disabled}
      className="border border-line2 px-3 py-1.5 font-mono text-[12px] text-muted transition-colors hover:border-dim hover:text-ink disabled:cursor-not-allowed disabled:opacity-40 enabled:active:scale-[0.97]"
    >
      {children}
    </button>
  );
}

function clampInt(v: string, min: number, max: number): number {
  const n = Math.floor(Number(v) || 0);
  return Math.max(min, Math.min(max, n));
}
