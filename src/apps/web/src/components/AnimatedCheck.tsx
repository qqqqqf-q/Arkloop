type Props = {
  size?: number
  color?: string
}

export function AnimatedCheck({ size = 16, color = 'currentColor' }: Props) {
  // Lucide Check path: M4 13l4 4L20 7
  // pathLength 约为 30，足够覆盖整段勾线
  return (
    <svg
      xmlns="http://www.w3.org/2000/svg"
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      stroke={color}
      strokeWidth={2}
      strokeLinecap="round"
      strokeLinejoin="round"
    >
      <style>{`
        @keyframes draw-check {
          from { stroke-dashoffset: 30; }
          to   { stroke-dashoffset: 0; }
        }
      `}</style>
      <path
        d="M4 13l4 4L20 7"
        strokeDasharray={30}
        strokeDashoffset={30}
        style={{
          animation: 'draw-check 0.15s ease-out forwards',
        }}
      />
    </svg>
  )
}
