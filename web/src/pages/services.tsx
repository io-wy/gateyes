import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { Plus, Pencil, Trash2, GitBranch, Play, RotateCcw } from 'lucide-react'
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
import { servicesApi } from '@/api/services'
import { ServiceFormDialog } from '@/components/service-form'
import type {
  Service,
  CreateServiceRequest,
  UpdateServiceRequest,
  ServiceVersion,
} from '@/types/service'
import { toast } from 'sonner'

export function ServicesPage() {
  const queryClient = useQueryClient()
  const [formOpen, setFormOpen] = useState(false)
  const [editingService, setEditingService] = useState<Service | null>(null)
  const [deletingService, setDeletingService] = useState<Service | null>(null)
  const [versionService, setVersionService] = useState<Service | null>(null)

  const { data: services, isLoading } = useQuery({
    queryKey: ['services'],
    queryFn: () => servicesApi.list(),
  })

  const { data: serviceDetail } = useQuery({
    queryKey: ['service-detail', versionService?.id],
    queryFn: () => servicesApi.get(versionService!.id),
    enabled: !!versionService,
  })

  const createMutation = useMutation({
    mutationFn: servicesApi.create,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['services'] })
      setFormOpen(false)
      setEditingService(null)
      toast.success('Service 创建成功')
    },
  })

  const updateMutation = useMutation({
    mutationFn: ({
      id,
      data,
    }: {
      id: string
      data: UpdateServiceRequest
    }) => servicesApi.update(id, data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['services'] })
      setFormOpen(false)
      setEditingService(null)
      toast.success('Service 更新成功')
    },
  })

  const deleteMutation = useMutation({
    mutationFn: (id: string) => servicesApi.delete(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['services'] })
      setDeletingService(null)
      toast.success('Service 删除成功')
    },
  })

  const createVersionMutation = useMutation({
    mutationFn: (id: string) => servicesApi.createVersion(id),
    onSuccess: () => {
      queryClient.invalidateQueries({
        queryKey: ['service-detail', versionService?.id],
      })
      toast.success('新版本已创建')
    },
  })

  const publishMutation = useMutation({
    mutationFn: ({ id, versionId }: { id: string; versionId: string }) =>
      servicesApi.publishVersion(id, versionId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['services'] })
      queryClient.invalidateQueries({
        queryKey: ['service-detail', versionService?.id],
      })
      toast.success('版本已发布')
    },
  })

  const promoteMutation = useMutation({
    mutationFn: (id: string) => servicesApi.promoteStaged(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['services'] })
      queryClient.invalidateQueries({
        queryKey: ['service-detail', versionService?.id],
      })
      toast.success('已 promote')
    },
  })

  const rollbackMutation = useMutation({
    mutationFn: ({ id, versionId }: { id: string; versionId: string }) =>
      servicesApi.rollbackVersion(id, versionId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['services'] })
      queryClient.invalidateQueries({
        queryKey: ['service-detail', versionService?.id],
      })
      toast.success('已回滚')
    },
  })

  const openCreate = () => {
    setEditingService(null)
    setFormOpen(true)
  }

  const openEdit = (service: Service) => {
    setEditingService(service)
    setFormOpen(true)
  }

  const handleSubmit = (
    data: CreateServiceRequest | UpdateServiceRequest
  ) => {
    if (editingService) {
      updateMutation.mutate({ id: editingService.id, data: data as UpdateServiceRequest })
    } else {
      createMutation.mutate(data as CreateServiceRequest)
    }
  }

  const getVersionActions = (version: ServiceVersion) => {
    const actions = []
    if (version.status === 'draft' || version.status === 'staged') {
      actions.push(
        <Button
          key="publish"
          variant="ghost"
          size="sm"
          onClick={() =>
            versionService &&
            publishMutation.mutate({
              id: versionService.id,
              versionId: version.id,
            })
          }
        >
          <Play className="mr-1 h-3 w-3" /> 发布
        </Button>
      )
    }
    if (version.status === 'published') {
      actions.push(
        <Button
          key="rollback"
          variant="ghost"
          size="sm"
          onClick={() =>
            versionService &&
            rollbackMutation.mutate({
              id: versionService.id,
              versionId: version.id,
            })
          }
        >
          <RotateCcw className="mr-1 h-3 w-3" /> 回滚
        </Button>
      )
    }
    return <div className="flex gap-1">{actions}</div>
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-semibold">Service 管理</h1>
        <Button onClick={openCreate}>
          <Plus className="mr-2 h-4 w-4" />
          创建 Service
        </Button>
      </div>

      {isLoading ? (
        <div className="text-muted-foreground">加载中...</div>
      ) : (
        <div className="rounded-md border">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>名称</TableHead>
                <TableHead>Prefix</TableHead>
                <TableHead>发布状态</TableHead>
                <TableHead>启用</TableHead>
                <TableHead className="text-right">操作</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {services?.length === 0 && (
                <TableRow>
                  <TableCell
                    colSpan={5}
                    className="text-muted-foreground text-center"
                  >
                    暂无 Service
                  </TableCell>
                </TableRow>
              )}
              {services?.map((service) => (
                <TableRow key={service.id}>
                  <TableCell>{service.name}</TableCell>
                  <TableCell className="font-mono text-xs">
                    {service.request_prefix}
                  </TableCell>
                  <TableCell>
                    <Badge
                      variant={
                        service.publish_status === 'published'
                          ? 'default'
                          : service.publish_status === 'staged'
                            ? 'secondary'
                            : 'destructive'
                      }
                    >
                      {service.publish_status === 'published'
                        ? '已发布'
                        : service.publish_status === 'staged'
                          ? '待发布'
                          : '未发布'}
                    </Badge>
                  </TableCell>
                  <TableCell>
                    <Badge variant={service.enabled ? 'default' : 'secondary'}>
                      {service.enabled ? '是' : '否'}
                    </Badge>
                  </TableCell>
                  <TableCell className="text-right">
                    <div className="flex justify-end gap-2">
                      {service.publish_status !== 'published' && (
                        <Button
                          variant="ghost"
                          size="icon"
                          onClick={() => setVersionService(service)}
                          aria-label="发布"
                          title="未发布，点击去版本管理发布"
                        >
                          <Play className="text-orange-500 h-4 w-4" />
                        </Button>
                      )}
                      <Button
                        variant="ghost"
                        size="icon"
                        onClick={() => setVersionService(service)}
                        aria-label="版本"
                        title="版本"
                      >
                        <GitBranch className="h-4 w-4" />
                      </Button>
                      <Button
                        variant="ghost"
                        size="icon"
                        onClick={() => openEdit(service)}
                        aria-label="编辑"
                      >
                        <Pencil className="h-4 w-4" />
                      </Button>
                      <Button
                        variant="ghost"
                        size="icon"
                        onClick={() => setDeletingService(service)}
                        aria-label="删除"
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

      <ServiceFormDialog
        open={formOpen}
        onOpenChange={setFormOpen}
        editingService={editingService}
        onSubmit={handleSubmit}
        loading={createMutation.isPending || updateMutation.isPending}
      />

      <Dialog
        open={!!deletingService}
        onOpenChange={(open) => !open && setDeletingService(null)}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>确认删除</DialogTitle>
            <DialogDescription>
              确定要删除 Service「{deletingService?.name}」吗？
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button
              variant="outline"
              onClick={() => setDeletingService(null)}
              disabled={deleteMutation.isPending}
            >
              取消
            </Button>
            <Button
              variant="destructive"
              onClick={() =>
                deletingService && deleteMutation.mutate(deletingService.id)
              }
              disabled={deleteMutation.isPending}
            >
              {deleteMutation.isPending ? '删除中...' : '删除'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog
        open={!!versionService}
        onOpenChange={(open) => !open && setVersionService(null)}
      >
        <DialogContent className="w-[calc(100vw-2rem)] max-w-3xl max-h-[85vh] overflow-y-auto p-4 sm:p-6">
          <DialogHeader className="space-y-2">
            <DialogTitle className="text-base sm:text-lg">Service 版本：{versionService?.name}</DialogTitle>
            <DialogDescription className="text-xs sm:text-sm">
              draft → staged → published；promote 将 staged 提升为 published
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-4">
            <div className="flex flex-wrap gap-2">
              <Button
                variant="outline"
                size="sm"
                onClick={() =>
                  versionService &&
                  createVersionMutation.mutate(versionService.id)
                }
                disabled={createVersionMutation.isPending}
              >
                创建新版本
              </Button>
              {serviceDetail?.versions?.some(
                (v) => v.status === 'staged'
              ) && (
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() =>
                    versionService && promoteMutation.mutate(versionService.id)
                  }
                  disabled={promoteMutation.isPending}
                >
                  Promote Staged
                </Button>
              )}
            </div>
            <div className="overflow-x-auto rounded-md border">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead className="w-16">版本</TableHead>
                    <TableHead className="w-24">状态</TableHead>
                    <TableHead className="min-w-[140px]">创建时间</TableHead>
                    <TableHead className="text-right">操作</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {serviceDetail?.versions?.length === 0 && (
                    <TableRow>
                      <TableCell
                        colSpan={4}
                        className="text-muted-foreground text-center"
                      >
                        暂无版本
                      </TableCell>
                    </TableRow>
                  )}
                  {serviceDetail?.versions?.map((version) => (
                    <TableRow key={version.id}>
                      <TableCell>v{version.version}</TableCell>
                      <TableCell>
                        <Badge
                          variant={
                            version.status === 'published'
                              ? 'default'
                              : version.status === 'staged'
                                ? 'secondary'
                                : 'outline'
                          }
                        >
                          {version.status}
                        </Badge>
                      </TableCell>
                      <TableCell className="text-muted-foreground text-xs whitespace-nowrap">
                        {new Date(version.created_at || '').toLocaleString('zh-CN', {
                          month: 'short',
                          day: 'numeric',
                          hour: '2-digit',
                          minute: '2-digit',
                        })}
                      </TableCell>
                      <TableCell className="text-right">
                        {getVersionActions(version)}
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </div>
          </div>
        </DialogContent>
      </Dialog>
    </div>
  )
}
