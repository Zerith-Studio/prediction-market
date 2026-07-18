"use client";

import { useEffect, useRef, useState } from "react";
import { AnimatePresence, motion } from "framer-motion";
import { Heart, MessageSquare, MoreHorizontal } from "lucide-react";
import type { Comment } from "@/lib/types";
import { useComments } from "@/lib/useComments";
import { usePitchWallet } from "@/lib/wallet";
import { relTime, shortHash } from "@/lib/format";
import { ApiError } from "@/lib/api";
import { Avatar } from "./Avatar";

/**
 * Per-market discussion. Comments are unsigned (the wallet is a claim). Posting
 * is gated on a connected wallet; the feed updates live over WS. Threaded one
 * level, with likes, avatars, author edit/delete (⋯), and admin soft-deletes.
 */
export function Comments({ marketId }: { marketId: string }) {
  const wallet = usePitchWallet();
  const { comments, count, loading, post, like, edit, remove } = useComments(marketId, wallet.address);
  const connected = !!wallet.address;

  return (
    <section className="rule-t py-10">
      <div className="mb-5 flex items-baseline justify-between">
        <h2 className="text-[15px] font-semibold text-ink">Discussion</h2>
        <span className="eyebrow">
          {count} {count === 1 ? "comment" : "comments"}
        </span>
      </div>

      {connected ? (
        <Composer onSubmit={(b) => post(b)} placeholder="Add a comment…" />
      ) : (
        <div className="rule-t rule-b flex items-center justify-between gap-4 py-4">
          <span className="text-[13px] text-muted">Connect your wallet to join the discussion.</span>
          <button
            onClick={wallet.connect}
            disabled={!wallet.ready}
            className="shrink-0 font-mono text-[12.5px] text-accent transition-[filter] hover:brightness-125 disabled:text-dim"
          >
            Connect to comment
          </button>
        </div>
      )}

      <div className="mt-8">
        {loading && <p className="font-mono text-[12px] text-dim">Loading…</p>}
        {!loading && comments.length === 0 && (
          <p className="py-6 text-center font-mono text-[12px] text-dim">
            No comments yet — start the conversation.
          </p>
        )}
        <AnimatePresence initial={false}>
          {comments.map((c) => (
            <motion.div
              key={c.id}
              initial={{ opacity: 0, height: 0 }}
              animate={{ opacity: 1, height: "auto" }}
              exit={{ opacity: 0 }}
              transition={{ duration: 0.2, ease: [0.22, 1, 0.36, 1] }}
              className="overflow-hidden"
            >
              <div className="rule-t py-4">
                <CommentBody
                  c={c}
                  connected={connected}
                  self={wallet.address}
                  onLike={like}
                  onEdit={edit}
                  onDelete={remove}
                  onReply={post}
                />
              </div>
            </motion.div>
          ))}
        </AnimatePresence>
      </div>
    </section>
  );
}

function CommentBody({
  c,
  connected,
  self,
  onLike,
  onEdit,
  onDelete,
  onReply,
  isReply,
}: {
  c: Comment;
  connected: boolean;
  self: string | null;
  onLike: (id: string) => Promise<void>;
  onEdit: (id: string, body: string) => Promise<void>;
  onDelete: (id: string) => Promise<void>;
  onReply?: (body: string, parentId?: string) => Promise<unknown>;
  isReply?: boolean;
}) {
  const [replying, setReplying] = useState(false);
  const [editing, setEditing] = useState(false);
  const [likeBusy, setLikeBusy] = useState(false);
  const own = !!self && c.wallet === self && !c.deleted;

  const doLike = async () => {
    if (!connected || likeBusy) return;
    setLikeBusy(true);
    try {
      await onLike(c.id);
    } catch {
      /* ignore */
    } finally {
      setLikeBusy(false);
    }
  };

  return (
    <div>
      <div className="flex gap-3">
        <Avatar seed={c.avatar_seed || c.wallet} size={28} />
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-2 font-mono text-[11px]">
            <span className="text-muted">{shortHash(c.wallet)}</span>
            {self && c.wallet === self && <span className="text-accent">· you</span>}
            <span className="text-dim">{relTime(c.created_at)}</span>
            {c.edited && !c.deleted && <span className="text-dim">· edited</span>}
            {own && (
              <span className="ml-auto">
                <CommentMenu onEdit={() => setEditing(true)} onDelete={() => onDelete(c.id)} />
              </span>
            )}
          </div>

          {editing ? (
            <div className="mt-2">
              <Composer
                small
                initial={c.body}
                submitLabel="Save"
                placeholder="Edit your comment…"
                onCancel={() => setEditing(false)}
                onSubmit={async (b) => {
                  await onEdit(c.id, b);
                  setEditing(false);
                }}
              />
            </div>
          ) : (
            <p
              className={`mt-1 whitespace-pre-wrap break-words text-[13.5px] leading-relaxed ${
                c.deleted ? "italic text-dim" : "text-muted"
              }`}
            >
              {c.deleted ? "[comment removed]" : c.body}
            </p>
          )}

          {!c.deleted && !editing && (
            <div className="mt-2 flex items-center gap-5 font-mono text-[11px]">
              <button
                onClick={doLike}
                disabled={!connected}
                aria-pressed={c.liked}
                className={`flex items-center gap-1 transition-colors disabled:opacity-40 ${
                  c.liked ? "text-accent" : "text-dim hover:text-muted"
                }`}
              >
                <Heart size={12} fill={c.liked ? "currentColor" : "none"} />
                {c.like_count > 0 ? c.like_count : ""}
              </button>
              {onReply && (
                <button
                  onClick={() => setReplying((v) => !v)}
                  disabled={!connected}
                  className="flex items-center gap-1 text-dim transition-colors hover:text-muted disabled:opacity-40"
                >
                  <MessageSquare size={12} /> Reply
                </button>
              )}
            </div>
          )}

          {replying && connected && onReply && (
            <div className="mt-3">
              <Composer
                small
                placeholder="Write a reply…"
                onCancel={() => setReplying(false)}
                onSubmit={async (b) => {
                  await onReply(b, c.id);
                  setReplying(false);
                }}
              />
            </div>
          )}
        </div>
      </div>

      {!isReply && c.replies && c.replies.length > 0 && (
        <div className="mt-3 space-y-3 border-l border-line2 pl-4 sm:ml-10">
          {c.replies.map((r) => (
            <CommentBody
              key={r.id}
              c={r}
              connected={connected}
              self={self}
              onLike={onLike}
              onEdit={onEdit}
              onDelete={onDelete}
              isReply
            />
          ))}
        </div>
      )}
    </div>
  );
}

