import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { Plus, Pencil, Trash2 } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
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
import { virtualKeysApi } from '@/api/virtual-keys'
import type { VirtualKey, CreateVirtualKeyRequest } from '@/types/virtual-key'
import { toast } from 'sonner'

interface FormState extends CreateVirtualKeyRequest {
  status?: string
}

function parseArray(value: string): string[] {
  return value
    .split(',')
    .map((s) => s.trim())
    .filter(Boolean)
}

function formatArray(arr?: string[]): string {
  return (arr || []).join(', ')
}

export function VirtualKeysPage() {
  const queryClient = useQueryClient()
  const [formOpen, setFormOpen] = useState(false)
  const [editingKey, setEditingKey] = useState<VirtualKey | null>(null)
  const [deletingKey, setDeletingKey] = useState<VirtualKey | null>(null)
  const [secretKey, setSecretKey] = useState<VirtualKey | null>(null)

  const { data: listData, isLoading } = useQuery({
    queryKey: ['virtual-keys'],
    queryFn: () => virtualKeysApi.list(),
  })

  const keys = listData?.Items

  const createMutation = useMutation({
    mutationFn: virtualKeysApi.create,
    onSuccess: (data) => {
      queryClient.invalidateQueries({ queryKey: ['virtual-keys'] })
      setFormOpen(false)
      setSecretKey(data)
      toast.success('Virtual Key 创建成功')
    },
  })

  const updateMutation = useMutation({
    mutationFn: ({
      id,
      data,
    }: {
      id: string
      data: Partial<CreateVirtualKeyRequest>
    }) => virtualKeysApi.update(id, data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['virtual-keys'] })
      setFormOpen(false)
      setEditingKey(null)
      toast.success('Virtual Key 更新成功')
    },
  })

  const deleteMutation = useMutation({
    mutationFn: (id: string) => virtualKeysApi.delete(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['virtual-keys'] })
      setDeletingKey(null)
      toast.success('Virtual Key 删除成功')
    },
  })

  const [form, setForm] = useState<FormState>({
    user_id: '',
    api_key_id: '',
    project_id: '',
    name: '',
    budget_usd: 0,
    budget_policy: '',
    rate_limit_qps: 0,
    allowed_models: [],
    allowed_providers: [],
    callback_url: '',
    status: 'active',
  })

  const openCreate = () => {
    setEditingKey(null)
    setForm({
      user_id: '',
      api_key_id: '',
      project_id: '',
      name: '',
      budget_usd: 0,
      budget_policy: '',
      rate_limit_qps: 0,
      allowed_models: [],
      allowed_providers: [],
      callback_url: '',
      status: 'active',
    })
    setFormOpen(true)
  }

  const openEdit = (key: VirtualKey) => {
    setEditingKey(key)
    setForm({
      user_id: key.user_id,
      api_key_id: key.api_key_id,
      project_id: key.project_id || '',
      name: key.name || '',
      budget_usd: key.budget_usd || 0,
      budget_policy: key.budget_policy || '',
      rate_limit_qps: key.rate_limit_qps || 0,
      allowed_models: key.allowed_models || [],
      allowed_providers: key.allowed_providers || [],
      callback_url: key.callback_url || '',
      status: key.status,
    })
    setFormOpen(true)
  }

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault()
    if (editingKey) {
      updateMutation.mutate({
        id: editingKey.id,
        data: {
          name: form.name,
          status: form.status,
          budget_usd: form.budget_usd,
          budget_policy: form.budget_policy,
          rate_limit_qps: form.rate_limit_qps,
          allowed_models: form.allowed_models,
          allowed_providers: form.allowed_providers,
          callback_url: form.callback_url,
        },
      })
    } else {
      createMutation.mutate(form)
    }
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-semibold">Virtual Key 管理</h1>
        <Button onClick={openCreate}>
          <Plus className="mr-2 h-4 w-4" />
          创建 Virtual Key
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
                <TableHead>Key</TableHead>
                <TableHead>状态</TableHead>
                <TableHead>预算</TableHead>
                <TableHead>QPS</TableHead>
                <TableHead className="text-right">操作</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {keys?.length === 0 && (
                <TableRow>
                  <TableCell
                    colSpan={6}
                    className="text-muted-foreground text-center"
                  >
                    暂无 Virtual Key
                  </TableCell>
                </TableRow>
              )}
              {keys?.map((key) => (
                <TableRow key={key.id}>
                  <TableCell>{key.name || '-'}</TableCell>
                  <TableCell className="font-mono text-xs">{key.key}</TableCell>
                  <TableCell>
                    <Badge
                      variant={
                        key.status === 'active' ? 'default' : 'secondary'
                      }
                    >
                      {key.status}
                    </Badge>
                  </TableCell>
                  <TableCell>
                    ${key.spent_usd?.toFixed(2) || 0} / ${key.budget_usd || 0}
                  </TableCell>
                  <TableCell>{key.rate_limit_qps || 0}</TableCell>
                  <TableCell className="text-right">
                    <div className="flex justify-end gap-2">
                      <Button
                        variant="ghost"
                        size="icon"
                        onClick={() => openEdit(key)}
                      >
                        <Pencil className="h-4 w-4" />
                      </Button>
                      <Button
                        variant="ghost"
                        size="icon"
                        onClick={() => setDeletingKey(key)}
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
              {editingKey ? '编辑 Virtual Key' : '创建 Virtual Key'}
            </DialogTitle>
          </DialogHeader>
          <form onSubmit={handleSubmit} className="space-y-4">
            <div className="grid gap-4 md:grid-cols-2">
              <div className="space-y-2">
                <Label htmlFor="user_id">用户 ID *</Label>
                <Input
                  id="user_id"
                  value={form.user_id}
                  onChange={(e) =>
                    setForm({ ...form, user_id: e.target.value })
                  }
                  required
                  disabled={!!editingKey}
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="api_key_id">API Key ID *</Label>
                <Input
                  id="api_key_id"
                  value={form.api_key_id}
                  onChange={(e) =>
                    setForm({ ...form, api_key_id: e.target.value })
                  }
                  required
                  disabled={!!editingKey}
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
                <Label htmlFor="project_id">Project ID</Label>
                <Input
                  id="project_id"
                  value={form.project_id}
                  onChange={(e) =>
                    setForm({ ...form, project_id: e.target.value })
                  }
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
              <div className="space-y-2">
                <Label htmlFor="rate_limit_qps">QPS 限制</Label>
                <Input
                  id="rate_limit_qps"
                  type="number"
                  value={form.rate_limit_qps}
                  onChange={(e) =>
                    setForm({
                      ...form,
                      rate_limit_qps: parseInt(e.target.value) || 0,
                    })
                  }
                />
              </div>
            </div>

            {[
              {
                key: 'allowed_models',
                label: '允许模型（逗号分隔）',
                value: formatArray(form.allowed_models),
              },
              {
                key: 'allowed_providers',
                label: '允许 Provider（逗号分隔）',
                value: formatArray(form.allowed_providers),
              },
            ].map((field) => (
              <div key={field.key} className="space-y-2">
                <Label htmlFor={field.key}>{field.label}</Label>
                <Input
                  id={field.key}
                  value={field.value}
                  onChange={(e) =>
                    setForm({
                      ...form,
                      [field.key]: parseArray(e.target.value),
                    } as FormState)
                  }
                />
              </div>
            ))}

            <div className="space-y-2">
              <Label htmlFor="callback_url">Callback URL</Label>
              <Input
                id="callback_url"
                value={form.callback_url}
                onChange={(e) =>
                  setForm({ ...form, callback_url: e.target.value })
                }
              />
            </div>

            {editingKey && (
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
        open={!!deletingKey}
        onOpenChange={(open) => !open && setDeletingKey(null)}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>确认删除</DialogTitle>
            <DialogDescription>
              确定要删除 Virtual Key「{deletingKey?.name || deletingKey?.key}
              」吗？
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button
              variant="outline"
              onClick={() => setDeletingKey(null)}
              disabled={deleteMutation.isPending}
            >
              取消
            </Button>
            <Button
              variant="destructive"
              onClick={() =>
                deletingKey && deleteMutation.mutate(deletingKey.id)
              }
              disabled={deleteMutation.isPending}
            >
              {deleteMutation.isPending ? '删除中...' : '删除'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog
        open={!!secretKey}
        onOpenChange={(open) => !open && setSecretKey(null)}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>请保存密钥</DialogTitle>
            <DialogDescription>
              以下密钥仅展示一次，请立即复制保存。
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-2">
            <div className="space-y-1">
              <Label>Token</Label>
              <Input readOnly value={secretKey?.token || ''} />
            </div>
            <div className="space-y-1">
              <Label>Secret</Label>
              <Input readOnly value={secretKey?.secret || ''} />
            </div>
          </div>
          <DialogFooter>
            <Button onClick={() => setSecretKey(null)}>我已保存</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
