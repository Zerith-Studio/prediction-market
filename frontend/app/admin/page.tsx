"use client";

// Admin — operator-gated manual control of the market lifecycle. Browse TxLINE
// fixtures + odds, create a fixture's markets on demand, and resolve/void
// markets (per-market or whole-fixture-from-score), all settling on devnet.
// Gated by an operator-wallet signature (usePitchWallet.signMessage). Not linked
// from the public nav — reach it by URL.

import { useCallback, useEffect, useState } from "react";
import { Loader2 } from "lucide-react";
import { usePitchWallet } from "@/lib/wallet";
import { toHex } from "@/lib/borsh";
import { ApiError, explorerTx, explorerAddr } from "@/lib/api";
import {
  admin,
  setAdminToken,
  hasAdminSession,
  adminConfigured,
  type AdminFixture,
  type AdminMarket,
  type AdminOps,
  type FinalScore,
} from "@/lib/adminApi";
import { TopBar } from "@/components/TopBar";
import { VerifyLink } from "@/components/VerifyLink";

const FRANCE_ENGLAND = "18257865"; // the Jul 18 dry-run fixture — pinned

const primaryBtn =
  "bg-accent px-5 py-3 text-[13px] font-semibold text-bg transition-[transform,filter] duration-150 ease-out-strong hover:brightness-110 enabled:active:scale-[0.98] disabled:bg-line2 disabled:text-dim";
const ghostBtn =
  "font-mono text-[11px] text-dim transition-colors hover:text-ink disabled:opacity-40";
const dangerBtn =
  "font-mono text-[11px] text-dim transition-colors hover:text-down disabled:opacity-40";

export default function AdminPage() {
  const [authed, setAuthed] = useState(false);
  useEffect(() => setAuthed(hasAdminSession()), []);

  return (
    <div className="min-h-screen">
      <TopBar balanceMicro={0} />
      <main className="mx-auto max-w-[1100px] px-5 sm:px-8">
        <div className="flex items-baseline justify-between py-8">
          <h1 className="text-[15px] font-semibold">Admin</h1>
          <span className="eyebrow">operator console</span>
        </div>
        {!adminConfigured() ? (
          <NotConfigured />
        ) : authed ? (
          <AdminConsole
            onSignOut={() => {
              setAdminToken(null);
              setAuthed(false);
            }}
          />
        ) : (
          <SignInGate onAuthed={() => setAuthed(true)} />
        )}
      </main>
    </div>
  );
}

function NotConfigured() {
  return (
    <div className="rule-t py-10 text-[13px] text-muted">
      Set <code className="font-mono text-ink">NEXT_PUBLIC_API_URL</code> to reach
      the exchange backend.
    </div>
  );
}

// --- auth gate ---------------------------------------------------------------

function SignInGate({ onAuthed }: { onAuthed: () => void }) {
  const wallet = usePitchWallet();
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);

  async function signIn() {
    setErr(null);
    setBusy(true);
    try {
      if (!wallet.address) throw new Error("connect a wallet first");
      const ch = await admin.challenge();
      const sig = await wallet.signMessage(new TextEncoder().encode(ch.message));
      const { token } = await admin.session(wallet.address, ch.nonce, toHex(sig));
      setAdminToken(token);
      onAuthed();
    } catch (e) {
      setErr(
        e instanceof ApiError && e.status === 403
          ? "This wallet is not the admin wallet."
          : (e as Error).message,
      );
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="rule-t py-10">
      <p className="max-w-[540px] text-[13px] leading-relaxed text-muted">
        The admin console is gated by an operator-wallet signature. Connect the
        admin wallet, then sign a one-time challenge — no password, and nothing
        leaves your device but the signature.
      </p>
      <div className="mt-6 flex items-center gap-4">
        {!wallet.address ? (
          <button onClick={wallet.connect} disabled={!wallet.ready} className={primaryBtn}>
            Connect wallet
          </button>
        ) : (
          <button onClick={signIn} disabled={busy} className={primaryBtn}>
            {busy ? (
              <span className="inline-flex items-center gap-2">
                <Loader2 size={14} className="animate-spin" />
                Signing…
              </span>
            ) : (
              "Sign in as admin"
            )}
          </button>
        )}
        {wallet.address && (
          <span className="font-mono text-[12px] text-dim">
            {wallet.address.slice(0, 4)}…{wallet.address.slice(-4)}
          </span>
        )}
      </div>
      {err && (
        <p role="alert" className="mt-4 font-mono text-[12px] text-down">
          {err}
        </p>
      )}
    </div>
  );
}

