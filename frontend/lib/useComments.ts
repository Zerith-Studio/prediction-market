"use client";

import { useCallback, useEffect, useState } from "react";
import { api, configured, wsUrl } from "./api";
import type { Comment } from "./types";

type WsComment = {
  type: string;
  market_id?: string;
  data?: {
    action?: "new" | "like" | "delete" | "edit";
    comment?: Comment;
    comment_id?: string;
    like_count?: number;
    body?: string;
  };
};

/**
 * Live per-market comments: initial REST fetch + a dedicated WS subscription for
 * `comment` events (new/like/delete). Posts and likes update optimistically; the
 * WS echo dedups by id. Comments are unsigned — `wallet` is the claimed author.
 */
export function useComments(marketId: string, wallet: string | null) {
  const [flat, setFlat] = useState<Comment[]>([]);
  const [loading, setLoading] = useState(true);

  const upsert = useCallback((c: Comment) => {
    setFlat((prev) => {
      const i = prev.findIndex((x) => x.id === c.id);
      if (i === -1) return [...prev, c];
      const next = prev.slice();
      next[i] = { ...next[i], ...c };
      return next;
    });
  }, []);

  // initial load (re-fetches when the viewer wallet changes, for `liked` flags)
  useEffect(() => {
    if (!configured()) {
      setLoading(false);
      return;
    }
    let alive = true;
    api
      .getComments(marketId, wallet)
      .then((cs) => alive && (setFlat(cs), setLoading(false)))
      .catch(() => alive && setLoading(false));
    return () => {
      alive = false;
    };
  }, [marketId, wallet]);

  // live WS (its own connection; filtered to this market's comment events)
  useEffect(() => {
    if (!configured()) return;
    let closed = false;
    let ws: WebSocket | null = null;
    let retry = 0;
    const connect = () => {
      ws = new WebSocket(wsUrl());
      ws.onopen = () => {
        retry = 0;
      };
      ws.onmessage = (e) => {
        let ev: WsComment;
        try {
          ev = JSON.parse(e.data as string);
        } catch {
          return;
        }
        if (ev.type !== "comment" || !ev.data) return;
        const d = ev.data;
        if (d.action === "new" && d.comment) {
          if (d.comment.market_id === marketId) upsert(d.comment);
        } else if (d.action === "like" && d.comment_id && d.like_count != null) {
          setFlat((prev) =>
            prev.map((c) => (c.id === d.comment_id ? { ...c, like_count: d.like_count! } : c))
          );
        } else if (d.action === "edit" && d.comment_id && d.body != null) {
          setFlat((prev) =>
            prev.map((c) => (c.id === d.comment_id ? { ...c, body: d.body!, edited: true } : c))
          );
        } else if (d.action === "delete" && d.comment_id) {
          setFlat((prev) =>
            prev.map((c) => (c.id === d.comment_id ? { ...c, deleted: true, body: "" } : c))
          );
        }
      };
      ws.onclose = () => {
        if (!closed) setTimeout(connect, Math.min(8000, 1000 * 2 ** retry++));
      };
    };
    connect();
    return () => {
      closed = true;
      ws?.close();
    };
  }, [marketId, upsert]);

  const post = useCallback(
    async (body: string, parentId?: string) => {
      if (!wallet) throw new Error("connect a wallet to comment");
      const c = await api.postComment(marketId, { wallet, body, parent_id: parentId });
      upsert(c); // optimistic; the WS echo dedups by id
      return c;
    },
    [marketId, wallet, upsert]
  );

  const like = useCallback(
    async (commentId: string) => {
      if (!wallet) throw new Error("connect a wallet to like");
      const res = await api.likeComment(commentId, wallet);
      setFlat((prev) =>
        prev.map((c) =>
          c.id === commentId ? { ...c, liked: res.liked, like_count: res.like_count } : c
        )
      );
    },
    [wallet]
  );

  const edit = useCallback(
    async (commentId: string, body: string) => {
      if (!wallet) throw new Error("connect a wallet");
      const res = await api.editComment(commentId, wallet, body);
      setFlat((prev) =>
        prev.map((c) => (c.id === commentId ? { ...c, body: res.body, edited: true } : c))
      );
    },
    [wallet]
  );

  const remove = useCallback(
    async (commentId: string) => {
      if (!wallet) throw new Error("connect a wallet");
      await api.deleteComment(commentId, wallet);
      setFlat((prev) =>
        prev.map((c) => (c.id === commentId ? { ...c, deleted: true, body: "" } : c))
      );
    },
    [wallet]
  );

  return {
    comments: buildTree(flat),
    count: flat.filter((c) => !c.deleted).length,
    loading,
    post,
    like,
    edit,
    remove,
  };
}

// buildTree nests one level of replies, drops deleted leaf comments, and keeps a
// deleted parent (rendered "[removed]") when it still has replies. Top-level
// newest-first; replies oldest-first.
function buildTree(flat: Comment[]): Comment[] {
  const byParent = new Map<string, Comment[]>();
  const tops: Comment[] = [];
  for (const c of flat) {
    if (c.parent_id) {
      const arr = byParent.get(c.parent_id) ?? [];
      arr.push(c);
      byParent.set(c.parent_id, arr);
    } else {
      tops.push(c);
    }
  }
  return tops
    .filter((t) => !(t.deleted && !byParent.get(t.id)?.length))
    .map((t) => ({
      ...t,
      replies: (byParent.get(t.id) ?? [])
        .filter((r) => !r.deleted)
        .sort((a, b) => a.created_at.localeCompare(b.created_at)),
    }))
    .sort((a, b) => b.created_at.localeCompare(a.created_at));
}
