// SafeLink renders a URL as a clickable external link only when it parses
// as http/https with a non-empty host. Anything else (file:, javascript:,
// custom schemes, malformed strings) renders as inert text — never
// auto-opened, never sent through window.location.

function isOpenable(rawUrl: string): boolean {
  try {
    const u = new URL(rawUrl)
    return (u.protocol === 'http:' || u.protocol === 'https:') && u.host !== ''
  } catch {
    return false
  }
}

interface SafeLinkProps {
  url: string
  children?: React.ReactNode
  className?: string
}

export function SafeLink({ url, children, className }: SafeLinkProps) {
  if (!isOpenable(url)) {
    return <span className={className}>{children ?? url}</span>
  }
  return (
    <a
      href={url}
      target="_blank"
      rel="noopener noreferrer"
      className={className ?? 'text-blue-600 hover:underline break-all'}
    >
      {children ?? url}
    </a>
  )
}