// --- console -----------------------------------------------------------------

interface Notice {
  title: string;
  detail: string;
  tx: string;
}

function AdminConsole({ onSignOut }: { onSignOut: () => void }) {
  const [ops, setOps] = useState<AdminOps | null>(null);
  const [markets, setMarkets] = useState<AdminMarket[]>([]);
  const [err, setErr] = useState<string | null>(null);
  const [notices, setNotices] = useState<Notice[]>([]);

  const notify = useCallback((title: string, detail: string, tx: string) => {
    setNotices((n) => [{ title, detail, tx }, ...n].slice(0, 6));
  }, []);

  const load = useCallback(async () => {
    try {
      const [o, m] = await Promise.all([admin.ops(), admin.markets()]);
      setOps(o);
      setMarkets(m);
      setErr(null);
    } catch (e) {
      if (e instanceof ApiError && e.status === 401) {
        onSignOut();
        return;
      }
      setErr((e as Error).message);
    }
  }, [onSignOut]);

  useEffect(() => {
    load();
    const t = setInterval(load, 8000);
    return () => clearInterval(t);
  }, [load]);

  return (
    <div className="space-y-12 pb-24">
      <OpsStrip ops={ops} onSignOut={onSignOut} />
      {err && (
        <p role="alert" className="font-mono text-[12px] text-down">
          {err}
        </p>
      )}
      {notices.length > 0 && <Notices notices={notices} />}
      <FixturesSection onChanged={load} notify={notify} />
      <MarketsSection markets={markets} onChanged={load} notify={notify} />
      <ResolveFromScoreSection onChanged={load} notify={notify} />
    </div>
  );
}

function Notices({ notices }: { notices: Notice[] }) {
  return (
    <section className="rule-t pt-4">
      <div className="eyebrow">recent actions</div>
      <ul className="mt-3 space-y-1.5">
        {notices.map((n, i) => (
          <li key={i} className="flex items-center gap-3 font-mono text-[12px]">
            <span className="text-muted">{n.title}</span>
            <span className="text-accent">{n.detail}</span>
            {n.tx && <VerifyLink href={explorerTx(n.tx)}>tx</VerifyLink>}
          </li>
        ))}
      </ul>
    </section>
  );
}

function OpsStrip({ ops, onSignOut }: { ops: AdminOps | null; onSignOut: () => void }) {
  const totalMarkets = ops
    ? Object.values(ops.markets_by_status).reduce((a, b) => a + b, 0)
    : null;
  return (
    <section>
      <div className="flex items-center justify-between">
        <span className="eyebrow">operations</span>
        <button onClick={onSignOut} className={ghostBtn}>
          sign out
        </button>
      </div>
      <div className="mt-4 grid grid-cols-2 gap-x-8 gap-y-5 sm:grid-cols-4">
        <Stat
          label="chain"
          value={ops ? (ops.chain_enabled ? "on-chain" : "mirror") : "—"}
          tone={ops?.chain_enabled ? "up" : "dim"}
        />
        <Stat
          label="operator SOL"
          value={ops?.operator_sol != null ? ops.operator_sol.toFixed(3) : "—"}
          tone={ops?.operator_sol != null && ops.operator_sol < 1 ? "down" : "ink"}
        />
        <Stat
          label="TxLINE creds"
          value={
            ops?.txline_valid == null ? "—" : ops.txline_valid ? "valid" : "expired"
          }
          tone={
            ops?.txline_valid ? "up" : ops?.txline_valid === false ? "down" : "dim"
          }
        />
        <Stat label="markets" value={totalMarkets != null ? String(totalMarkets) : "—"} />
      </div>
      {ops && (ops.operator || totalMarkets) ? (
        <div className="mt-3 font-mono text-[11px] text-dim">
          {ops.operator && (
            <>
              operator{" "}
              <VerifyLink href={explorerAddr(ops.operator)}>
                {ops.operator.slice(0, 6)}…{ops.operator.slice(-6)}
              </VerifyLink>
            </>
          )}
          {totalMarkets ? (
            <span className="ml-4">
              {Object.entries(ops.markets_by_status)
                .map(([k, v]) => `${v} ${k}`)
                .join(" · ")}
            </span>
          ) : null}
        </div>
      ) : null}
    </section>
  );
}

