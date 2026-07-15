import type { Config } from "tailwindcss";

const config: Config = {
  content: [
    "./app/**/*.{ts,tsx}",
    "./components/**/*.{ts,tsx}",
    "./lib/**/*.{ts,tsx}",
  ],
  theme: {
    extend: {
      colors: {
        bg: "#07090d",
        panel: "#11161f",
        panel2: "#141b26",
        line: "#1e2735",
        line2: "#2a3547",
        ink: "#eef2f8",
        muted: "#8b98ad",
        dim: "#5c6a7e",
        yes: { DEFAULT: "#22e08a", ink: "#052015", dim: "#0e5c3a" },
        no: { DEFAULT: "#ff4d6a", ink: "#2a0710", dim: "#5c1224" },
        accent: "#ffd21e",
        verify: "#00e0ff",
      },
      fontFamily: {
        sans: ["var(--font-sans)", "system-ui", "sans-serif"],
        mono: ["var(--font-mono)", "ui-monospace", "monospace"],
      },
      keyframes: {
        pulse: {
          "70%": { boxShadow: "0 0 0 9px rgba(255,46,77,0)" },
          "100%": { boxShadow: "0 0 0 0 rgba(255,46,77,0)" },
        },
        flashYes: {
          "0%": { backgroundColor: "rgba(34,224,138,.35)" },
          "100%": { backgroundColor: "transparent" },
        },
        flashNo: {
          "0%": { backgroundColor: "rgba(255,77,106,.35)" },
          "100%": { backgroundColor: "transparent" },
        },
      },
      animation: {
        "live-pulse": "pulse 1.6s infinite",
        "flash-yes": "flashYes .9s ease-out",
        "flash-no": "flashNo .9s ease-out",
      },
    },
  },
  plugins: [],
};

export default config;
