import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { BrowserRouter } from 'react-router-dom'
import './index.css'
import App from './App.tsx'
import { LocaleProvider } from './contexts/LocaleContext'
import { ThemeProvider } from './contexts/ThemeContext'
import { AppearanceProvider } from './contexts/AppearanceContext'

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <BrowserRouter>
      <ThemeProvider>
        <AppearanceProvider>
          <LocaleProvider>
            <App />
          </LocaleProvider>
        </AppearanceProvider>
      </ThemeProvider>
    </BrowserRouter>
  </StrictMode>,
)
