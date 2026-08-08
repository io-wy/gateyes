import { Checkbox } from '@/components/ui/checkbox'
import { Label } from '@/components/ui/label'

export interface ScopeOption {
  value: string
  label: string
  description?: string
}

interface ScopeCheckboxGroupProps {
  idPrefix: string
  label: string
  value?: string[]
  options: ScopeOption[]
  emptyText?: string
  onChange: (value: string[]) => void
}

export function ScopeCheckboxGroup({
  idPrefix,
  label,
  value = [],
  options,
  emptyText = '暂无可选项',
  onChange,
}: ScopeCheckboxGroupProps) {
  const mergedOptions: ScopeOption[] = [
    ...options,
    ...value
      .filter(
        (item) => item && !options.some((option) => option.value === item)
      )
      .map((item) => ({ value: item, label: item })),
  ]

  const toggle = (item: string, checked: boolean) => {
    onChange(
      checked
        ? [...new Set([...value, item])]
        : value.filter((current) => current !== item)
    )
  }

  return (
    <div className="space-y-2">
      <Label>{label}</Label>
      <div className="max-h-44 space-y-2 overflow-auto rounded-md border p-3">
        {mergedOptions.length === 0 ? (
          <div className="text-muted-foreground text-sm">{emptyText}</div>
        ) : (
          mergedOptions.map((option, index) => {
            const checkboxID = `${idPrefix}-${index}`
            return (
              <div key={option.value} className="flex items-start gap-2">
                <Checkbox
                  id={checkboxID}
                  checked={value.includes(option.value)}
                  onCheckedChange={(checked) => toggle(option.value, !!checked)}
                />
                <Label
                  htmlFor={checkboxID}
                  className="grid gap-0.5 text-sm font-normal"
                >
                  <span>{option.label}</span>
                  {option.description && (
                    <span className="text-muted-foreground text-xs">
                      {option.description}
                    </span>
                  )}
                </Label>
              </div>
            )
          })
        )}
      </div>
    </div>
  )
}
