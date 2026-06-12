import { useCallback, useState } from "react";
import { THEME_KEY, saveString } from "../lib/storage";

export function useTheme() {
  const [dark, setDark] = useState(() => document.documentElement.classList.contains("dark"));

  const toggle = useCallback(() => {
    setDark((current) => {
      const next = !current;
      document.documentElement.classList.toggle("dark", next);
      saveString(THEME_KEY, next ? "dark" : "light");
      return next;
    });
  }, []);

  return { dark, toggle };
}
