import type { Config } from "tailwindcss";

const config: Config = {
  future: {
    // gate every hover: behind (hover:hover) and (pointer:fine) — touch
    // devices trigger hover on tap, leaving sticky false hover states
    hoverOnlyWhenSupported: true,
  },
  content: [
    "./app/**/*.{ts,tsx}",
    "./components/**/*.{ts,tsx}",
    "./lib/**/*.{ts,tsx}",
  ],
  theme: {
    extend: {
      colors: {
        bg: "#0a0a0b", // single flat surface — no panels
        ink: "#f4f5f7", // near-white
        muted: "#9297a0", // secondary
        dim: "#565b63", // tertiary / labels
        line: "#1b1c20", // hairline
        line2: "#292b30", // hairline, stronger
        accent: "#34d399", // the one accent: up / yes / positive / on-chain
        down: "#f2637e", // down / no — used sparingly
      },
      fontFamily: {
        sans: ["var(--font-sans)", "system-ui", "sans-serif"],
        mono: ["var(--font-mono)", "ui-monospace", "monospace"],
      },
      transitionTimingFunction: {
        // stock ease-out is too weak to feel intentional; one strong curve,
        // used consistently (easing.dev "strong out")
        "out-strong": "cubic-bezier(0.23, 1, 0.32, 1)",
      },
      borderRadius: {
        // sharp by default
        DEFAULT: "0px",
        sm: "1px",
        md: "2px",
      },
      keyframes: {
        pulse: {
          "70%": { boxShadow: "0 0 0 7px rgba(52,211,153,0)" },
          "100%": { boxShadow: "0 0 0 0 rgba(52,211,153,0)" },
        },
        pulseDown: {
          "70%": { boxShadow: "0 0 0 7px rgba(242,99,126,0)" },
          "100%": { boxShadow: "0 0 0 0 rgba(242,99,126,0)" },
        },
        flashUp: {
          "0%": { color: "#34d399" },
          "100%": { color: "inherit" },
        },
        flashDown: {
          "0%": { color: "#f2637e" },
          "100%": { color: "inherit" },
        },
      },
      animation: {
        "live-pulse": "pulse 1.8s infinite",
        "live-pulse-down": "pulseDown 1.8s infinite",
        "flash-up": "flashUp 1s ease-out",
        "flash-down": "flashDown 1s ease-out",
      },
    },
  },
  plugins: [],
};

export default config;
