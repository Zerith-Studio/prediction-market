// Money-UI honesty (PRODUCT.md principle 5): one consistent numeric treatment.

/** micro-USDC integer -> "$1,240.00" */
export function usd(micro: number): string {
  return (micro / 1_000_000).toLocaleString("en-US", {
    style: "currency",
    currency: "USD",
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  });
}

/** micro-USDC integer -> "1,240.00" (no symbol) */
export function usdBare(micro: number): string {
  return (micro / 1_000_000).toLocaleString("en-US", {
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  });
}

/** price cents 1..99 -> "64¢" */
export function cents(p: number): string {
  return `${p}¢`;
}

/** price cents -> implied probability "64%" */
export function prob(p: number): string {
  return `${p}%`;
}

/** shares integer -> "1,120" */
export function shares(n: number): string {
  return n.toLocaleString("en-US");
}

/** short hash for display: "7f3a…c2e1" */
export function shortHash(h: string, lead = 4, tail = 4): string {
  if (h.length <= lead + tail) return h;
  return `${h.slice(0, lead)}…${h.slice(-tail)}`;
}

/** cost of a BUY at price p for size shares, in micro-USDC. p is cents. */
export function buyCostMicro(priceCents: number, size: number): number {
  // 1 share pays out 1 USDC on win; cost = price(as fraction of $1) * size.
  return Math.round((priceCents / 100) * size * 1_000_000);
}

/** max payout of a position (1:1 per winning share), micro-USDC. */
export function maxPayoutMicro(size: number): number {
  return size * 1_000_000;
}
