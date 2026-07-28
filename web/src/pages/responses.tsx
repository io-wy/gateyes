import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Search, Eye } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Badge } from '@/components/ui/badge'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { responsesApi } from '@/api/responses'
import { JsonBlock } from '@/components/json-block'

const DETAIL_TABS = [
  { id: 'request', label: 'Request' },
  { id: 'response', label: 'Response' },
  { id: 'trace', label: 'Route Trace' },
] as const

export function ResponsesPage() {
  const [filters, setFilters] = useState({
    provider_name: '',
    model: '',
    status: '',
    project_id: '',
    api_key_id: '',
    user_id: '',
    q: '',
  })
  const [appliedFilters, setAppliedFilters] = useState(filters)
  const [selectedId, setSelectedId] = useState<string | null>(null)
  const [detailTab, setDetailTab] = useState<string>('request')

  const { data: listData, isLoading } = useQuery({
    queryKey: ['responses', appliedFilters],
    queryFn: () => responsesApi.list({ ...appliedFilters, limit: 100 }),
  })

  const { data: detailData } = useQuery({
    queryKey: ['response-detail', selectedId],
    queryFn: () => responsesApi.detail(selectedId!),
    enabled: !!selectedId,
  })

  const handleSearch = () => {
    setAppliedFilters(filters)
  }

  return (
    <div className="space-y-4">
      <h1 className="text-2xl font-semibold">响应记录</h1>

      <div className="grid gap-2 md:grid-cols-4">
        {[
          { key: 'provider_name', label: 'Provider' },
          { key: 'model', label: '模型' },
          { key: 'status', label: '状态' },
          { key: 'project_id', label: 'Project ID' },
          { key: 'api_key_id', label: 'API Key ID' },
          { key: 'user_id', label: 'User ID' },
          { key: 'q', label: '关键词' },
        ].map((field) => (
          <div key={field.key} className="space-y-1">
            <Label className="text-xs">{field.label}</Label>
            <Input
              value={filters[field.key as keyof typeof filters]}
              onChange={(e) =>
                setFilters({ ...filters, [field.key]: e.target.value })
              }
              onKeyDown={(e) => e.key === 'Enter' && handleSearch()}
            />
          </div>
        ))}
        <div className="flex items-end">
          <Button onClick={handleSearch}>
            <Search className="mr-2 h-4 w-4" />
            查询
          </Button>
        </div>
      </div>

      {isLoading ? (
        <div className="text-muted-foreground">加载中...</div>
      ) : (
        <div className="rounded-md border">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>ID</TableHead>
                <TableHead>Provider</TableHead>
                <TableHead>模型</TableHead>
                <TableHead>状态</TableHead>
                <TableHead>创建时间</TableHead>
                <TableHead className="text-right">操作</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {listData?.Items.length === 0 && (
                <TableRow>
                  <TableCell
                    colSpan={6}
                    className="text-muted-foreground text-center"
                  >
                    暂无响应记录
                  </TableCell>
                </TableRow>
              )}
              {listData?.Items.map((response) => (
                <TableRow key={response.id}>
                  <TableCell className="font-mono text-xs">
                    {response.id}
                  </TableCell>
                  <TableCell>{response.provider_name}</TableCell>
                  <TableCell>{response.model}</TableCell>
                  <TableCell>
                    <Badge variant="outline">{response.status}</Badge>
                  </TableCell>
                  <TableCell>{response.created_at}</TableCell>
                  <TableCell className="text-right">
                    <Button
                      variant="ghost"
                      size="icon"
                      onClick={() => setSelectedId(response.id)}
                      title="查看详情"
                    >
                      <Eye className="h-4 w-4" />
                    </Button>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}

      <div className="text-muted-foreground text-sm">
        共 {listData?.Total ?? 0} 条记录
      </div>

      <Dialog
        open={!!selectedId}
        onOpenChange={(open) => !open && setSelectedId(null)}
      >
        <DialogContent className="max-w-4xl overflow-hidden lg:max-w-5xl">
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2 font-mono text-sm">
              <span className="text-muted-foreground">Response</span>
              {selectedId}
            </DialogTitle>
          </DialogHeader>
          {detailData && (
            <div className="min-w-0 space-y-3">
              <div className="flex flex-wrap gap-2 text-xs text-muted-foreground">
                <span>Provider: <strong className="text-foreground">{detailData.provider_name}</strong></span>
                <span>Model: <strong className="text-foreground">{detailData.model}</strong></span>
                <span>Status: <Badge variant="outline" className="text-xs">{detailData.status}</Badge></span>
              </div>
              <div className="flex gap-1 border-b">
                {DETAIL_TABS.map((tab) => (
                  <button
                    key={tab.id}
                    type="button"
                    className={`px-3 py-2 text-sm transition-colors ${
                      detailTab === tab.id
                        ? 'border-b-2 border-primary font-medium'
                        : 'text-muted-foreground hover:text-foreground'
                    }`}
                    onClick={() => setDetailTab(tab.id)}
                  >
                    {tab.label}
                  </button>
                ))}
              </div>
              {detailTab === 'request' && (
                <JsonBlock title="Request Body" value={detailData.request_body} />
              )}
              {detailTab === 'response' && (
                <JsonBlock title="Response Body" value={detailData.response_body} />
              )}
              {detailTab === 'trace' && (
                <JsonBlock title="Route Trace" value={detailData.route_trace} />
              )}
            </div>
          )}
        </DialogContent>
      </Dialog>
    </div>
  )
}
