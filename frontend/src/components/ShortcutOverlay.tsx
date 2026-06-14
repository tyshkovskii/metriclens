import { HOTKEY_GROUPS } from "../lib/hotkeys";
import { HotkeyHint, Keycap } from "./HotkeyHint";

export function ShortcutOverlay({ onClose }: { onClose: () => void }) {
  return (
    <div
      aria-modal="true"
      className="fixed inset-0 z-50 flex items-start justify-center bg-bg/80 px-4 pt-20 backdrop-blur-sm"
      onMouseDown={onClose}
      role="dialog"
    >
      <div
        className="w-full max-w-xl border border-edge bg-bg shadow-[0_18px_60px_color-mix(in_srgb,var(--fg)_14%,transparent)]"
        onMouseDown={(event) => event.stopPropagation()}
      >
        <div className="flex items-center gap-3 border-b border-edge px-4 py-3">
          <h2 className="text-xs uppercase tracking-widest text-muted">shortcuts</h2>
          <button
            className="ml-auto flex items-center gap-2 text-[11px] text-muted hover:text-fg"
            onClick={onClose}
            type="button"
          >
            close
            <Keycap value="Esc" />
          </button>
        </div>
        <div className="grid gap-5 p-4 sm:grid-cols-3">
          {HOTKEY_GROUPS.map((group) => (
            <section key={group.name}>
              <h3 className="mb-2 text-[11px] uppercase tracking-widest text-muted">{group.name}</h3>
              <dl className="space-y-2">
                {group.hotkeys.map((hotkey) => (
                  <div
                    className="flex items-center justify-between gap-3"
                    key={`${group.name}-${hotkey.label}`}
                  >
                    <dt className="text-[11px] text-muted">{hotkey.label}</dt>
                    <dd>
                      <HotkeyHint keys={hotkey.keys} />
                    </dd>
                  </div>
                ))}
              </dl>
            </section>
          ))}
        </div>
      </div>
    </div>
  );
}
