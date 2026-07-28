import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Search } from 'lucide-react'
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
                  <TableCell>{log.created_at}</TableCell>
                  <TableCell>
                    <Badge variant="outline">{log.action}</Badge>
                  </TableCell>
                  <TableCell>{log.resource_type}</TableCell>
                  <TableCell className="font-mono text-xs">
                    {log.resource_id}
                  </TableCell>
                  <TableCell>{log.actor_user_id || '-'}</TableCell>
                  <TableCell className="text-right">
                    <Button
                      variant="ghost"
                      size="sm"
                      onClick={() => setSelectedLog(log)}
                    >
                      详情
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
        <DialogContent className="max-w-2xl">
          <DialogHeader>
            <DialogTitle>审计详情</DialogTitle>
          </DialogHeader>
          <div className="space-y-2 text-sm">
            <div>
              <span className="font-medium">Action: </span>
              {selectedLog?.action}
            </div>
            <div>
              <span className="font-medium">Resource: </span>
              {selectedLog?.resource_type}/{selectedLog?.resource_id}
            </div>
            <div>
              <span className="font-medium">Actor: </span>
              {selectedLog?.actor_user_id || '-'} ({selectedLog?.actor_role})
            </div>
            <div>
              <span className="font-medium">IP: </span>
              {selectedLog?.ip_address}
            </div>
            <div>
              <span className="font-medium">Request ID: </span>
              {selectedLog?.request_id}
            </div>
            <pre className="bg-muted max-h-64 overflow-auto rounded-md p-4 text-xs">
              {JSON.stringify(selectedLog?.payload || {}, null, 2)}
            </pre>
          </div>
        </DialogContent>
      </Dialog>
    </div>
  )
}
