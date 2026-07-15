"use client";

import { useEffect, useMemo, useState } from "react";
import { AnimatePresence, motion } from "framer-motion";
import { Check, Loader2, Lock, TriangleAlert } from "lucide-react";
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

  // keep the limit price tracking the market until the user edits it
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
    if (insufficient) return "Insufficient vault balance for this order.";
    return null;
  }, [locked, n, insufficient]);

  const canSubmit = !locked && !error && submit === "idle";

  function place() {
    if (!canSubmit) return;
    setSubmit("signing");
    // Stub for the real Privy ed25519 sign → POST /orders flow (own craft pass).
    window.setTimeout(() => setSubmit("placed"), 720);
    window.setTimeout(() => setSubmit("idle"), 2600);
  }

  return (
    <div className="panel p-5">
      <div className="mb-4 flex items-center justify-between">
        <h3 className="text-[13px] font-bold">Trade</h3>
        <span className="rounded-md bg-yes/[0.15] px-2 py-0.5 font-mono text-[11px] font-bold text-yes">
          YES
        </span>
      </div>

      <div className="mb-4 grid grid-cols-2 gap-2">
        <SideTab active={side === "buy"} tone="yes" onClick={() => setSide("buy")}>
          BUY YES
        </SideTab>
        <SideTab active={side === "sell"} tone="no" onClick={() => setSide("sell")}>
          SELL YES
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

      <Field
        label="Size"
        hint={`max ${Math.floor(balanceMicro / (p * 10_000)).toLocaleString()} @ ${p}¢`}
      >
        <NumInput value={size} unit="shares" disabled={locked} onChange={setSize} />
      </Field>

      <div className="my-3.5 flex items-center justify-between rounded-[10px] border border-dashed border-line2 bg-bg/60 px-3.5 py-3 font-mono text-[12px] text-muted">
        <span>
          {side === "buy" ? "Cost" : "Proceeds"}{" "}
          <b className="ml-1 text-[14px] text-ink tnum">{usd(costMicro)}</b>
        </span>
        <span>
          Max payout <b className="ml-1 text-[14px] text-yes tnum">{usd(payoutMicro)}</b>
        </span>
      </div>

      <AnimatePresence>
        {error && (
          <motion.p
            initial={{ opacity: 0, y: -4 }}
            animate={{ opacity: 1, y: 0 }}
            exit={{ opacity: 0 }}
            transition={{ duration: 0.18 }}
            className="mb-3 flex items-center gap-1.5 text-[12px] font-medium text-no"
            role="alert"
          >
            <TriangleAlert size={13} /> {error}
          </motion.p>
        )}
      </AnimatePresence>

      <SubmitButton state={submit} locked={locked} disabled={!canSubmit} side={side} onClick={place} />

      <p className="mt-2.5 text-center font-mono text-[11px] text-dim">
        {locked ? (
          <span className="flex items-center justify-center gap-1.5">
            <Lock size={11} /> market closed at kickoff
          </span>
        ) : (
          <>
            order signed in-wallet · <span className="text-verify">gasless</span> ·
            settled by crank
          </>
        )}
      </p>
    </div>
  );
}

function SubmitButton({
  state,
  locked,
  disabled,
  side,
  onClick,
}: {
  state: SubmitState;
  locked: boolean;
  disabled: boolean;
  side: Side;
  onClick: () => void;
}) {
  const buy = side === "buy";
  const base =
    "flex w-full items-center justify-center gap-2 rounded-xl px-5 py-3.5 text-[15px] font-extrabold transition-all disabled:cursor-not-allowed";
  if (locked) {
    return (
      <button disabled className={`${base} bg-panel2 text-dim`}>
        <Lock size={15} /> Trading closed
      </button>
    );
  }
  return (
    <button
      onClick={onClick}
      disabled={disabled}
      className={`${base} ${
        buy
          ? "bg-gradient-to-b from-yes to-[#12a866] text-yes-ink shadow-[0_8px_24px_rgba(34,224,138,.22)]"
          : "bg-gradient-to-b from-no to-[#c9243f] text-no-ink shadow-[0_8px_24px_rgba(255,77,106,.2)]"
      } disabled:from-panel2 disabled:to-panel2 disabled:text-dim disabled:shadow-none`}
    >
      {state === "signing" && <Loader2 size={16} className="animate-spin" />}
      {state === "placed" && <Check size={16} />}
      {state === "idle" && (buy ? "Sign & Buy YES" : "Sign & Sell YES")}
      {state === "signing" && "Signing…"}
      {state === "placed" && "Order resting on book"}
    </button>
  );
}

function SideTab({
  active,
  tone,
  onClick,
  children,
}: {
  active: boolean;
  tone: "yes" | "no";
  onClick: () => void;
  children: React.ReactNode;
}) {
  const activeCls =
    tone === "yes"
      ? "border-yes bg-yes/10 text-yes"
      : "border-no bg-no/10 text-no";
  return (
    <button
      onClick={onClick}
      aria-pressed={active}
      className={`rounded-xl border p-3 font-mono text-[14px] font-extrabold transition-colors ${
        active ? activeCls : "border-line2 bg-panel2 text-muted hover:text-ink"
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
    <label className="mb-3 block">
      <span className="mb-1.5 flex items-center justify-between font-mono text-[11px] tracking-[0.05em] text-muted">
        {label} <span className="text-dim">{hint}</span>
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
      className={`flex items-center justify-between rounded-[10px] border border-line2 bg-bg/70 px-3.5 py-2.5 focus-within:border-verify ${
        disabled ? "opacity-50" : ""
      }`}
    >
      <input
        inputMode="numeric"
        value={value}
        disabled={disabled}
        onChange={(e) => onChange(e.target.value.replace(/[^0-9]/g, ""))}
        className="w-full bg-transparent font-mono text-[16px] font-bold text-ink outline-none tnum"
      />
      <span className="ml-2 font-mono text-[12px] text-dim">{unit}</span>
    </div>
  );
}

function clampInt(v: string, min: number, max: number): number {
  const n = Math.floor(Number(v) || 0);
  return Math.max(min, Math.min(max, n));
}
