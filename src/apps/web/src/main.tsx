import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { BrowserRouter, HashRouter } from 'react-router-dom'
import { ToastProvider } from '@arkloop/shared'
import '@fontsource-variable/geist/index.css'
import './styles/misans-vf.css'
import './index.css'
import App from './App.tsx'
import { LocaleProvider } from './contexts/LocaleContext'
import { ThemeProvider } from './contexts/ThemeContext'
import { AppearanceProvider } from './contexts/AppearanceContext'
import { BlurWarmup } from './components/BlurWarmup'

const Router = window.location.protocol === 'file:' ? HashRouter : BrowserRouter

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <Router>
      <ThemeProvider>
        <AppearanceProvider>
          <LocaleProvider>
            <ToastProvider>
              <BlurWarmup />
              <App />
            </ToastProvider>
          </LocaleProvider>
        </AppearanceProvider>
      </ThemeProvider>
    </Router>
  </StrictMode>,
)
