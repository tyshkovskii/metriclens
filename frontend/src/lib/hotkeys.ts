export type Hotkey = {
  keys: string[];
  label: string;
};

export type HotkeyGroup = {
  name: string;
  hotkeys: Hotkey[];
};

export const HOTKEY_GROUPS: HotkeyGroup[] = [
  {
    name: "Navigation",
    hotkeys: [
      { keys: ["1-9"], label: "Switch target" },
      { keys: ["N"], label: "Next target" },
      { keys: ["P"], label: "Previous target" },
      { keys: ["/"], label: "Focus metric search" },
    ],
  },
  {
    name: "Timeline",
    hotkeys: [
      { keys: ["Left", "Right"], label: "Scrub 5 seconds" },
      { keys: ["L"], label: "Return to live" },
      { keys: ["R"], label: "Refresh target" },
    ],
  },
  {
    name: "Interface",
    hotkeys: [
      { keys: ["T"], label: "Toggle theme" },
      { keys: ["?"], label: "Show shortcuts" },
      { keys: ["Esc"], label: "Close or blur" },
    ],
  },
];
