"use client";

import { useCallback, useEffect, useState } from "react";

// Light or dark, chosen in this browser; "system" follows the device. The choice is set on
// <html> as data-theme ("light", "dark", or none for system) before the page paints, by
// themeScript in the root layout, and kept in localStorage.

export type Theme = "system" | "light" | "dark";

const key = "theme";

// themeScript runs before the page paints, so a chosen theme never flashes the other one.
export const themeScript = `try{var t=localStorage.getItem("${key}");if(t==="light"||t==="dark")document.documentElement.dataset.theme=t}catch(e){}`;

function saved(): Theme {
  try {
    const t = localStorage.getItem(key);
    return t === "light" || t === "dark" ? t : "system";
  } catch {
    return "system";
  }
}

const systemDark = () => typeof window !== "undefined" && window.matchMedia("(prefers-color-scheme: dark)").matches;

// useTheme is the choice and whether the page is dark now, and a way to change it.
export function useTheme() {
  const [theme, setTheme] = useState<Theme>("system");
  const [dark, setDark] = useState(false);

  useEffect(() => {
    const t = saved();
    setTheme(t);
    setDark(t === "dark" || (t === "system" && systemDark()));
    const media = window.matchMedia("(prefers-color-scheme: dark)");
    const follow = () => setDark(saved() === "dark" || (saved() === "system" && media.matches));
    media.addEventListener("change", follow);
    return () => media.removeEventListener("change", follow);
  }, []);

  const choose = useCallback((t: Theme) => {
    const root = document.documentElement;
    if (t === "system") delete root.dataset.theme;
    else root.dataset.theme = t;
    try {
      if (t === "system") localStorage.removeItem(key);
      else localStorage.setItem(key, t);
    } catch {}
    setTheme(t);
    setDark(t === "dark" || (t === "system" && systemDark()));
  }, []);

  return { theme, dark, choose };
}
