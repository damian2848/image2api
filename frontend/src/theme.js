// Light/dark theme state. Default is LIGHT. The choice persists in localStorage
// and is reflected as a `dark` class on <html>, which drives the CSS-variable
// palette in style.css (and the conditional `.public-dark` override layer).
import { ref } from 'vue'

const KEY = 'gw_theme'
// Explicit user choices remain intact. New sessions start in the light console
// theme so the app has the same predictable default across operating systems.
const saved = localStorage.getItem(KEY)
const initial = saved === 'dark' || saved === 'light' ? saved : 'light'

export const theme = ref(initial)
export const isDark = ref(initial === 'dark')

function apply(t) {
  isDark.value = t === 'dark'
  const el = document.documentElement
  el.classList.toggle('dark', t === 'dark')
}

// Apply at module load so the first paint already matches the resolved choice.
apply(theme.value)

/** Flip light ⇄ dark and persist as the user's explicit choice. */
export function toggleTheme() {
  theme.value = theme.value === 'dark' ? 'light' : 'dark'
  localStorage.setItem(KEY, theme.value)
  apply(theme.value)
}