function Stat({
  label,
  value,
  tone = "ink",
}: {
  label: string;
  value: string;
  tone?: "ink" | "up" | "down" | "dim";
}) {
  const color =
    tone === "up"
      ? "text-accent"
      : tone === "down"
        ? "text-down"
        : tone === "dim"
          ? "text-dim"
          : "text-ink";
  return (
    <div>
      <div className="eyebrow">{label}</div>
      <div className={`mt-1 font-mono text-[15px] tnum ${color}`}>{value}</div>
    </div>
  );
}

// --- fixtures & odds ---------------------------------------------------------

function FixturesSection({
  onChanged,
  notify,
}: {
  onChanged: () => void;
  notify: (title: string, detail: string, tx: string) => void;
}) {
  const [comp, setComp] = useState(72);
  const [fixtures, setFixtures] = useState<AdminFixture[] | null>(null);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const [odds, setOdds] = useState<Record<string, Record<string, number>>>({});
  const [creating, setCreating] = useState<string | null>(null);

  const loadFixtures = useCallback(async () => {
    setBusy(true);
    setErr(null);
    try {
      setFixtures(await admin.fixtures(comp));
    } catch (e) {
      setErr(fixtureErr(e));
    } finally {
      setBusy(false);
    }
  }, [comp]);

  useEffect(() => {
    loadFixtures();
  }, [loadFixtures]);

  async function toggleOdds(id: string) {
    if (odds[id]) {
      setOdds((o) => {
        const n = { ...o };
        delete n[id];
        return n;
      });
      return;
    }
    try {
      const d = await admin.odds(id);
      setOdds((o) => ({ ...o, [id]: d }));
    } catch (e) {
      setErr((e as Error).message);
    }
  }

  async function create(f: AdminFixture) {
    setCreating(f.id);
    setErr(null);
    try {
      const created = await admin.createMarkets(f.id, f.home, f.away, f.kickoff);
      notify(`${f.home} v ${f.away}`, `created ${created.length} markets`, "");
      await loadFixtures();
      onChanged();
    } catch (e) {
      setErr((e as Error).message);
    } finally {
      setCreating(null);
    }
  }

  return (
    <section>
      <div className="flex items-center justify-between">
        <span className="eyebrow">fixtures &amp; odds</span>
        <label className="flex items-center gap-2 font-mono text-[11px] text-dim">
          competition
          <input
            value={comp}
            onChange={(e) => setComp(Number(e.target.value.replace(/\D/g, "")) || 0)}
            className="w-16 border-b border-line2 bg-transparent pb-0.5 text-right text-ink outline-none focus:border-accent tnum"
          />
        </label>
      </div>

      {err && (
        <p role="alert" className="mt-3 font-mono text-[12px] text-down">
          {err}
        </p>
      )}

      <div className="mt-4">
        {fixtures == null ? (
          <Empty>{busy ? "Loading fixtures…" : "No fixtures."}</Empty>
        ) : fixtures.length === 0 ? (
          <Empty>No fixtures in competition {comp}.</Empty>
        ) : (
          fixtures.map((f) => (
            <div key={f.id} className="rule-b py-3.5">
              <div className="grid grid-cols-[1fr_auto] items-center gap-4">
                <div>
                  <div className="flex items-center gap-2 text-[13px]">
                    <span className={f.id === FRANCE_ENGLAND ? "text-accent" : "text-ink"}>
                      {f.home} v {f.away}
                    </span>
                    {f.live && (
                      <span className="font-mono text-[10px] uppercase tracking-wide text-down">
                        live
                      </span>
                    )}
                    {f.registered && (
                      <span className="font-mono text-[10px] uppercase tracking-wide text-dim">
                        markets ✓
                      </span>
                    )}
                  </div>
                  <div className="mt-0.5 font-mono text-[11px] text-dim tnum">
                    {new Date(f.kickoff).toLocaleString()} · {f.competition} · #{f.id}
                  </div>
                </div>
                <div className="flex items-center gap-4">
                  <button onClick={() => toggleOdds(f.id)} className={ghostBtn}>
                    {odds[f.id] ? "hide odds" : "odds"}
                  </button>
                  <button
                    onClick={() => create(f)}
                    disabled={creating === f.id}
                    className={ghostBtn + " text-accent hover:brightness-110"}
                  >
                    {creating === f.id
                      ? "creating…"
                      : f.registered
                        ? "recreate markets"
                        : "create markets"}
                  </button>
                </div>
              </div>
              {odds[f.id] && (
                <div className="mt-2 flex flex-wrap gap-x-6 gap-y-1 font-mono text-[11px] text-muted tnum">
                  {Object.keys(odds[f.id]).length === 0 ? (
                    <span className="text-dim">no priced markets</span>
                  ) : (
                    Object.entries(odds[f.id]).map(([k, v]) => (
                      <span key={k}>
                        <span className="text-dim">{k}</span> {v}¢
                      </span>
                    ))
                  )}
                </div>
              )}
            </div>
          ))
        )}
      </div>
    </section>
  );
}

