import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { Plus, Pencil, Trash2, RefreshCw } from 'lucide-react'
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
import { apiKeysApi } from '@/api/api-keys'
import type { APIKey, CreateAPIKeyRequest } from '@/types/api-key'
import { toast } from 'sonner'

interface FormState extends CreateAPIKeyRequest {
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

export function KeysPage() {
  const queryClient = useQueryClient()
  const [formOpen, setFormOpen] = useState(false)
  const [editingKey, setEditingKey] = useState<APIKey | null>(null)
  const [secretKey, setSecretKey] = useState<APIKey | null>(null)

  const { data: keys, isLoading } = useQuery({
    queryKey: ['api-keys'],
    queryFn: () => apiKeysApi.list(),
  })

  const createMutation = useMutation({
    mutationFn: apiKeysApi.create,
    onSuccess: (data) => {
      queryClient.invalidateQueries({ queryKey: ['api-keys'] })
      setFormOpen(false)
      setSecretKey(data)
      toast.success('API Key 创建成功')
    },
  })

  const updateMutation = useMutation({
    mutationFn: ({
      id,
      data,
    }: {
      id: string
      data: Partial<CreateAPIKeyRequest>
    }) => apiKeysApi.update(id, data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['api-keys'] })
      setFormOpen(false)
      setEditingKey(null)
      toast.success('API Key 更新成功')
    },
  })

  const rotateMutation = useMutation({
    mutationFn: apiKeysApi.rotate,
    onSuccess: (data) => {
      queryClient.invalidateQueries({ queryKey: ['api-keys'] })
      setSecretKey(data)
      toast.success('API Key 轮换成功')
    },
  })

  const revokeMutation = useMutation({
    mutationFn: apiKeysApi.revoke,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['api-keys'] })
      toast.success('API Key 已吊销')
    },
  })

  const [form, setForm] = useState<FormState>({
    user_id: '',
    project_id: '',
    budget_usd: 0,
    rate_limit_qps: 0,
    allowed_models: [],
    allowed_providers: [],
    allowed_services: [],
    status: 'active',
  })

  const openCreate = () => {
    setEditingKey(null)
    setForm({
      user_id: '',
      project_id: '',
      budget_usd: 0,
      rate_limit_qps: 0,
      allowed_models: [],
      allowed_providers: [],
      allowed_services: [],
      status: 'active',
    })
    setFormOpen(true)
  }

  const openEdit = (key: APIKey) => {
    setEditingKey(key)
    setForm({
      user_id: key.user_id,
      project_id: key.project_id || '',
      budget_usd: key.budget_usd || 0,
      rate_limit_qps: key.rate_limit_qps || 0,
      allowed_models: key.allowed_models || [],
      allowed_providers: key.allowed_providers || [],
      allowed_services: key.allowed_services || [],
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
          project_id: form.project_id,
          status: form.status,
          budget_usd: form.budget_usd,
          rate_limit_qps: form.rate_limit_qps,
          allowed_models: form.allowed_models,
          allowed_providers: form.allowed_providers,
          allowed_services: form.allowed_services,
        },
      })
    } else {
      createMutation.mutate(form)
    }
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-semibold">API Key 管理</h1>
        <Button onClick={openCreate}>
          <Plus className="mr-2 h-4 w-4" />
          创建 API Key
        </Button>
      </div>

      {isLoading ? (
        <div className="text-muted-foreground">加载中...</div>
      ) : (
        <div className="rounded-md border">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Key</TableHead>
                <TableHead>用户</TableHead>
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
                    暂无 API Key
                  </TableCell>
                </TableRow>
              )}
              {keys?.map((key) => (
                <TableRow key={key.id}>
                  <TableCell className="font-mono text-xs">
                    {key.api_key}
                  </TableCell>
                  <TableCell>{key.user_name || key.user_id}</TableCell>
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
                        onClick={() => rotateMutation.mutate(key.id)}
                        disabled={rotateMutation.isPending}
                        title="轮换"
                      >
                        <RefreshCw className="h-4 w-4" />
                      </Button>
                      <Button
                        variant="ghost"
                        size="icon"
                        onClick={() => openEdit(key)}
                      >
                        <Pencil className="h-4 w-4" />
                      </Button>
                      {key.status !== 'revoked' && (
                        <Button
                          variant="ghost"
                          size="icon"
                          onClick={() => revokeMutation.mutate(key.id)}
                          disabled={revokeMutation.isPending}
                          title="吊销"
                        >
                          <Trash2 className="text-destructive h-4 w-4" />
                        </Button>
                      )}
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
              {editingKey ? '编辑 API Key' : '创建 API Key'}
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
              {
                key: 'allowed_services',
                label: '允许 Service（逗号分隔）',
                value: formatArray(form.allowed_services),
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
              <Input readOnly value={secretKey?.api_secret || ''} />
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
