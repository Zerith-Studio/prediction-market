"use client";

import { ArrowUpRight } from "lucide-react";

export function VerifyLink({
  href,
  children,
}: {
  href: string;
  children: React.ReactNode;
}) {
  return (
    <a
      href={href}
      target="_blank"
      rel="noreferrer"
      className="inline-flex items-center gap-1 font-mono text-[12px] text-accent transition-[filter] hover:brightness-125"
    >
      {children}
      <ArrowUpRight size={12} />
    </a>
  );
}
