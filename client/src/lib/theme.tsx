import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from 'react';

export type ThemeMode = 'light' | 'dark' | 'auto';
export type Surface = 'procedural' | 'image';

const THEME_KEY = 'theme';
const BACKGROUND_KEY = 'background';

function systemPrefersDark(): boolean {
  return window.matchMedia?.('(prefers-color-scheme: dark)').matches ?? false;
}

function apply(mode: ThemeMode): void {
  const dark = mode === 'dark' || (mode === 'auto' && systemPrefersDark());
  document.documentElement.classList.toggle('dark', dark);
  document.documentElement.style.colorScheme = dark ? 'dark' : 'light';
}

/** Only http(s) is accepted; the value is escaped before touching CSS. */
export function sanitizeBackground(value: string): string | null {
  const trimmed = value.trim();
  if (!trimmed) return null;
  try {
    const parsed = new URL(trimmed);
    if (parsed.protocol !== 'http:' && parsed.protocol !== 'https:') return null;
    return parsed.toString();
  } catch {
    return null;
  }
}

interface ThemeContextValue {
  mode: ThemeMode;
  background: string | null;
  setMode: (mode: ThemeMode) => void;
  setBackground: (value: string) => boolean;
}

const ThemeContext = createContext<ThemeContextValue | null>(null);

export function ThemeProvider({ children }: { children: ReactNode }) {
  const [mode, setModeState] = useState<ThemeMode>(() => {
    const stored = localStorage.getItem(THEME_KEY);
    return stored === 'light' || stored === 'dark' ? stored : 'auto';
  });
  const [background, setBackgroundState] = useState<string | null>(() => {
    const stored = localStorage.getItem(BACKGROUND_KEY);
    return stored ? sanitizeBackground(stored) : null;
  });

  useEffect(() => {
    apply(mode);
    localStorage.setItem(THEME_KEY, mode);
    if (mode !== 'auto') return;
    const media = window.matchMedia?.('(prefers-color-scheme: dark)');
    if (!media) return;
    const listener = () => apply('auto');
    media.addEventListener('change', listener);
    return () => media.removeEventListener('change', listener);
  }, [mode]);

  useEffect(() => {
    const root = document.documentElement;
    if (background) {
      root.style.setProperty('--app-bg-image', 'url(' + JSON.stringify(background) + ')');
      root.dataset.image = 'true';
    } else {
      root.style.removeProperty('--app-bg-image');
      delete root.dataset.image;
    }
  }, [background]);

  const setBackground = useCallback((value: string) => {
    const cleaned = sanitizeBackground(value);
    if (cleaned === null) {
      localStorage.removeItem(BACKGROUND_KEY);
      setBackgroundState(null);
      return value.trim() === '';
    }
    localStorage.setItem(BACKGROUND_KEY, cleaned);
    setBackgroundState(cleaned);
    return true;
  }, []);

  const value = useMemo<ThemeContextValue>(
    () => ({ mode, background, setMode: setModeState, setBackground }),
    [mode, background, setBackground],
  );

  return <ThemeContext.Provider value={value}>{children}</ThemeContext.Provider>;
}

export function useTheme(): ThemeContextValue {
  const context = useContext(ThemeContext);
  if (!context) throw new Error('useTheme must be used inside ThemeProvider');
  return context;
}
