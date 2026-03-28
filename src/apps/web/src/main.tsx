import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { BrowserRouter, HashRouter } from 'react-router-dom'
import { ToastProvider } from '@arkloop/shared'
import './index.css'
import App from './App.tsx'
import { LocaleProvider } from './contexts/LocaleContext'
import { ThemeProvider } from './contexts/ThemeContext'
import { AppearanceProvider } from './contexts/AppearanceContext'

const Router = window.location.protocol === 'file:' ? HashRouter : BrowserRouter

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <Router>
      <ThemeProvider>
        <AppearanceProvider>
          <LocaleProvider>
            <ToastProvider>
              <App />
            </ToastProvider>
          </LocaleProvider>
        </AppearanceProvider>
      </ThemeProvider>
    </Router>
  </StrictMode>,
)
