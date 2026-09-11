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
import { auditApi, type AuditLog } from '@/api/audit'
import { JsonBlock } from '@/components/json-block'

function formatTime(value?: string) {
  if (!value) return '-'
  try {
    return new Date(value).toLocaleString('zh-CN')
  } catch {
    return value
  }
}

function CodeValue({ children }: { children: React.ReactNode }) {
  return (
    <code className="rounded bg-muted px-1.5 py-0.5 text-xs font-mono">
      {children}
    </code>
  )
}

export function AuditPage() {
  const [filters, setFilters] = useState({
    action: '',
    resource_type: '',
    resource_id: '',
    actor_user_id: '',
  })
  const [appliedFilters, setAppliedFilters] = useState(filters)
  const [selectedLog, setSelectedLog] = useState<AuditLog | null>(null)

  const { data: listData, isLoading } = useQuery({
    queryKey: ['audit', appliedFilters],
    queryFn: () => auditApi.list({ ...appliedFilters, limit: 100 }),
  })

  const handleSearch = () => {
    setAppliedFilters(filters)
  }

  return (
    <div className="space-y-4">
      <h1 className="text-2xl font-semibold">审计日志</h1>

      <div className="grid gap-2 md:grid-cols-5">
        {[
          { key: 'action', label: 'Action' },
          { key: 'resource_type', label: '资源类型' },
          { key: 'resource_id', label: '资源 ID' },
          { key: 'actor_user_id', label: '操作者 ID' },
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
                <TableHead>时间</TableHead>
                <TableHead>Action</TableHead>
                <TableHead>资源类型</TableHead>
                <TableHead>资源 ID</TableHead>
                <TableHead>操作者</TableHead>
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
                    暂无审计日志
                  </TableCell>
                </TableRow>
              )}
              {listData?.Items.map((log) => (
                <TableRow key={log.id}>
                  <TableCell className="text-xs">
                    {formatTime(log.created_at)}
                  </TableCell>
                  <TableCell>
                    <Badge variant="outline">{log.action}</Badge>
                  </TableCell>
                  <TableCell>{log.resource_type}</TableCell>
                  <TableCell className="font-mono text-xs max-w-[200px] truncate">
                    {log.resource_id || '-'}
                  </TableCell>
                  <TableCell className="font-mono text-xs">
                    {log.actor_user_id || '-'}
                  </TableCell>
                  <TableCell className="text-right">
                    <Button
                      variant="ghost"
                      size="icon"
                      onClick={() => setSelectedLog(log)}
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
        open={!!selectedLog}
        onOpenChange={(open) => !open && setSelectedLog(null)}
      >
        <DialogContent className="max-w-4xl overflow-hidden lg:max-w-5xl">
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2 font-mono text-sm">
              <span className="text-muted-foreground">Audit</span>
              {selectedLog?.id}
            </DialogTitle>
          </DialogHeader>
          {selectedLog && (
            <div className="min-w-0 space-y-4">
              <div className="grid grid-cols-1 gap-3 text-sm sm:grid-cols-2">
                <div>
                  <span className="text-muted-foreground">Action: </span>
                  <Badge variant="outline">{selectedLog.action}</Badge>
                </div>
                <div>
                  <span className="text-muted-foreground">Resource: </span>
                  <CodeValue>
                    {selectedLog.resource_type}/{selectedLog.resource_id || '-'}
                  </CodeValue>
                </div>
                <div>
                  <span className="text-muted-foreground">Actor User: </span>
                  <CodeValue>{selectedLog.actor_user_id || '-'}</CodeValue>
                </div>
                <div>
                  <span className="text-muted-foreground">Actor API Key: </span>
                  <CodeValue>{selectedLog.actor_api_key_id || '-'}</CodeValue>
                </div>
                <div>
                  <span className="text-muted-foreground">Role: </span>
                  <CodeValue>{selectedLog.actor_role || '-'}</CodeValue>
                </div>
                <div>
                  <span className="text-muted-foreground">IP: </span>
                  <CodeValue>{selectedLog.ip_address || '-'}</CodeValue>
                </div>
                <div>
                  <span className="text-muted-foreground">Request ID: </span>
                  <CodeValue>{selectedLog.request_id || '-'}</CodeValue>
                </div>
                <div>
                  <span className="text-muted-foreground">Tenant: </span>
                  <CodeValue>{selectedLog.tenant_id}</CodeValue>
                </div>
                <div className="sm:col-span-2">
                  <span className="text-muted-foreground">Time: </span>
                  {formatTime(selectedLog.created_at)}
                </div>
              </div>
              <JsonBlock title="Payload" value={selectedLog.payload} />
            </div>
          )}
        </DialogContent>
      </Dialog>
    </div>
  )
}
