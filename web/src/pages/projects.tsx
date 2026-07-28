import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { Plus, Pencil, Trash2, BarChart3 } from 'lucide-react'
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
import { projectsApi } from '@/api/projects'
import type { Project, CreateProjectRequest } from '@/types/project'
import { toast } from 'sonner'

interface FormState extends CreateProjectRequest {
  status?: string
}

export function ProjectsPage() {
  const queryClient = useQueryClient()
  const [formOpen, setFormOpen] = useState(false)
  const [editingProject, setEditingProject] = useState<Project | null>(null)
  const [deletingProject, setDeletingProject] = useState<Project | null>(null)
  const [usageProject, setUsageProject] = useState<Project | null>(null)

  const { data: projects, isLoading } = useQuery({
    queryKey: ['projects'],
    queryFn: () => projectsApi.list(),
  })

  const { data: usageData } = useQuery({
    queryKey: ['project-usage', usageProject?.id],
    queryFn: () => projectsApi.usage(usageProject!.id, 7),
    enabled: !!usageProject,
  })

  const createMutation = useMutation({
    mutationFn: projectsApi.create,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['projects'] })
      setFormOpen(false)
      toast.success('Project 创建成功')
    },
  })

  const updateMutation = useMutation({
    mutationFn: ({
      id,
      data,
    }: {
      id: string
      data: Partial<CreateProjectRequest>
    }) => projectsApi.update(id, data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['projects'] })
      setFormOpen(false)
      setEditingProject(null)
      toast.success('Project 更新成功')
    },
  })

  const deleteMutation = useMutation({
    mutationFn: (id: string) => projectsApi.delete(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['projects'] })
      setDeletingProject(null)
      toast.success('Project 删除成功')
    },
  })

  const [form, setForm] = useState<FormState>({
    tenant_id: '',
    slug: '',
    name: '',
    budget_usd: 0,
    status: 'active',
  })

  const openCreate = () => {
    setEditingProject(null)
    setForm({
      tenant_id: '',
      slug: '',
      name: '',
      budget_usd: 0,
      status: 'active',
    })
    setFormOpen(true)
  }

  const openEdit = (project: Project) => {
    setEditingProject(project)
    setForm({
      tenant_id: project.tenant_id,
      slug: project.slug,
      name: project.name,
      budget_usd: project.budget_usd || 0,
      status: project.status,
    })
    setFormOpen(true)
  }

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault()
    if (editingProject) {
      updateMutation.mutate({
        id: editingProject.id,
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

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-semibold">Project 管理</h1>
        <Button onClick={openCreate}>
          <Plus className="mr-2 h-4 w-4" />
          创建 Project
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
              {projects?.length === 0 && (
                <TableRow>
                  <TableCell
                    colSpan={5}
                    className="text-muted-foreground text-center"
                  >
                    暂无 Project
                  </TableCell>
                </TableRow>
              )}
              {projects?.map((project) => (
                <TableRow key={project.id}>
                  <TableCell className="font-mono text-xs">
                    {project.slug}
                  </TableCell>
                  <TableCell>{project.name}</TableCell>
                  <TableCell>
                    <Badge
                      variant={
                        project.status === 'active' ? 'default' : 'secondary'
                      }
                    >
                      {project.status}
                    </Badge>
                  </TableCell>
                  <TableCell>
                    ${project.spent_usd?.toFixed(2) || 0} / $
                    {project.budget_usd || 0}
                  </TableCell>
                  <TableCell className="text-right">
                    <div className="flex justify-end gap-2">
                      <Button
                        variant="ghost"
                        size="icon"
                        onClick={() => setUsageProject(project)}
                        title="用量"
                      >
                        <BarChart3 className="h-4 w-4" />
                      </Button>
                      <Button
                        variant="ghost"
                        size="icon"
                        onClick={() => openEdit(project)}
                      >
                        <Pencil className="h-4 w-4" />
                      </Button>
                      <Button
                        variant="ghost"
                        size="icon"
                        onClick={() => setDeletingProject(project)}
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
              {editingProject ? '编辑 Project' : '创建 Project'}
            </DialogTitle>
          </DialogHeader>
          <form onSubmit={handleSubmit} className="space-y-4">
            <div className="grid gap-4 md:grid-cols-2">
              <div className="space-y-2">
                <Label htmlFor="slug">Slug *</Label>
                <Input
                  id="slug"
                  value={form.slug}
                  onChange={(e) => setForm({ ...form, slug: e.target.value })}
                  required
                  disabled={!!editingProject}
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="name">名称 *</Label>
                <Input
                  id="name"
                  value={form.name}
                  onChange={(e) => setForm({ ...form, name: e.target.value })}
                  required
                />
              </div>
              {!editingProject && (
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

            {editingProject && (
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
        open={!!deletingProject}
        onOpenChange={(open) => !open && setDeletingProject(null)}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>确认删除</DialogTitle>
            <DialogDescription>
              确定要删除 Project「{deletingProject?.name}」吗？
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button
              variant="outline"
              onClick={() => setDeletingProject(null)}
              disabled={deleteMutation.isPending}
            >
              取消
            </Button>
            <Button
              variant="destructive"
              onClick={() =>
                deletingProject && deleteMutation.mutate(deletingProject.id)
              }
              disabled={deleteMutation.isPending}
            >
              {deleteMutation.isPending ? '删除中...' : '删除'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog
        open={!!usageProject}
        onOpenChange={(open) => !open && setUsageProject(null)}
      >
        <DialogContent className="max-w-2xl">
          <DialogHeader>
            <DialogTitle>Project 用量：{usageProject?.name}</DialogTitle>
          </DialogHeader>
          <div className="space-y-4">
            <div className="grid gap-4 md:grid-cols-3">
              <div className="rounded-md border p-4">
                <div className="text-muted-foreground text-sm">总请求数</div>
                <div className="text-xl font-semibold">
                  {usageData?.summary?.total_requests ?? 0}
                </div>
              </div>
              <div className="rounded-md border p-4">
                <div className="text-muted-foreground text-sm">总 Token</div>
                <div className="text-xl font-semibold">
                  {usageData?.summary?.total_tokens ?? 0}
                </div>
              </div>
              <div className="rounded-md border p-4">
                <div className="text-muted-foreground text-sm">总成本</div>
                <div className="text-xl font-semibold">
                  ${usageData?.summary?.total_cost_usd?.toFixed(4) ?? 0}
                </div>
              </div>
            </div>
            <pre className="bg-muted max-h-64 overflow-auto rounded-md p-4 text-xs">
              {JSON.stringify(usageData?.trend || [], null, 2)}
            </pre>
          </div>
        </DialogContent>
      </Dialog>
    </div>
  )
}
