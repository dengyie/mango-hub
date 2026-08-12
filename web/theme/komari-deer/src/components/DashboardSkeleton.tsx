export default function DashboardSkeleton() {
  return (
    <div className="dashboard-skeleton container mx-auto space-y-4 px-4 animate-pulse">
      {/* Map placeholder (mirrors node-map-view__surface clamp height) */}
      <div
        className="w-full rounded-2xl border border-white/[0.06] bg-white/[0.04]"
        style={{ height: "clamp(420px, 42vw, 560px)" }}
      />

      {/* Node grid placeholder */}
      <div
        className="grid w-full gap-4 py-3"
        style={{ gridTemplateColumns: "repeat(auto-fill, minmax(284px, 1fr))" }}
      >
        {Array.from({ length: 5 }).map((_, i) => (
          <div
            key={i}
            className="h-[404px] rounded-[14px] border border-white/[0.06] bg-white/[0.04]"
          />
        ))}
      </div>
    </div>
  );
}
