/** @type {import('tailwindcss').Config} */
export default {
  content: ['./*.html', './src/**/*.{ts,tsx}'],
  // Dark mode follows the data-theme attribute that theme.ts sets on <html>.
  darkMode: ['class', '[data-theme="dark"]'],
  theme: { extend: {} },
  plugins: [],
};
