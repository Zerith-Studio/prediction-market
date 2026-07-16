/** @type {import('tailwindcss').Config} */
module.exports = {
  content: ["./src/app/**/*.{ts,tsx}", "./src/components/**/*.{ts,tsx}"],
  presets: [require("nativewind/preset")],
  theme: {
    extend: {
      colors: {
        bg: "#0a0a0b",
        ink: "#f4f5f7",
        muted: "#9297a0",
        dim: "#565b63",
        line: "#1b1c20",
        line2: "#292b30",
        accent: "#34d399",
        down: "#f2637e",
      },
    },
  },
  plugins: [],
};