function CommentMenu({ onEdit, onDelete }: { onEdit: () => void; onDelete: () => void }) {
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (!open) return;
    const onDown = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false);
    };
    window.addEventListener("mousedown", onDown);
    return () => window.removeEventListener("mousedown", onDown);
  }, [open]);

  return (
    <div ref={ref} className="relative">
      <button
        onClick={() => setOpen((v) => !v)}
        aria-label="Comment actions"
        className="flex text-dim transition-colors hover:text-muted"
      >
        <MoreHorizontal size={15} />
      </button>
      {open && (
        <div className="absolute right-0 top-5 z-10 w-28 overflow-hidden rounded-[3px] border border-line2 bg-bg py-1 shadow-lg shadow-black/40">
          <button
            onClick={() => {
              setOpen(false);
              onEdit();
            }}
            className="block w-full px-3 py-1.5 text-left font-mono text-[11px] text-muted transition-colors hover:bg-line hover:text-ink"
          >
            Edit
          </button>
          <button
            onClick={() => {
              setOpen(false);
              onDelete();
            }}
            className="block w-full px-3 py-1.5 text-left font-mono text-[11px] text-down transition-[filter] hover:bg-line hover:brightness-125"
          >
            Delete
          </button>
        </div>
      )}
    </div>
  );
}

function Composer({
  onSubmit,
  onCancel,
  placeholder,
  initial = "",
  submitLabel = "Post",
  small,
}: {
  onSubmit: (body: string) => Promise<unknown>;
  onCancel?: () => void;
  placeholder: string;
  initial?: string;
  submitLabel?: string;
  small?: boolean;
}) {
  const [body, setBody] = useState(initial);
  const [state, setState] = useState<"idle" | "posting" | "posted">("idle");
  const [err, setErr] = useState<string | null>(null);
  const max = 500;

  const submit = async () => {
    const b = body.trim();
    if (!b || state === "posting") return;
    setState("posting");
    setErr(null);
    try {
      await onSubmit(b);
      if (!onCancel) setBody(""); // keep new-comment box clear; edit/reply boxes unmount
      setState("posted");
      window.setTimeout(() => setState("idle"), 1500);
    } catch (e) {
      setState("idle");
      setErr(e instanceof ApiError ? commentError(e) : "Couldn’t post.");
    }
  };

  return (
    <div>
      <div className="flex items-end gap-3 border-b border-line2 pb-2 transition-colors focus-within:border-accent">
        <textarea
          value={body}
          autoFocus={!!onCancel}
          onChange={(e) => setBody(e.target.value.slice(0, max))}
          onKeyDown={(e) => {
            // Enter posts; Shift+Enter (or Cmd/Ctrl+Enter) inserts a newline.
            if (e.key === "Enter" && !e.shiftKey && !e.metaKey && !e.ctrlKey) {
              e.preventDefault();
              submit();
            }
            if (e.key === "Escape" && onCancel) onCancel();
          }}
          rows={small ? 1 : 2}
          placeholder={placeholder}
          className="min-h-[24px] w-full resize-none bg-transparent text-[13.5px] text-ink outline-none placeholder:text-dim"
        />
        {onCancel && (
          <button
            onClick={onCancel}
            className="shrink-0 font-mono text-[11px] text-dim transition-colors hover:text-muted"
          >
            Cancel
          </button>
        )}
        <button
          onClick={submit}
          disabled={!body.trim() || state === "posting"}
          className="shrink-0 rounded-[2px] bg-ink px-3 py-1.5 font-mono text-[11px] font-semibold text-bg transition-[filter] hover:brightness-90 disabled:opacity-40"
        >
          {state === "posting" ? "…" : state === "posted" ? "✓" : submitLabel}
        </button>
      </div>
      <div className="mt-1 flex items-center justify-between">
        {err ? (
          <span role="alert" className="font-mono text-[11px] text-down">
            {err}
          </span>
        ) : (
          <span className="font-mono text-[10px] text-dim">Enter to send · Shift+Enter for a new line</span>
        )}
        {body.length > max * 0.8 && (
          <span className="font-mono text-[10px] text-dim tnum">
            {body.length}/{max}
          </span>
        )}
      </div>
    </div>
  );
}

function commentError(e: ApiError): string {
  switch (e.status) {
    case 429:
      return "Slow down — too many comments.";
    case 403:
      return "Not your comment.";
    case 400:
      return e.message || "Comment rejected.";
    case 0:
      return "Not connected to the exchange.";
    default:
      return "Couldn’t post — try again.";
  }
}
