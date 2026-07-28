import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { Plus, Pencil, Trash2, BarChart3, RotateCcw } from 'lucide-react'
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
import { usersApi } from '@/api/users'
import type { User, CreateUserRequest } from '@/types/user'
import { toast } from 'sonner'

interface FormState extends CreateUserRequest {
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

export function UsersPage() {
  const queryClient = useQueryClient()
  const [formOpen, setFormOpen] = useState(false)
  const [editingUser, setEditingUser] = useState<User | null>(null)
  const [deletingUser, setDeletingUser] = useState<User | null>(null)
  const [usageUser, setUsageUser] = useState<User | null>(null)
  const [secretUser, setSecretUser] = useState<User | null>(null)

  const { data: users, isLoading } = useQuery({
    queryKey: ['users'],
    queryFn: () => usersApi.list(),
  })

  const { data: usageData } = useQuery({
    queryKey: ['user-usage', usageUser?.id],
    queryFn: () => usersApi.usage(usageUser!.id, 7),
    enabled: !!usageUser,
  })

  const createMutation = useMutation({
    mutationFn: usersApi.create,
    onSuccess: (data) => {
      queryClient.invalidateQueries({ queryKey: ['users'] })
      setFormOpen(false)
      setSecretUser(data)
      toast.success('User 创建成功')
    },
  })

  const updateMutation = useMutation({
    mutationFn: ({
      id,
      data,
    }: {
      id: string
      data: Partial<CreateUserRequest>
    }) => usersApi.update(id, data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['users'] })
      setFormOpen(false)
      setEditingUser(null)
      toast.success('User 更新成功')
    },
  })

  const deleteMutation = useMutation({
    mutationFn: (id: string) => usersApi.delete(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['users'] })
      setDeletingUser(null)
      toast.success('User 删除成功')
    },
  })

  const resetMutation = useMutation({
    mutationFn: (id: string) => usersApi.resetUsage(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['users'] })
      toast.success('User 用量已重置')
    },
  })

  const [form, setForm] = useState<FormState>({
    tenant_id: '',
    project_id: '',
    name: '',
    email: '',
    role: 'tenant_user',
    quota: -1,
    qps: 0,
    key_budget_usd: 0,
    models: [],
    status: 'active',
  })

  const openCreate = () => {
    setEditingUser(null)
    setForm({
      tenant_id: '',
      project_id: '',
      name: '',
      email: '',
      role: 'tenant_user',
      quota: -1,
      qps: 0,
      key_budget_usd: 0,
      models: [],
      status: 'active',
    })
    setFormOpen(true)
  }

  const openEdit = (user: User) => {
    setEditingUser(user)
    setForm({
      tenant_id: user.tenant_id,
      project_id: user.project_id || '',
      name: user.name,
      email: user.email || '',
      role: user.role,
      quota: user.quota,
      qps: user.qps || 0,
      key_budget_usd: user.key_budget_usd || 0,
      models: user.models || [],
      status: user.status,
    })
    setFormOpen(true)
  }

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault()
    if (editingUser) {
      updateMutation.mutate({
        id: editingUser.id,
        data: {
          role: form.role,
          quota: form.quota,
          qps: form.qps,
          project_id: form.project_id,
          key_budget_usd: form.key_budget_usd,
          models: form.models,
          status: form.status,
        },
      })
    } else {
      createMutation.mutate(form)
    }
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-semibold">User 管理</h1>
        <Button onClick={openCreate}>
          <Plus className="mr-2 h-4 w-4" />
          创建 User
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
                <TableHead>角色</TableHead>
                <TableHead>状态</TableHead>
                <TableHead>Quota</TableHead>
                <TableHead>Key Budget</TableHead>
                <TableHead className="text-right">操作</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {users?.length === 0 && (
                <TableRow>
                  <TableCell
                    colSpan={6}
                    className="text-muted-foreground text-center"
                  >
                    暂无 User
                  </TableCell>
                </TableRow>
              )}
              {users?.map((user) => (
                <TableRow key={user.id}>
                  <TableCell>
                    {user.name}
                    <div className="text-muted-foreground text-xs">
                      {user.email}
                    </div>
                  </TableCell>
                  <TableCell>{user.role}</TableCell>
                  <TableCell>
                    <Badge
                      variant={
                        user.status === 'active' ? 'default' : 'secondary'
                      }
                    >
                      {user.status}
                    </Badge>
                  </TableCell>
                  <TableCell>
                    {user.used} / {user.quota > 0 ? user.quota : '∞'}
                  </TableCell>
                  <TableCell>
                    ${user.key_spent_usd?.toFixed(2) || 0} / $
                    {user.key_budget_usd || 0}
                  </TableCell>
                  <TableCell className="text-right">
                    <div className="flex justify-end gap-2">
                      <Button
                        variant="ghost"
                        size="icon"
                        onClick={() => setUsageUser(user)}
                        title="用量"
                      >
                        <BarChart3 className="h-4 w-4" />
                      </Button>
                      <Button
                        variant="ghost"
                        size="icon"
                        onClick={() => resetMutation.mutate(user.id)}
                        disabled={resetMutation.isPending}
                        title="重置用量"
                      >
                        <RotateCcw className="h-4 w-4" />
                      </Button>
                      <Button
                        variant="ghost"
                        size="icon"
                        onClick={() => openEdit(user)}
                      >
                        <Pencil className="h-4 w-4" />
                      </Button>
                      <Button
                        variant="ghost"
                        size="icon"
                        onClick={() => setDeletingUser(user)}
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
            <DialogTitle>{editingUser ? '编辑 User' : '创建 User'}</DialogTitle>
          </DialogHeader>
          <form onSubmit={handleSubmit} className="space-y-4">
            <div className="grid gap-4 md:grid-cols-2">
              <div className="space-y-2">
                <Label htmlFor="name">名称 *</Label>
                <Input
                  id="name"
                  value={form.name}
                  onChange={(e) => setForm({ ...form, name: e.target.value })}
                  required
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="email">邮箱</Label>
                <Input
                  id="email"
                  value={form.email}
                  onChange={(e) => setForm({ ...form, email: e.target.value })}
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="role">角色</Label>
                <Input
                  id="role"
                  value={form.role}
                  onChange={(e) => setForm({ ...form, role: e.target.value })}
                  placeholder="tenant_user / tenant_admin"
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
              {!editingUser && (
                <div className="space-y-2">
                  <Label htmlFor="tenant_id">Tenant ID（超级管理员）</Label>
                  <Input
                    id="tenant_id"
                    value={form.tenant_id}
                    onChange={(e) =>
                      setForm({ ...form, tenant_id: e.target.value })
                    }
                  />
                </div>
              )}
              <div className="space-y-2">
                <Label htmlFor="quota">Quota（-1 表示无限制）</Label>
                <Input
                  id="quota"
                  type="number"
                  value={form.quota}
                  onChange={(e) =>
                    setForm({ ...form, quota: parseInt(e.target.value) || 0 })
                  }
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="qps">QPS</Label>
                <Input
                  id="qps"
                  type="number"
                  value={form.qps}
                  onChange={(e) =>
                    setForm({ ...form, qps: parseInt(e.target.value) || 0 })
                  }
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="key_budget_usd">Key Budget (USD)</Label>
                <Input
                  id="key_budget_usd"
                  type="number"
                  value={form.key_budget_usd}
                  onChange={(e) =>
                    setForm({
                      ...form,
                      key_budget_usd: parseFloat(e.target.value) || 0,
                    })
                  }
                />
              </div>
            </div>

            <div className="space-y-2">
              <Label htmlFor="models">允许模型（逗号分隔）</Label>
              <Input
                id="models"
                value={formatArray(form.models)}
                onChange={(e) =>
                  setForm({ ...form, models: parseArray(e.target.value) })
                }
              />
            </div>

            {editingUser && (
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
        open={!!deletingUser}
        onOpenChange={(open) => !open && setDeletingUser(null)}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>确认删除</DialogTitle>
            <DialogDescription>
              确定要删除 User「{deletingUser?.name}」吗？
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button
              variant="outline"
              onClick={() => setDeletingUser(null)}
              disabled={deleteMutation.isPending}
            >
              取消
            </Button>
            <Button
              variant="destructive"
              onClick={() =>
                deletingUser && deleteMutation.mutate(deletingUser.id)
              }
              disabled={deleteMutation.isPending}
            >
              {deleteMutation.isPending ? '删除中...' : '删除'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog
        open={!!usageUser}
        onOpenChange={(open) => !open && setUsageUser(null)}
      >
        <DialogContent className="max-w-2xl">
          <DialogHeader>
            <DialogTitle>User 用量：{usageUser?.name}</DialogTitle>
          </DialogHeader>
          <div className="space-y-4">
            <div className="grid gap-4 md:grid-cols-3">
              <div className="rounded-md border p-4">
                <div className="text-muted-foreground text-sm">已用 / 总额</div>
                <div className="text-xl font-semibold">
                  {usageData?.user?.used ?? 0} /{' '}
                  {(usageData?.user?.quota ?? 0) > 0
                    ? usageData?.user?.quota
                    : '∞'}
                </div>
              </div>
              <div className="rounded-md border p-4">
                <div className="text-muted-foreground text-sm">使用率</div>
                <div className="text-xl font-semibold">
                  {usageData?.user?.usage_percent?.toFixed(1) ?? 0}%
                </div>
              </div>
              <div className="rounded-md border p-4">
                <div className="text-muted-foreground text-sm">Key 花费</div>
                <div className="text-xl font-semibold">
                  ${usageData?.user?.key_spent_usd?.toFixed(2) ?? 0}
                </div>
              </div>
            </div>
            <pre className="bg-muted max-h-64 overflow-auto rounded-md p-4 text-xs">
              {JSON.stringify(usageData?.trend || [], null, 2)}
            </pre>
          </div>
        </DialogContent>
      </Dialog>

      <Dialog
        open={!!secretUser}
        onOpenChange={(open) => !open && setSecretUser(null)}
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
              <Input readOnly value={secretUser?.token || ''} />
            </div>
            <div className="space-y-1">
              <Label>Secret</Label>
              <Input readOnly value={secretUser?.api_secret || ''} />
            </div>
          </div>
          <DialogFooter>
            <Button onClick={() => setSecretUser(null)}>我已保存</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
