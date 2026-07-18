"use client";

import { createContext, useCallback, useContext, useEffect, useState } from "react";
import { api } from "@/lib/api";
import { usePitchWallet } from "@/lib/wallet";

interface WatchlistCtx {
  isWatched: (id: string) => boolean;
  toggle: (id: string) => void;
  ready: boolean;
}

const Ctx = createContext<WatchlistCtx | null>(null);

export function useWatchlist(): WatchlistCtx {
  const ctx = useContext(Ctx);
  if (!ctx) throw new Error("useWatchlist must be used within WatchlistProvider");
  return ctx;
}

export function WatchlistProvider({ children }: { children: React.ReactNode }) {
  const wallet = usePitchWallet();
  const [ids, setIds] = useState<Set<string>>(new Set());
  const [ready, setReady] = useState(false);

  // Load once per connected wallet; clear on disconnect.
  useEffect(() => {
    let alive = true;
    setReady(false);
    if (!wallet.address) {
      setIds(new Set());
      setReady(true);
      return;
    }
    api
      .getWatchlist(wallet.address)
      .then((list) => {
        if (alive) {
          setIds(new Set(list));
          setReady(true);
        }
      })
      .catch(() => {
        if (alive) setReady(true);
      });
    return () => {
      alive = false;
    };
  }, [wallet.address]);

  const isWatched = useCallback((id: string) => ids.has(id), [ids]);

  // Optimistic: flip the set now, call the API in the background, revert on error.
  const toggle = useCallback(
    (id: string) => {
      const addr = wallet.address;
      if (!addr) return;
      const currentlyWatched = ids.has(id);
      setIds((prev) => {
        const next = new Set(prev);
        if (currentlyWatched) next.delete(id);
        else next.add(id);
        return next;
      });
      const call = currentlyWatched
        ? api.removeWatch(addr, id)
        : api.addWatch(addr, id);
      call.catch(() => {
        setIds((prev) => {
          const next = new Set(prev);
          if (currentlyWatched) next.add(id);
          else next.delete(id);
          return next;
        });
      });
    },
    [ids, wallet.address]
  );

  return <Ctx.Provider value={{ isWatched, toggle, ready }}>{children}</Ctx.Provider>;
}
