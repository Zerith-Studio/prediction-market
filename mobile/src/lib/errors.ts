import { ApiError } from "./api";
import type { Side } from "./types";

/** Human message for an order-placement failure (pinned API semantics). */
export function placeErrorMessage(e: unknown, side: Side): string {
  if (e instanceof ApiError) {
    switch (e.status) {
      case 0: return "Exchange not configured.";
      case 401: return "Signature rejected — reconnect your wallet.";
      case 402: return side === "buy" ? "Insufficient vault balance." : "Not enough shares to sell.";
      case 409: return "Duplicate order — try again.";
      case 410: return "Locked at kickoff.";
    }
    return e.message || "Order failed.";
  }
  return e instanceof Error ? e.message : "Order failed.";
}
