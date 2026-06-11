import { useCallback, useState } from "react";

export function useTheme() {
  const [dark, setDark] = useState(() => document.documentElement.classList.contains("dark"));

  const toggle = useCallback(() => {
    setDark((current) => {
      const next = !current;
      document.documentElement.classList.toggle("dark", next);
      localStorage.setItem("ml-theme", next ? "dark" : "light");
      return next;
    });
  }, []);

  return { dark, toggle };
}
