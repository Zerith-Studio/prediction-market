"use client";

import { useState } from "react";
import { api } from "@/lib/api";
import { admin } from "@/lib/adminApi";
import type { Comment } from "@/lib/types";
import { relTime, shortHash } from "@/lib/format";
import { Avatar } from "./Avatar";

/** Operator comment moderation for one market: list + soft-delete. */
export function AdminComments({ marketId }: { marketId: string }) {
  const [open, setOpen] = useState(false);
  const [items, setItems] = useState<Comment[] | null>(null);
  const [busy, setBusy] = useState<string | null>(null);

  const load = async () => {
    setItems(null);
    try {
      setItems(await api.getComments(marketId, null));
    } catch {
      setItems([]);
    }
  };
  const toggle = () => {
    const next = !open;
    setOpen(next);
    if (next) load();
  };
  const del = async (id: string) => {
    setBusy(id);
    try {
      await admin.deleteComment(id);
      await load();
    } catch {
      /* 401 clears the session; the page falls back to sign-in */
    } finally {
      setBusy(null);
    }
  };

  const live = items?.filter((c) => !c.deleted) ?? [];

  return (
    <div className="mt-3">
      <button onClick={toggle} className="font-mono text-[11px] text-dim transition-colors hover:text-muted">
        {open ? "hide comments" : "moderate comments"}
      </button>
      {open && (
        <div className="mt-2 space-y-1.5">
          {items === null && <p className="font-mono text-[11px] text-dim">loading…</p>}
          {items !== null && live.length === 0 && (
            <p className="font-mono text-[11px] text-dim">no comments</p>
          )}
          {live.map((c) => (
            <div key={c.id} className="rule-b flex items-start justify-between gap-3 pb-1.5">
              <span className="flex min-w-0 items-start gap-2">
                <Avatar seed={c.avatar_seed || c.wallet} size={16} />
                <span className="min-w-0">
                  <span className="font-mono text-[10px] text-dim">
                    {shortHash(c.wallet)} · {relTime(c.created_at)}
                  </span>
                  <span className="block truncate text-[12px] text-muted">{c.body}</span>
                </span>
              </span>
              <button
                onClick={() => del(c.id)}
                disabled={busy === c.id}
                className="shrink-0 font-mono text-[11px] text-down transition-[filter] hover:brightness-125 disabled:opacity-50"
              >
                {busy === c.id ? "…" : "delete"}
              </button>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
