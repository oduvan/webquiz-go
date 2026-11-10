/** @type {import('tailwindcss').Config} */
export default {
  content: ['./*.html', './src/**/*.{ts,tsx}'],
  // Dark mode follows the data-theme attribute that theme.ts sets on <html>.
  darkMode: ['class', '[data-theme="dark"]'],
  theme: {
    // The project's rule: 768 px and less is a phone or tablet layout (the old pages
    // used max-width: 768px). So md: starts at 769 px; the other screens are Tailwind's.
    screens: {
      sm: '640px',
      md: '769px',
      lg: '1024px',
      xl: '1280px',
      '2xl': '1536px',
    },
    extend: {},
  },
  plugins: [],
};
