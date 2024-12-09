/** @type {import('tailwindcss').Config} */
import daisyui from 'daisyui';

export default {
  content: ['./web/**/*.html', './web/**/*.templ', './web/**/*.go', './web/static/*.js'],
  theme: {
    extend: {},
  },
  plugins: [daisyui],
  daisyui: {
    themes: [
      'light',
      'dark',
      'cupcake',
      'bumblebee',
      'emerald',
      'corporate',
      'synthwave',
      'retro',
      'cyberpunk',
      'valentine',
      'halloween',
      'garden',
      'forest',
      'aqua',
      'lofi',
      'pastel',
      'fantasy',
      'wireframe',
      'black',
      'luxury',
      'dracula',
      'cmyk',
      'autumn',
      'business',
      'acid',
      'lemonade',
      'night',
      'coffee',
      'winter',
      'dim',
      'nord',
      'sunset',
      {
        datastar: {
          primary: '#c9a75f',
          secondary: '#bfdbfe',
          accent: '#7dd3fc',
          neutral: '#444',
          'neutral-content': '#fff',
          'base-100': '#0b1325',
          'base-200': '#1e304a',
          'base-300': '#3a506b',
          info: '#0369a1',
          success: '#69c383',
          warning: '#facc15',
          error: '#e11d48',
        },
      },
    ],
  },
};