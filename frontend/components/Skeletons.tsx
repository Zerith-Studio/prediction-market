"use client";

function Bar({ className = "" }: { className?: string }) {
  return <div className={`animate-pulse bg-line2/50 ${className}`} aria-hidden />;
}

export function MarketSkeleton() {
  return (
    <div className="space-y-10" aria-busy="true" aria-label="Loading market">
      <div className="space-y-4 py-8">
        <div className="flex justify-between">
          <Bar className="h-3 w-40" />
          <Bar className="h-3 w-52" />
        </div>
        <Bar className="mx-auto h-8 w-72" />
      </div>
      <div className="space-y-4">
        <Bar className="h-5 w-40" />
        <Bar className="h-[190px] w-full sm:h-[240px]" />
      </div>
      <div className="grid gap-10 lg:grid-cols-[1fr_300px]">
        <div className="space-y-2.5">
          {Array.from({ length: 8 }).map((_, i) => (
            <Bar key={i} className="h-5 w-full" />
          ))}
        </div>
        <div className="space-y-4">
          <Bar className="h-8 w-full" />
          <Bar className="h-8 w-full" />
          <Bar className="h-11 w-full" />
        </div>
      </div>
    </div>
  );
}
