import { useState } from 'react'

// CopyButton writes to the clipboard only on explicit user click — never
// automatically on render or on data arrival — and shows transient
// success/failure feedback.

interface CopyButtonProps {
  text: string
  label?: string
  className?: string
}

export function CopyButton({ text, label = 'URLをコピー', className }: CopyButtonProps) {
  const [status, setStatus] = useState<'idle' | 'ok' | 'error'>('idle')

  const handleClick = async () => {
    try {
      await navigator.clipboard.writeText(text)
      setStatus('ok')
    } catch {
      setStatus('error')
    } finally {
      setTimeout(() => setStatus('idle'), 2000)
    }
  }

  return (
    <button
      type="button"
      onClick={handleClick}
      className={
        className ??
        'px-3 py-1.5 text-sm rounded-md bg-gray-100 text-gray-700 hover:bg-gray-200 transition-colors'
      }
    >
      {status === 'ok' ? 'コピーしました' : status === 'error' ? 'コピー失敗' : label}
    </button>
  )
}
