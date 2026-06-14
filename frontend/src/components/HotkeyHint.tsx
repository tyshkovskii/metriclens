export function Keycap({ value, className = "" }: { value: string; className?: string }) {
  return (
    <kbd
      className={`inline-flex h-[18px] min-w-[18px] items-center justify-center rounded-[2px] border border-edge bg-transparent px-1.5 text-[10px] font-normal leading-none text-muted ${className}`}
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
