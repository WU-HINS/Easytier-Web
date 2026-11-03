import type { Config } from 'tailwindcss';

/**
 * Design tokens are CSS variables (see src/ui/tokens.css) so a host can retheme
 * the console without touching components. Geometry uses the golden-ratio
 * ladder: every size is 8 * phi^(n/2).
 */
const config: Config = {
  darkMode: ['class'],
  content: ['./index.html', './src/**/*.{ts,tsx}'],
  theme: {
    extend: {
      colors: {
        background: 'hsl(var(--background) / <alpha-value>)',
        foreground: 'hsl(var(--foreground) / <alpha-value>)',
        card: 'hsl(var(--card) / <alpha-value>)',
        'card-foreground': 'hsl(var(--card-foreground) / <alpha-value>)',
        muted: 'hsl(var(--muted) / <alpha-value>)',
        'muted-foreground': 'hsl(var(--muted-foreground) / <alpha-value>)',
        primary: 'hsl(var(--primary) / <alpha-value>)',
        'primary-foreground': 'hsl(var(--primary-foreground) / <alpha-value>)',
        destructive: 'hsl(var(--destructive) / <alpha-value>)',
        border: 'hsl(var(--border) / <alpha-value>)',
        input: 'hsl(var(--input) / <alpha-value>)',
        ring: 'hsl(var(--ring) / <alpha-value>)',
        success: 'hsl(var(--success) / <alpha-value>)',
        warning: 'hsl(var(--warning) / <alpha-value>)',
      },
      borderRadius: { DEFAULT: 'var(--radius)', lg: 'calc(var(--radius) * 1.382)', sm: 'calc(var(--radius) * 0.618)' },
      spacing: {
        'phi-1': 'var(--u-1)', 'phi-2': 'var(--u-2)', 'phi-3': 'var(--u-3)', 'phi-4': 'var(--u-4)',
        'phi-5': 'var(--u-5)', 'phi-6': 'var(--u-6)', 'phi-7': 'var(--u-7)', 'phi-8': 'var(--u-8)',
        'phi-9': 'var(--u-9)', 'phi-10': 'var(--u-10)', 'phi-11': 'var(--u-11)', 'phi-12': 'var(--u-12)',
      },
      fontSize: {
        'phi-sm': ['var(--u-5)', { lineHeight: '1.618' }],
        'phi-base': ['var(--u-6)', { lineHeight: '1.618' }],
        'phi-lg': ['var(--u-7)', { lineHeight: '1.5' }],
        'phi-xl': ['var(--u-8)', { lineHeight: '1.382' }],
        'phi-2xl': ['var(--u-9)', { lineHeight: '1.236' }],
        'phi-3xl': ['var(--u-11)', { lineHeight: '1.118' }],
      },
      maxWidth: { content: 'var(--w-content)', narrow: 'var(--w-narrow)' },
      boxShadow: {
        panel: '0 1px 2px hsl(var(--foreground) / 0.06), 0 12px 34px -18px hsl(var(--foreground) / 0.35)',
      },
      keyframes: {
        drift: { '0%,100%': { transform: 'translate3d(0,0,0) scale(1)' }, '50%': { transform: 'translate3d(2%,-2%,0) scale(1.06)' } },
      },
      animation: { drift: 'drift 28s ease-in-out infinite' },
    },
  },
  plugins: [],
};

export default config;
