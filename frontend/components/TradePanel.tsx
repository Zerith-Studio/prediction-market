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
  onPlaced,
}: {
  marketId: string;
  marketTitle: string;
  yesPrice: number;
  balanceMicro: number;
  marketStatus: MarketStatus;
  onPlaced?: () => void;
}) {
  const wallet = usePitchWallet();
  const [side, setSide] = useState<Side>("buy");
  const [price, setPrice] = useState(String(yesPrice));
  const [size, setSize] = useState("500");
  const [submit, setSubmit] = useState<SubmitState>("idle");
  const [placedLabel, setPlacedLabel] = useState("Resting on book");
  const [serverError, setServerError] = useState<string | null>(null);
  const [touchedPrice, setTouchedPrice] = useState(false);
  const [funding, setFunding] = useState(false);

  useEffect(() => {
    if (!touchedPrice) setPrice(String(yesPrice));
  }, [yesPrice, touchedPrice]);

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
        outcome: 1, // this panel trades the YES ladder
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
        outcome: 1,
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

  return (
    <div>
      <div className="mb-5 flex items-baseline justify-between">
        <h2 className="text-[13px] font-semibold text-ink">Trade</h2>
        <span className="max-w-[170px] truncate font-mono text-[11px] text-dim">
          YES · {marketTitle}
        </span>
      </div>

      <div className="mb-6 grid grid-cols-2">
        <SideTab active={side === "buy"} tone="up" onClick={() => setSide("buy")}>
          Buy
        </SideTab>
        <SideTab active={side === "sell"} tone="down" onClick={() => setSide("sell")}>
          Sell
        </SideTab>
      </div>

      <Field label="Limit price" hint="¢ 1–99">
        <NumInput
          value={price}
          unit="¢"
          disabled={locked}
          onChange={(v) => {
            setTouchedPrice(true);
            setServerError(null);
            setPrice(v);
          }}
        />
      </Field>

      <Field label="Size" hint={connected ? `max ${maxShares.toLocaleString()}` : ""}>
        <NumInput
          value={size}
          unit="shares"
          disabled={locked}
          onChange={(v) => {
            setServerError(null);
            setSize(v);
          }}
        />
      </Field>

      <dl className="mb-5 mt-6 space-y-2.5 font-mono text-[12.5px]">
        <div className="flex items-baseline justify-between">
          <dt className="text-dim">{side === "buy" ? "Cost" : "Proceeds"}</dt>
          <dd className="text-ink tnum">{usd(costMicro)}</dd>
        </div>
        <div className="flex items-baseline justify-between">
          <dt className="text-dim">Max payout</dt>
          <dd className="text-accent tnum">{usd(payoutMicro)}</dd>
        </div>
      </dl>

      <AnimatePresence>
        {error && (
          <motion.p
            initial={{ opacity: 0, height: 0, marginBottom: 0 }}
            animate={{ opacity: 1, height: "auto", marginBottom: 12 }}
            exit={{ opacity: 0, height: 0, marginBottom: 0, transition: { duration: 0.12 } }}
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

      <button
        onClick={place}
        disabled={!canSubmit}
        className={`w-full px-5 py-3.5 text-[14px] font-semibold tracking-tight transition-[transform,filter,background-color,color] duration-150 ease-out-strong disabled:cursor-not-allowed enabled:active:scale-[0.98] ${
          locked
            ? "bg-line2 text-dim"
            : side === "buy"
              ? "bg-accent text-bg hover:brightness-110 disabled:bg-line2 disabled:text-dim"
              : "bg-down text-bg hover:brightness-110 disabled:bg-line2 disabled:text-dim"
        }`}
      >
        {/* keyed crossfade: the state morph is the feedback (rare, deliberate
            action — worth animating); blur masks the two-states overlap */}
        <AnimatePresence mode="popLayout" initial={false}>
          <motion.span
            key={buttonKey(locked, connected, submit, side)}
            initial={{ opacity: 0, y: 5, filter: "blur(2px)" }}
            animate={{ opacity: 1, y: 0, filter: "blur(0px)" }}
            exit={{ opacity: 0, y: -5, filter: "blur(2px)", transition: { duration: 0.1 } }}
            transition={{ duration: 0.13, ease: [0.23, 1, 0.32, 1] }}
            className="flex items-center justify-center gap-2"
          >
            {submit === "signing" && <Loader2 size={15} className="animate-spin" />}
            {submit === "placed" && <Check size={15} />}
            {buttonLabel(locked, connected, submit, side, placedLabel)}
          </motion.span>
        </AnimatePresence>
      </button>

      <p className="mt-3 font-mono text-[11px] leading-relaxed text-dim">
        {locked
          ? "Market closed at kickoff."
          : wallet.isDemo && connected
            ? "Demo wallet (local key) — orders sign and settle on the real exchange."
            : "Order signed in your wallet. Gasless — settled on-chain by the crank."}
      </p>
    </div>
  );
}

function buttonKey(locked: boolean, connected: boolean, submit: SubmitState, side: Side) {
  if (locked) return "locked";
  if (!connected) return "connect";
  return `${submit}-${submit === "idle" ? side : ""}`;
}

function buttonLabel(
  locked: boolean,
  connected: boolean,
  submit: SubmitState,
  side: Side,
  placedLabel: string
) {
  if (locked) return "Trading closed";
  if (!connected) return "Connect wallet";
  if (submit === "signing") return "Signing…";
  if (submit === "placed") return placedLabel;
  return side === "buy" ? "Sign & Buy YES" : "Sign & Sell YES";
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
  onClick,
  children,
}: {
  active: boolean;
  tone: "up" | "down";
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
        /* one underline shared across both tabs — the slide states the
           segmented relationship; a jump reads as two unrelated buttons */
        <motion.span
          layoutId="trade-side-underline"
          transition={{ duration: 0.2, ease: [0.23, 1, 0.32, 1] }}
          className={`absolute inset-x-0 -bottom-0.5 h-0.5 ${tone === "up" ? "bg-accent" : "bg-down"}`}
          aria-hidden
        />
      )}
    </button>
  );
}

function Field({
  label,
  hint,
  children,
}: {
  label: string;
  hint: string;
  children: React.ReactNode;
}) {
  return (
    <label className="mb-5 block">
      <span className="mb-1 flex items-baseline justify-between font-mono text-[11px] text-muted">
        <span className="tracking-[0.04em]">{label}</span>
        <span className="text-dim">{hint}</span>
      </span>
      {children}
    </label>
  );
}

function NumInput({
  value,
  unit,
  disabled,
  onChange,
}: {
  value: string;
  unit: string;
  disabled?: boolean;
  onChange: (v: string) => void;
}) {
  return (
    <div
      className={`flex items-baseline justify-between border-b border-line2 pb-1.5 transition-colors focus-within:border-accent ${
        disabled ? "opacity-40" : ""
      }`}
    >
      <input
        inputMode="numeric"
        value={value}
        disabled={disabled}
        onChange={(e) => onChange(e.target.value.replace(/[^0-9]/g, ""))}
        className="w-full bg-transparent font-mono text-[22px] font-light text-ink outline-none tnum"
      />
      <span className="ml-2 font-mono text-[12px] text-dim">{unit}</span>
    </div>
  );
}

function clampInt(v: string, min: number, max: number): number {
  const n = Math.floor(Number(v) || 0);
  return Math.max(min, Math.min(max, n));
}