function fixtureErr(e: unknown): string {
  if (e instanceof ApiError && e.status === 503)
    return "Live fixture feed not configured (replay / off-chain mode).";
  return (e as Error).message;
}

// --- markets -----------------------------------------------------------------

function MarketsSection({
  markets,
  onChanged,
  notify,
}: {
  markets: AdminMarket[];
  onChanged: () => void;
  notify: (title: string, detail: string, tx: string) => void;
}) {
  const [showResolved, setShowResolved] = useState(false);
  const visible = markets.filter(
    (m) => showResolved || (m.status !== "settled" && m.status !== "void"),
  );
  return (
    <section>
      <div className="flex items-center justify-between">
        <span className="eyebrow">markets</span>
        <button onClick={() => setShowResolved((v) => !v)} className={ghostBtn}>
          {showResolved ? "hide resolved" : "show resolved"}
        </button>
      </div>
      <div className="mt-4">
        {visible.length === 0 ? (
          <Empty>No markets. Create a fixture&apos;s markets above.</Empty>
        ) : (
          visible.map((m) => (
            <MarketRow key={m.market_id} m={m} onChanged={onChanged} notify={notify} />
          ))
        )}
      </div>
    </section>
  );
}

function MarketRow({
  m,
  onChanged,
  notify,
}: {
  m: AdminMarket;
  onChanged: () => void;
  notify: (title: string, detail: string, tx: string) => void;
}) {
  const [armed, setArmed] = useState<string | null>(null);
  const [value, setValue] = useState("");
  const [busy, setBusy] = useState<string | null>(null);
  const [err, setErr] = useState<string | null>(null);

  const resolved = m.status === "settled" || m.status === "void";

  async function resolve(outcome: string, val?: number) {
    setBusy("resolve");
    setErr(null);
    try {
      const r = await admin.resolveMarket(m.market_id, outcome, val);
      notify(m.title, resolved ? outcome : `→ ${outcome}`, r.tx);
      onChanged();
    } catch (e) {
      setErr((e as Error).message);
    } finally {
      setBusy(null);
      setArmed(null);
    }
  }

  async function op(kind: "close" | "clear") {
    setBusy(kind);
    setErr(null);
    try {
      if (kind === "close") {
        await admin.closeMarket(m.market_id);
        notify(m.title, "closed", "");
      } else {
        const r = await admin.cancelOrders(m.market_id);
        notify(m.title, `cleared ${r.cancelled} orders`, "");
      }
      onChanged();
    } catch (e) {
      setErr((e as Error).message);
    } finally {
      setBusy(null);
    }
  }

  return (
    <div className="rule-b py-4">
      <div className="grid grid-cols-[1fr_auto] items-start gap-4">
        <div>
          <div className="flex items-center gap-2 text-[13px] text-ink">
            {m.title}
            <StatusBadge status={m.status} />
          </div>
          <div className="mt-0.5 font-mono text-[11px] text-dim tnum">
            {m.template_key} · {m.type}
            {m.book && (m.book.yes_bid || m.book.yes_ask) ? (
              <span className="ml-3 text-muted">
                bid {m.book.yes_bid ?? "—"} / ask {m.book.yes_ask ?? "—"}¢
              </span>
            ) : m.book ? (
              <span className="ml-3 text-dim">empty book</span>
            ) : null}
          </div>
        </div>

        <div className="flex flex-col items-end gap-2">
          {resolved ? (
            <div className="flex items-center gap-3 font-mono text-[11px]">
              <span className="text-muted">
                {m.outcome?.result ?? (m.outcome?.actual != null ? `actual ${m.outcome.actual}` : m.status)}
              </span>
              {m.chain_tx && <VerifyLink href={explorerTx(m.chain_tx)}>tx</VerifyLink>}
            </div>
          ) : m.type === "binary" ? (
            <div className="flex items-center gap-2">
              {(["yes", "no", "void"] as const).map((o) =>
                armed === o ? (
                  <button
                    key={o}
                    onClick={() => resolve(o)}
                    disabled={busy === "resolve"}
                    className="bg-accent px-2.5 py-1 font-mono text-[11px] font-semibold text-bg transition-[filter] hover:brightness-110 disabled:opacity-60"
                  >
                    {busy === "resolve" ? "…" : `confirm ${o}`}
                  </button>
                ) : (
                  <button
                    key={o}
                    onClick={() => setArmed(o)}
                    className={
                      o === "void"
                        ? dangerBtn + " uppercase"
                        : ghostBtn + " uppercase hover:text-accent"
                    }
                  >
                    {o}
                  </button>
                ),
              )}
            </div>
          ) : (
            <div className="flex items-center gap-2">
              <input
                value={value}
                onChange={(e) => setValue(e.target.value.replace(/[^\d.]/g, ""))}
                placeholder="actual"
                className="w-20 border-b border-line2 bg-transparent pb-0.5 text-right font-mono text-[12px] text-ink outline-none focus:border-accent tnum"
              />
              <button
                onClick={() => value && resolve("settle", Number(value))}
                disabled={!value || busy === "resolve"}
                className={ghostBtn + " text-accent hover:brightness-110"}
              >
                {busy === "resolve" ? "…" : "settle"}
              </button>
              <button
                onClick={() => resolve("void")}
                disabled={busy === "resolve"}
                className={dangerBtn}
              >
                void
              </button>
            </div>
          )}

          {!resolved && (
            <div className="flex items-center gap-3">
              <button onClick={() => op("clear")} disabled={busy === "clear"} className={ghostBtn}>
                {busy === "clear" ? "…" : "clear orders"}
              </button>
              {m.status !== "closed" && (
                <button onClick={() => op("close")} disabled={busy === "close"} className={ghostBtn}>
                  {busy === "close" ? "…" : "close"}
                </button>
              )}
            </div>
          )}
        </div>
      </div>
      {err && (
        <p role="alert" className="mt-2 text-right font-mono text-[11px] text-down">
          {err}
        </p>
      )}
    </div>
  );
}

