// Tailwind v3 writes some modern CSS. Student pages must work on about Safari 13 and
// Chrome 80, so postcss-preset-env adds fallbacks for what matters for layout
// (for example inset becomes top, right, bottom and left).
export default {
  plugins: {
    tailwindcss: {},
    'postcss-preset-env': {
      browsers: ['safari >= 13', 'chrome >= 80'],
      features: {
        'custom-properties': false,
        'cascade-layers': false,
      },
    },
    autoprefixer: {},
  },
};
