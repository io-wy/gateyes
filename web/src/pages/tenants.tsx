import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { Plus, Pencil, Trash2, Link2 } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { Checkbox } from '@/components/ui/checkbox'
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
import { tenantsApi } from '@/api/tenants'
import { providersApi } from '@/api/providers'
import type { Tenant, CreateTenantRequest } from '@/types/tenant'
import { toast } from 'sonner'

interface FormState extends CreateTenantRequest {
  status?: string
}

export function TenantsPage() {
  const queryClient = useQueryClient()
  const [formOpen, setFormOpen] = useState(false)
  const [editingTenant, setEditingTenant] = useState<Tenant | null>(null)
  const [deletingTenant, setDeletingTenant] = useState<Tenant | null>(null)
  const [bindingTenant, setBindingTenant] = useState<Tenant | null>(null)

  const { data: tenants, isLoading } = useQuery({
    queryKey: ['tenants'],
    queryFn: () => tenantsApi.list(),
  })

  const { data: providers } = useQuery({
    queryKey: ['providers'],
    queryFn: () => providersApi.list(),
  })

  const { data: tenantDetail } = useQuery({
    queryKey: ['tenant-detail', bindingTenant?.id],
    queryFn: () => tenantsApi.get(bindingTenant!.id),
    enabled: !!bindingTenant,
  })

  const createMutation = useMutation({
    mutationFn: tenantsApi.create,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['tenants'] })
      setFormOpen(false)
      toast.success('Tenant 创建成功')
    },
  })

  const updateMutation = useMutation({
    mutationFn: ({
      id,
      data,
    }: {
      id: string
      data: Partial<CreateTenantRequest>
    }) => tenantsApi.update(id, data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['tenants'] })
      setFormOpen(false)
      setEditingTenant(null)
      toast.success('Tenant 更新成功')
    },
  })

  const deleteMutation = useMutation({
    mutationFn: (id: string) => tenantsApi.delete(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['tenants'] })
      setDeletingTenant(null)
      toast.success('Tenant 删除成功')
    },
  })

  const bindProvidersMutation = useMutation({
    mutationFn: ({ id, providers }: { id: string; providers: string[] }) =>
      tenantsApi.replaceProviders(id, { providers }),
    onSuccess: () => {
      queryClient.invalidateQueries({
        queryKey: ['tenant-detail', bindingTenant?.id],
      })
      toast.success('Provider 绑定已更新')
    },
  })

  const [form, setForm] = useState<FormState>({
    slug: '',
    name: '',
    budget_usd: 0,
    status: 'active',
  })

  const [selectedProviders, setSelectedProviders] = useState<string[]>([])

  const openCreate = () => {
    setEditingTenant(null)
    setForm({ slug: '', name: '', budget_usd: 0, status: 'active' })
    setFormOpen(true)
  }

  const openEdit = (tenant: Tenant) => {
    setEditingTenant(tenant)
    setForm({
      slug: tenant.slug,
      name: tenant.name,
      budget_usd: tenant.budget_usd || 0,
      status: tenant.status,
    })
    setFormOpen(true)
  }

  const openBind = (tenant: Tenant) => {
    setBindingTenant(tenant)
    setSelectedProviders([])
  }

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault()
    if (editingTenant) {
      updateMutation.mutate({
        id: editingTenant.id,
        data: {
          name: form.name,
          status: form.status,
          budget_usd: form.budget_usd,
        },
      })
    } else {
      createMutation.mutate(form)
    }
  }

  const handleBind = () => {
    if (bindingTenant) {
      bindProvidersMutation.mutate({
        id: bindingTenant.id,
        providers: selectedProviders,
      })
    }
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-semibold">Tenant 管理</h1>
        <Button onClick={openCreate}>
          <Plus className="mr-2 h-4 w-4" />
          创建 Tenant
        </Button>
      </div>

      {isLoading ? (
        <div className="text-muted-foreground">加载中...</div>
      ) : (
        <div className="rounded-md border">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Slug</TableHead>
                <TableHead>名称</TableHead>
                <TableHead>状态</TableHead>
                <TableHead>预算</TableHead>
                <TableHead className="text-right">操作</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {tenants?.length === 0 && (
                <TableRow>
                  <TableCell
                    colSpan={5}
                    className="text-muted-foreground text-center"
                  >
                    暂无 Tenant
                  </TableCell>
                </TableRow>
              )}
              {tenants?.map((tenant) => (
                <TableRow key={tenant.id}>
                  <TableCell className="font-mono text-xs">
                    {tenant.slug}
                  </TableCell>
                  <TableCell>{tenant.name || '-'}</TableCell>
                  <TableCell>
                    <Badge
                      variant={
                        tenant.status === 'active' ? 'default' : 'secondary'
                      }
                    >
                      {tenant.status}
                    </Badge>
                  </TableCell>
                  <TableCell>
                    ${tenant.spent_usd?.toFixed(2) || 0} / $
                    {tenant.budget_usd || 0}
                  </TableCell>
                  <TableCell className="text-right">
                    <div className="flex justify-end gap-2">
                      <Button
                        variant="ghost"
                        size="icon"
                        onClick={() => openBind(tenant)}
                        title="Provider 绑定"
                      >
                        <Link2 className="h-4 w-4" />
                      </Button>
                      <Button
                        variant="ghost"
                        size="icon"
                        onClick={() => openEdit(tenant)}
                      >
                        <Pencil className="h-4 w-4" />
                      </Button>
                      <Button
                        variant="ghost"
                        size="icon"
                        onClick={() => setDeletingTenant(tenant)}
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

      <Dialog open={formOpen} onOpenChange={setFormOpen}>
        <DialogContent className="max-w-xl">
          <DialogHeader>
            <DialogTitle>
              {editingTenant ? '编辑 Tenant' : '创建 Tenant'}
            </DialogTitle>
          </DialogHeader>
          <form onSubmit={handleSubmit} className="space-y-4">
            <div className="grid gap-4 md:grid-cols-2">
              {!editingTenant && (
                <div className="space-y-2">
                  <Label htmlFor="id">ID（可选）</Label>
                  <Input
                    id="id"
                    value={form.id || ''}
                    onChange={(e) => setForm({ ...form, id: e.target.value })}
                  />
                </div>
              )}
              <div className="space-y-2">
                <Label htmlFor="slug">Slug *</Label>
                <Input
                  id="slug"
                  value={form.slug}
                  onChange={(e) => setForm({ ...form, slug: e.target.value })}
                  required
                  disabled={!!editingTenant}
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="name">名称</Label>
                <Input
                  id="name"
                  value={form.name}
                  onChange={(e) => setForm({ ...form, name: e.target.value })}
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="budget_usd">预算 (USD)</Label>
                <Input
                  id="budget_usd"
                  type="number"
                  value={form.budget_usd}
                  onChange={(e) =>
                    setForm({
                      ...form,
                      budget_usd: parseFloat(e.target.value) || 0,
                    })
                  }
                />
              </div>
            </div>

            {editingTenant && (
              <div className="flex items-center gap-2">
                <Switch
                  id="status"
                  checked={form.status === 'active'}
                  onCheckedChange={(checked) =>
                    setForm({
                      ...form,
                      status: checked ? 'active' : 'inactive',
                    })
                  }
                />
                <Label htmlFor="status">启用</Label>
              </div>
            )}

            <DialogFooter>
              <Button
                type="button"
                variant="outline"
                onClick={() => setFormOpen(false)}
              >
                取消
              </Button>
              <Button
                type="submit"
                disabled={createMutation.isPending || updateMutation.isPending}
              >
                {createMutation.isPending || updateMutation.isPending
                  ? '保存中...'
                  : '保存'}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      <Dialog
        open={!!deletingTenant}
        onOpenChange={(open) => !open && setDeletingTenant(null)}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>确认删除</DialogTitle>
            <DialogDescription>
              确定要删除 Tenant「{deletingTenant?.name || deletingTenant?.slug}
              」吗？
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button
              variant="outline"
              onClick={() => setDeletingTenant(null)}
              disabled={deleteMutation.isPending}
            >
              取消
            </Button>
            <Button
              variant="destructive"
              onClick={() =>
                deletingTenant && deleteMutation.mutate(deletingTenant.id)
              }
              disabled={deleteMutation.isPending}
            >
              {deleteMutation.isPending ? '删除中...' : '删除'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog
        open={!!bindingTenant}
        onOpenChange={(open) => {
          if (!open) {
            setBindingTenant(null)
            setSelectedProviders([])
          }
        }}
      >
        <DialogContent className="max-w-md">
          <DialogHeader>
            <DialogTitle>
              Provider 绑定：{bindingTenant?.name || bindingTenant?.slug}
            </DialogTitle>
            <DialogDescription>选择该 Tenant 可用的 Provider</DialogDescription>
          </DialogHeader>
          <div className="max-h-72 space-y-2 overflow-auto">
            {providers?.map((provider) => {
              const bound =
                tenantDetail?.providers?.includes(provider.name) ?? false
              const checked = selectedProviders.includes(provider.name) || bound
              return (
                <div key={provider.name} className="flex items-center gap-2">
                  <Checkbox
                    id={`provider-${provider.name}`}
                    checked={checked}
                    onCheckedChange={(checked) => {
                      setSelectedProviders((prev) =>
                        checked
                          ? [...new Set([...prev, provider.name])]
                          : prev.filter((p) => p !== provider.name)
                      )
                    }}
                  />
                  <Label
                    htmlFor={`provider-${provider.name}`}
                    className="text-sm font-normal"
                  >
                    {provider.name} ({provider.model})
                  </Label>
                </div>
              )
            })}
          </div>
          <DialogFooter>
            <Button
              variant="outline"
              onClick={() => setBindingTenant(null)}
              disabled={bindProvidersMutation.isPending}
            >
              取消
            </Button>
            <Button
              onClick={handleBind}
              disabled={bindProvidersMutation.isPending}
            >
              {bindProvidersMutation.isPending ? '保存中...' : '保存'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
