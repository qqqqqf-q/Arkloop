import { useEffect, useState } from 'react'

export function BlurWarmup() {
  const [alive, setAlive] = useState(true)

  useEffect(() => {
    const id = requestAnimationFrame(() => requestAnimationFrame(() => setAlive(false)))
    return () => cancelAnimationFrame(id)
  }, [])

  return alive ? <div className="blur-warmup" /> : null
}
