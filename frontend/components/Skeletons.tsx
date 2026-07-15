"use client";

function Shimmer({ className = "" }: { className?: string }) {
  return (
    <div
      className={`animate-pulse rounded-lg bg-line2/40 ${className}`}
      aria-hidden
    />
  );
}

export function MarketSkeleton() {
  return (
    <div className="space-y-4" aria-busy="true" aria-label="Loading market">
      <div className="panel pitch-stripes p-6">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-3.5">
            <Shimmer className="h-[52px] w-[52px] rounded-[13px]" />
            <Shimmer className="h-5 w-28" />
          </div>
          <Shimmer className="h-8 w-16" />
          <div className="flex items-center gap-3.5">
            <Shimmer className="h-5 w-28" />
            <Shimmer className="h-[52px] w-[52px] rounded-[13px]" />
          </div>
        </div>
      </div>
      <div className="grid gap-4 lg:grid-cols-[1fr_340px]">
        <div className="panel space-y-2.5 p-5">
          <Shimmer className="h-11 w-full rounded-xl" />
          {Array.from({ length: 7 }).map((_, i) => (
            <Shimmer key={i} className="h-6 w-full" />
          ))}
        </div>
        <div className="panel space-y-3 p-5">
          <Shimmer className="h-12 w-full rounded-xl" />
          <Shimmer className="h-12 w-full rounded-xl" />
          <Shimmer className="h-12 w-full rounded-xl" />
        </div>
      </div>
    </div>
  );
}
