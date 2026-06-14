export function EmptyState({ error }: { error: string | null }) {
  return (
    <div className="flex h-[70vh] flex-col items-center justify-center gap-3">
      <span className="h-2 w-2 animate-pulse rounded-full bg-accent" />
      <p className="text-xs text-muted">
        {error ? "backend unreachable" : "scanning docker compose services…"}
      </p>
      <p className="text-[11px] text-muted">services exposing a /metrics endpoint appear here as tabs</p>
    </div>
  );
}
