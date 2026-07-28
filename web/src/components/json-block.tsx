import { useMemo, useState } from 'react'
import { Check, Copy } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'

function prettyValue(value: unknown) {
  if (typeof value === 'string') {
    return value
  }
  if (value == null) {
    return '{}'
  }
  try {
    return JSON.stringify(value, null, 2)
  } catch {
    return String(value)
  }
}

export function JsonBlock({
  title,
  value,
  className,
}: {
  title: string
  value: unknown
  className?: string
}) {
  const text = useMemo(() => prettyValue(value), [value])
  const [copied, setCopied] = useState(false)

  const handleCopy = async () => {
    await navigator.clipboard.writeText(text)
    setCopied(true)
    window.setTimeout(() => setCopied(false), 1200)
  }

  return (
    <section className={cn('space-y-2', className)}>
      <div className="flex items-center justify-between gap-3">
        <h3 className="text-sm font-medium">{title}</h3>
        <Button variant="ghost" size="sm" onClick={handleCopy}>
          {copied ? <Check className="mr-2 h-4 w-4" /> : <Copy className="mr-2 h-4 w-4" />}
          {copied ? '已复制' : '复制'}
        </Button>
      </div>
      <pre className="max-h-[60vh] overflow-auto rounded-md border bg-muted p-4 text-xs leading-6 whitespace-pre-wrap break-words">
        {text}
      </pre>
    </section>
  )
}
