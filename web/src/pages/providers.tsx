import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { Plus, Pencil, Trash2, Activity } from 'lucide-react'
import { Button } from '@/components/ui/button'
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
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { ProviderFormDialog } from '@/components/provider-form'
import { providersApi } from '@/api/providers'
import type { Provider, CreateProviderRequest } from '@/types/provider'
import { toast } from 'sonner'

function StatusBadge({ status }: { status?: string }) {
  const value = status || 'unknown'
  const variant =
    value === 'healthy'
      ? 'default'
      : value === 'degraded'
        ? 'secondary'
        : value === 'unhealthy'
          ? 'destructive'
          : 'outline'

  return <Badge variant={variant as never}>{value}</Badge>
}

function ProviderLabels({ labels }: { labels?: Record<string, string> }) {
  const entries = Object.entries(labels || {})
  if (entries.length === 0) {
    return <span className="text-muted-foreground">-</span>
  }

  const visible = entries.slice(0, 2)
  return (
    <div className="flex flex-wrap gap-1">
      {visible.map(([key, value]) => (
        <Badge key={key} variant="outline" className="font-normal">
          {key}={value}
        </Badge>
      ))}
      {entries.length > visible.length && (
        <Badge variant="outline" className="font-normal">
          +{entries.length - visible.length}
        </Badge>
      )}
    </div>
  )
}

export function ProvidersPage() {
  const queryClient = useQueryClient()
  const [formOpen, setFormOpen] = useState(false)
  const [editingProvider, setEditingProvider] = useState<Provider | null>(null)
  const [deletingProvider, setDeletingProvider] = useState<Provider | null>(
    null
  )
  const [formVersion, setFormVersion] = useState(0)

  const { data: providers, isLoading } = useQuery({
    queryKey: ['providers'],
    queryFn: () => providersApi.list(),
  })

  const createMutation = useMutation({
    mutationFn: providersApi.create,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['providers'] })
      setFormOpen(false)
      toast.success('Provider 创建成功')
    },
  })

  const updateMutation = useMutation({
    mutationFn: ({
      name,
      data,
    }: {
      name: string
      data: Partial<CreateProviderRequest>
    }) => providersApi.update(name, data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['providers'] })
      setFormOpen(false)
      setEditingProvider(null)
      toast.success('Provider 更新成功')
    },
  })

  const deleteMutation = useMutation({
    mutationFn: (name: string) => providersApi.delete(name),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['providers'] })
      setDeletingProvider(null)
      toast.success('Provider 删除成功')
    },
  })

  const checkMutation = useMutation({
    mutationFn: providersApi.check,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['providers'] })
      toast.success('健康检查完成')
    },
  })

  const handleCreate = (data: CreateProviderRequest) => {
    createMutation.mutate(data)
  }

  const handleUpdate = (data: CreateProviderRequest) => {
    if (!editingProvider) return
    // 编辑时剔除 name，且 api_key 为空时不提交
    const { api_key, ...rest } = data
    const payload: Partial<CreateProviderRequest> = { ...rest }
    delete payload.name
    if (api_key) {
      payload.api_key = api_key
    }
    updateMutation.mutate({ name: editingProvider.name, data: payload })
  }

  const handleDelete = () => {
    if (deletingProvider) {
      deleteMutation.mutate(deletingProvider.name)
    }
  }

  const openCreate = () => {
    setEditingProvider(null)
    setFormVersion((current) => current + 1)
    setFormOpen(true)
  }

  const openEdit = (provider: Provider) => {
    setEditingProvider(provider)
    setFormVersion((current) => current + 1)
    setFormOpen(true)
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-semibold">Provider 管理</h1>
        <div className="flex gap-2">
          <Button
            variant="outline"
            onClick={() => checkMutation.mutate()}
            disabled={checkMutation.isPending}
          >
            <Activity className="mr-2 h-4 w-4" />
            {checkMutation.isPending ? '检查中...' : '健康检查'}
          </Button>
          <Button onClick={openCreate}>
            <Plus className="mr-2 h-4 w-4" />
            创建 Provider
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
                <TableHead>名称</TableHead>
                <TableHead>模型</TableHead>
                <TableHead>类型</TableHead>
                <TableHead>标签</TableHead>
                <TableHead>状态</TableHead>
                <TableHead>请求数</TableHead>
                <TableHead>错误率</TableHead>
                <TableHead>平均延迟</TableHead>
                <TableHead className="text-right">操作</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {providers?.length === 0 && (
                <TableRow>
                  <TableCell
                    colSpan={9}
                    className="text-muted-foreground text-center"
                  >
                    暂无 Provider
                  </TableCell>
                </TableRow>
              )}
              {providers?.map((provider) => (
                <TableRow key={provider.name}>
                  <TableCell className="font-medium">
                    {provider.name}
                    {!provider.enabled && (
                      <Badge variant="outline" className="ml-2">
                        已禁用
                      </Badge>
                    )}
                  </TableCell>
                  <TableCell>{provider.model}</TableCell>
                  <TableCell>{provider.type}</TableCell>
                  <TableCell>
                    <ProviderLabels labels={provider.labels} />
                  </TableCell>
                  <TableCell>
                    <StatusBadge
                      status={provider.status || provider.health_status}
                    />
                  </TableCell>
                  <TableCell>{provider.total_requests ?? 0}</TableCell>
                  <TableCell>
                    {((provider.error_rate ?? 0) * 100).toFixed(2)}%
                  </TableCell>
                  <TableCell>{provider.avg_latency_ms ?? 0} ms</TableCell>
                  <TableCell className="text-right">
                    <div className="flex justify-end gap-2">
                      <Button
                        variant="ghost"
                        size="icon"
                        onClick={() => openEdit(provider)}
                      >
                        <Pencil className="h-4 w-4" />
                      </Button>
                      <Button
                        variant="ghost"
                        size="icon"
                        onClick={() => setDeletingProvider(provider)}
                      >
                        <Trash2 className="text-destructive h-4 w-4" />
                      </Button>
                    </div>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}

      <ProviderFormDialog
        key={`${editingProvider?.name ?? 'new'}-${formVersion}`}
        open={formOpen}
        onOpenChange={setFormOpen}
        initial={editingProvider}
        onSubmit={editingProvider ? handleUpdate : handleCreate}
        loading={createMutation.isPending || updateMutation.isPending}
      />

      <Dialog
        open={!!deletingProvider}
        onOpenChange={(open) => !open && setDeletingProvider(null)}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>确认删除</DialogTitle>
            <DialogDescription>
              确定要删除 Provider「{deletingProvider?.name}
              」吗？此操作不可撤销。
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button
              variant="outline"
              onClick={() => setDeletingProvider(null)}
              disabled={deleteMutation.isPending}
            >
              取消
            </Button>
            <Button
              variant="destructive"
              onClick={handleDelete}
              disabled={deleteMutation.isPending}
            >
              {deleteMutation.isPending ? '删除中...' : '删除'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
