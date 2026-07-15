"use client";

import { useEffect, useMemo, useState } from "react";
import { AnimatePresence, motion } from "framer-motion";
import { Check, Loader2 } from "lucide-react";
import type { MarketStatus, Side } from "@/lib/types";
import { buyCostMicro, maxPayoutMicro, usd } from "@/lib/format";

type SubmitState = "idle" | "signing" | "placed";

export function TradePanel({
  yesPrice,
  balanceMicro,
  marketStatus,
}: {
  yesPrice: number;
  balanceMicro: number;
  marketStatus: MarketStatus;
}) {
  const [side, setSide] = useState<Side>("buy");
  const [price, setPrice] = useState(String(yesPrice));
  const [size, setSize] = useState("500");
  const [submit, setSubmit] = useState<SubmitState>("idle");
  const [touchedPrice, setTouchedPrice] = useState(false);

  useEffect(() => {
    if (!touchedPrice) setPrice(String(yesPrice));
  }, [yesPrice, touchedPrice]);

  const locked = marketStatus !== "open";
  const p = clampInt(price, 1, 99);
  const n = Math.max(0, Math.floor(Number(size) || 0));
  const costMicro = buyCostMicro(p, n);
  const payoutMicro = maxPayoutMicro(n);
  const insufficient = side === "buy" && costMicro > balanceMicro;

  const error = useMemo(() => {
    if (locked) return null;
    if (n <= 0) return "Enter a size to trade.";
    if (insufficient) return "Insufficient vault balance.";
    return null;
  }, [locked, n, insufficient]);

  const canSubmit = !locked && !error && submit === "idle";

  function place() {
    if (!canSubmit) return;
    setSubmit("signing");
    window.setTimeout(() => setSubmit("placed"), 720);
    window.setTimeout(() => setSubmit("idle"), 2600);
  }

  const maxShares = Math.floor(balanceMicro / (p * 10_000));

  return (
    <div>
      <div className="mb-5 flex items-baseline justify-between">
        <h2 className="text-[13px] font-semibold text-ink">Trade</h2>
        <span className="font-mono text-[11px] text-dim">YES · Brazil</span>
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
            setPrice(v);
          }}
        />
      </Field>

      <Field label="Size" hint={`max ${maxShares.toLocaleString()}`}>
        <NumInput value={size} unit="shares" disabled={locked} onChange={setSize} />
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
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
            transition={{ duration: 0.16 }}
            className="mb-3 font-mono text-[12px] text-down"
            role="alert"
          >
            {error}
          </motion.p>
        )}
      </AnimatePresence>

      <button
        onClick={place}
        disabled={!canSubmit}
        className={`flex w-full items-center justify-center gap-2 px-5 py-3.5 text-[14px] font-semibold tracking-tight transition-colors disabled:cursor-not-allowed ${
          locked
            ? "bg-line2 text-dim"
            : side === "buy"
              ? "bg-accent text-bg hover:brightness-110 disabled:bg-line2 disabled:text-dim"
              : "bg-down text-bg hover:brightness-110 disabled:bg-line2 disabled:text-dim"
        }`}
      >
        {submit === "signing" && <Loader2 size={15} className="animate-spin" />}
        {submit === "placed" && <Check size={15} />}
        {locked
          ? "Trading closed"
          : submit === "idle"
            ? side === "buy"
              ? "Sign & Buy YES"
              : "Sign & Sell YES"
            : submit === "signing"
              ? "Signing…"
              : "Resting on book"}
      </button>

      <p className="mt-3 font-mono text-[11px] leading-relaxed text-dim">
        {locked
          ? "Market closed at kickoff."
          : "Order signed in your wallet. Gasless — settled on-chain by the crank."}
      </p>
    </div>
  );
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
      className={`border-b-2 pb-2.5 text-[13px] font-semibold transition-colors ${
        active
          ? tone === "up"
            ? "border-accent text-ink"
            : "border-down text-ink"
          : "border-line text-dim hover:text-muted"
      }`}
    >
      {children}
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