function StatusBadge({ status }: { status: string }) {
  const tone =
    status === "settled"
      ? "text-accent"
      : status === "void"
        ? "text-down"
        : status === "closed"
          ? "text-muted"
          : "text-dim";
  return (
    <span className={`font-mono text-[10px] uppercase tracking-wide ${tone}`}>
      {status}
    </span>
  );
}

// --- resolve fixture from score ---------------------------------------------

function ResolveFromScoreSection({
  onChanged,
  notify,
}: {
  onChanged: () => void;
  notify: (title: string, detail: string, tx: string) => void;
}) {
  const [fixtureId, setFixtureId] = useState("");
  const [score, setScore] = useState<FinalScore>({ home_goals: 0, away_goals: 0 });
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const [done, setDone] = useState<AdminMarket[] | null>(null);

  function set<K extends keyof FinalScore>(k: K, v: FinalScore[K]) {
    setScore((s) => ({ ...s, [k]: v }));
  }

  async function run() {
    setBusy(true);
    setErr(null);
    setDone(null);
    try {
      if (!fixtureId) throw new Error("fixture id required");
      const markets = await admin.resolveFixture(fixtureId, score);
      const withTx = markets.filter((m) => m.chain_tx).length;
      notify(`fixture #${fixtureId}`, `cascade: ${markets.length} markets, ${withTx} on-chain`, "");
      setDone(markets);
      onChanged();
    } catch (e) {
      setErr((e as Error).message);
    } finally {
      setBusy(false);
    }
  }

  return (
    <section>
      <span className="eyebrow">resolve fixture from score</span>
      <p className="mt-2 max-w-[560px] text-[12.5px] leading-relaxed text-muted">
        Fires the full cascade — every binary market resolves on-chain, precision
        pools settle, combos sweep. Use the numeric fixture id from the table
        above.
      </p>
      <div className="mt-5 flex flex-wrap items-end gap-x-6 gap-y-4">
        <Field label="fixture id">
          <input
            value={fixtureId}
            onChange={(e) => setFixtureId(e.target.value.replace(/\D/g, ""))}
            placeholder={FRANCE_ENGLAND}
            className="w-28 border-b border-line2 bg-transparent pb-1 font-mono text-[13px] text-ink outline-none focus:border-accent tnum"
          />
        </Field>
        <ScoreField label="home" v={score.home_goals} onChange={(n) => set("home_goals", n)} />
        <ScoreField label="away" v={score.away_goals} onChange={(n) => set("away_goals", n)} />
        <ScoreField label="ht home" v={score.ht_home_goals ?? 0} onChange={(n) => set("ht_home_goals", n)} />
        <ScoreField label="ht away" v={score.ht_away_goals ?? 0} onChange={(n) => set("ht_away_goals", n)} />
        <ScoreField
          label="passes"
          v={score.total_passes ?? 0}
          onChange={(n) => set("total_passes", n)}
          wide
        />
        <label className="flex items-center gap-2 font-mono text-[11px] text-dim">
          <input
            type="checkbox"
            checked={!!score.abandoned}
            onChange={(e) => set("abandoned", e.target.checked)}
            className="accent-down"
          />
          abandoned (void all)
        </label>
      </div>
      <div className="mt-6 flex items-center gap-4">
        <button onClick={run} disabled={busy} className={primaryBtn}>
          {busy ? (
            <span className="inline-flex items-center gap-2">
              <Loader2 size={14} className="animate-spin" />
              Resolving…
            </span>
          ) : (
            "Resolve fixture"
          )}
        </button>
        {err && (
          <span role="alert" className="font-mono text-[12px] text-down">
            {err}
          </span>
        )}
      </div>
      {done && (
        <div className="mt-5 rule-t pt-4">
          <div className="eyebrow">resolved</div>
          <div className="mt-3 space-y-1.5">
            {done.map((m) => (
              <div
                key={m.market_id}
                className="flex items-center gap-3 font-mono text-[11px]"
              >
                <span className="w-40 truncate text-muted">{m.template_key}</span>
                <StatusBadge status={m.status} />
                <span className="text-dim">{m.outcome?.result ?? (m.outcome?.actual != null ? `actual ${m.outcome.actual}` : "")}</span>
                {m.chain_tx && <VerifyLink href={explorerTx(m.chain_tx)}>tx</VerifyLink>}
              </div>
            ))}
          </div>
        </div>
      )}
    </section>
  );
}

function ScoreField({
  label,
  v,
  onChange,
  wide,
}: {
  label: string;
  v: number;
  onChange: (n: number) => void;
  wide?: boolean;
}) {
  return (
    <Field label={label}>
      <input
        value={v}
        onChange={(e) => onChange(Number(e.target.value.replace(/\D/g, "")) || 0)}
        className={`${wide ? "w-16" : "w-10"} border-b border-line2 bg-transparent pb-1 text-center font-mono text-[13px] text-ink outline-none focus:border-accent tnum`}
      />
    </Field>
  );
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div>
      <div className="eyebrow mb-1.5">{label}</div>
      {children}
    </div>
  );
}

function Empty({ children }: { children: React.ReactNode }) {
  return <div className="py-8 text-[13px] text-dim">{children}</div>;
}
