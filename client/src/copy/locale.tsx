import { createContext, useContext, useEffect, useMemo, useState, type ReactNode } from 'react';
import { copy as zhCN } from './zh-CN';

export type Locale = 'zh-CN' | 'en-US';

/**
/**
 * Only the shell, the navigation and the dashboard are translated so far. Every
 * other page falls back to the Chinese table through the merge below, so a
 * missing key shows real copy instead of a key name.
 */
const enUS: Record<string, unknown> = {
  brand: 'EasyTier',
  brandTag: 'Console',
  nav: {
    dashboard: 'Overview',
    devices: 'Devices',
    networks: 'Networks',
    settings: 'Appearance',
    account: 'Account',
    adminUsers: 'Users',
    adminSettings: 'System',
    blocked: 'Blocked devices',
    password: 'Password',
    docs: 'Docs',
  },
  shell: {
    disconnected: 'Cannot reach the console',
    skipToContent: 'Skip to content',
    language: 'Language',
    languageZh: (zhCN.shell as { languageZh: string }).languageZh,
    languageEn: 'English',
  },
  common: {
    logout: 'Sign out',
    theme: 'Theme',
    background: 'Background URL',
    save: 'Save',
    loading: 'Loading…',
    retry: 'Retry',
    failed: 'Failed to load',
  },
  dashboard: {
    title: 'Overview',
    devices: 'Devices',
    online: 'Online',
    networks: 'Networks',
    instances: 'Network instances',
    recent: 'Recently online',
    viewAll: 'View all',
    autoRefresh: 'Auto refresh',
    noNetworks: 'No networks yet',
  },
  login: {
    title: 'Sign in',
    subtitle: 'Manage your devices, networks and access rules',
    username: 'Username',
    password: 'Password',
    submit: 'Sign in',
  },
};

function merge(base: unknown, override: unknown): unknown {
  if (override === undefined) return base;
  if (typeof base !== 'object' || base === null || typeof override !== 'object' || override === null) {
    return override;
  }
  const out: Record<string, unknown> = { ...(base as Record<string, unknown>) };
  for (const [key, value] of Object.entries(override as Record<string, unknown>)) {
    out[key] = merge((base as Record<string, unknown>)[key], value);
  }
  return out;
}

export type Copy = typeof zhCN;

const tables: Record<Locale, Copy> = {
  'zh-CN': zhCN,
  'en-US': merge(zhCN, enUS) as Copy,
};

interface LocaleValue {
  locale: Locale;
  setLocale: (next: Locale) => void;
  copy: Copy;
}

const LocaleContext = createContext<LocaleValue>({ locale: 'zh-CN', setLocale: () => {}, copy: zhCN });

const storageKey = 'easytier.console.locale';

export function LocaleProvider({ children }: { children: ReactNode }) {
  const [locale, setLocaleState] = useState<Locale>(() => {
    const stored = typeof window === 'undefined' ? null : window.localStorage.getItem(storageKey);
    return stored === 'en-US' ? 'en-US' : 'zh-CN';
  });

  useEffect(() => {
    window.localStorage.setItem(storageKey, locale);
    document.documentElement.lang = locale;
  }, [locale]);

  const value = useMemo<LocaleValue>(() => ({
    locale,
    setLocale: setLocaleState,
    copy: tables[locale],
  }), [locale]);

  return <LocaleContext.Provider value={value}>{children}</LocaleContext.Provider>;
}

export function useCopy(): Copy {
  return useContext(LocaleContext).copy;
}

export function useLocale() {
  const value = useContext(LocaleContext);
  return { locale: value.locale, setLocale: value.setLocale };
}
