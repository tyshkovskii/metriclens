export function Keycap({ value, className = "" }: { value: string; className?: string }) {
  return (
    <kbd
      className={`inline-flex h-5 min-w-5 items-center justify-center border border-edge bg-fg/[0.035] px-1.5 text-[10px] font-medium leading-none text-muted shadow-[inset_0_-1px_0_var(--edge)] ${className}`}
    >
      {value}
    </kbd>
  );
}

export function HotkeyHint({
  keys,
  label,
  className = "",
}: {
  keys: string[];
  label?: string;
  className?: string;
}) {
  return (
    <span className={`inline-flex items-center gap-1 ${className}`}>
      {label ? <span>{label}</span> : null}
      {keys.map((key) => (
        <Keycap key={key} value={key} />
      ))}
    </span>
  );
}
