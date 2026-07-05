import { useState, useRef } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { Plus, Trash2, Upload, PlugZap, FileType } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
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
import { pluginsApi } from '@/api/plugins'
import type { Plugin, CreatePluginRequest } from '@/types/plugin'
import { toast } from 'sonner'

const TABS = [
  { id: 'installed', label: '已安装' },
  { id: 'register', label: '注册插件' },
] as const

const PHASES = ['pre_route', 'post_route', 'pre_upstream', 'post_upstream', 'audit'] as const
const ALL_PHASES = [...PHASES] as string[]

function initGRPCForm(): Partial<CreatePluginRequest> {
  return {
    name: '',
    type: 'grpc',
    address: '',
    description: '',
    author: '',
    phases: ['post_upstream'],
  }
}

function phaseBadgeVariant(phase: string): 'default' | 'secondary' | 'outline' {
  if (phase === 'pre_upstream' || phase === 'pre_route') return 'default'
  if (phase === 'audit') return 'secondary'
  return 'outline'
}

export function PluginsPage() {
  const queryClient = useQueryClient()
  const [activeTab, setActiveTab] = useState<string>('installed')
  const [gRPCForm, setGRPCForm] = useState(initGRPCForm())
  const [deletingPlugin, setDeletingPlugin] = useState<Plugin | null>(null)
  const fileInputRef = useRef<HTMLInputElement>(null)

  const { data: plugins, isLoading } = useQuery({
    queryKey: ['plugins'],
    queryFn: () => pluginsApi.list(),
  })

  const createMutation = useMutation({
    mutationFn: pluginsApi.create,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['plugins'] })
      setGRPCForm(initGRPCForm())
      toast.success('插件注册成功')
    },
  })

  const uploadMutation = useMutation({
    mutationFn: pluginsApi.upload,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['plugins'] })
      toast.success('插件上传成功')
    },
  })

  const updateMutation = useMutation({
    mutationFn: ({ id, data }: { id: string; data: { enabled: boolean } }) =>
      pluginsApi.update(id, data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['plugins'] })
      toast.success('插件更新成功')
    },
  })

  const deleteMutation = useMutation({
    mutationFn: (id: string) => pluginsApi.delete(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['plugins'] })
      setDeletingPlugin(null)
      toast.success('插件删除成功')
    },
  })

  const handleToggle = (plugin: Plugin) => {
    updateMutation.mutate({
      id: plugin.id,
      data: { enabled: !plugin.enabled },
    })
  }

  const handleRegister = (e: React.FormEvent) => {
    e.preventDefault()
    createMutation.mutate(gRPCForm as CreatePluginRequest)
  }

  const handleUpload = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0]
    if (!file) return
    const formData = new FormData()
    formData.append('file', file)
    formData.append('name', file.name.replace(/\.wasm$/i, ''))
    formData.append('phases', 'post_upstream')
    uploadMutation.mutate(formData)
    e.target.value = ''
  }

  const toggleGRPCPhase = (phase: string) => {
    setGRPCForm((prev) => {
      const current = prev.phases || []
      const next = current.includes(phase)
        ? current.filter((p) => p !== phase)
        : [...current, phase]
      return { ...prev, phases: next }
    })
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-semibold">Plugin 市场</h1>
        <div className="flex gap-2">
          <input
            ref={fileInputRef}
            type="file"
            accept=".wasm"
            className="hidden"
            onChange={handleUpload}
            disabled={uploadMutation.isPending}
          />
          <Button
            variant="outline"
            onClick={() => fileInputRef.current?.click()}
            disabled={uploadMutation.isPending}
          >
            <Upload className="mr-2 h-4 w-4" />
            上传 WASM
          </Button>
          <Button onClick={() => setActiveTab('register')}>
            <Plus className="mr-2 h-4 w-4" />
            注册 gRPC
          </Button>
        </div>
      </div>

      <div className="flex gap-1 border-b">
        {TABS.map((tab) => (
          <button
            key={tab.id}
            type="button"
            className={`px-3 py-2 text-sm transition-colors ${
              activeTab === tab.id
                ? 'border-b-2 border-primary font-medium'
                : 'text-muted-foreground hover:text-foreground'
            }`}
            onClick={() => setActiveTab(tab.id)}
          >
            {tab.label}
          </button>
        ))}
      </div>

      {activeTab === 'installed' && (
        <div className="rounded-md border">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>名称</TableHead>
                <TableHead>类型</TableHead>
                <TableHead>Phases</TableHead>
                <TableHead>来源</TableHead>
                <TableHead>状态</TableHead>
                <TableHead className="text-right">操作</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {isLoading ? (
                <TableRow>
                  <TableCell colSpan={6} className="text-muted-foreground text-center">
                    加载中...
                  </TableCell>
                </TableRow>
              ) : plugins?.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={6} className="text-muted-foreground text-center">
                    暂无插件，上传一个 WASM 或注册一个 gRPC 吧
                  </TableCell>
                </TableRow>
              ) : (
                plugins?.map((plugin) => (
                  <TableRow key={plugin.id}>
                    <TableCell className="font-medium">{plugin.name}</TableCell>
                    <TableCell>
                      <Badge variant={plugin.type === 'wasm' ? 'default' : 'secondary'}>
                        {plugin.type === 'wasm' ? (
                          <FileType className="mr-1 h-3 w-3" />
                        ) : (
                          <PlugZap className="mr-1 h-3 w-3" />
                        )}
                        {plugin.type}
                      </Badge>
                    </TableCell>
                    <TableCell>
                      <div className="flex flex-wrap gap-1">
                        {plugin.phases?.map((phase) => (
                          <Badge key={phase} variant={phaseBadgeVariant(phase)} className="text-xs">
                            {phase}
                          </Badge>
                        ))}
                      </div>
                    </TableCell>
                    <TableCell>
                      <span className="text-muted-foreground text-xs capitalize">
                        {plugin.source}
                      </span>
                    </TableCell>
                    <TableCell>
                      <Switch
                        checked={plugin.enabled}
                        onCheckedChange={() => handleToggle(plugin)}
                      />
                    </TableCell>
                    <TableCell className="text-right">
                      <div className="flex justify-end gap-2">
                        <Button
                          variant="ghost"
                          size="icon"
                          onClick={() => setDeletingPlugin(plugin)}
                          aria-label="删除"
                        >
                          <Trash2 className="text-destructive h-4 w-4" />
                        </Button>
                      </div>
                    </TableCell>
                  </TableRow>
                ))
              )}
            </TableBody>
          </Table>
        </div>
      )}

      {activeTab === 'register' && (
        <div className="max-w-xl rounded-md border p-6">
          <h3 className="mb-4 text-lg font-medium">注册 gRPC 插件</h3>
          <form onSubmit={handleRegister} className="space-y-4">
            <div className="space-y-2">
              <Label htmlFor="grpc-name">名称 *</Label>
              <Input
                id="grpc-name"
                value={gRPCForm.name || ''}
                onChange={(e) =>
                  setGRPCForm((prev) => ({ ...prev, name: e.target.value }))
                }
                required
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="grpc-address">gRPC 地址 *</Label>
              <Input
                id="grpc-address"
                value={gRPCForm.address || ''}
                onChange={(e) =>
                  setGRPCForm((prev) => ({ ...prev, address: e.target.value }))
                }
                placeholder="localhost:50052"
                required
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="grpc-desc">描述</Label>
              <Input
                id="grpc-desc"
                value={gRPCForm.description || ''}
                onChange={(e) =>
                  setGRPCForm((prev) => ({ ...prev, description: e.target.value }))
                }
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="grpc-author">作者</Label>
              <Input
                id="grpc-author"
                value={gRPCForm.author || ''}
                onChange={(e) =>
                  setGRPCForm((prev) => ({ ...prev, author: e.target.value }))
                }
              />
            </div>
            <div className="space-y-2">
              <Label>Phases</Label>
              <div className="flex flex-wrap gap-2">
                {ALL_PHASES.map((phase) => (
                  <Badge
                    key={phase}
                    variant={
                      (gRPCForm.phases || []).includes(phase)
                        ? 'default'
                        : 'outline'
                    }
                    className="cursor-pointer"
                    onClick={() => toggleGRPCPhase(phase)}
                  >
                    {phase}
                  </Badge>
                ))}
              </div>
            </div>
            <Button type="submit" disabled={createMutation.isPending}>
              {createMutation.isPending ? '注册中...' : '注册'}
            </Button>
          </form>
        </div>
      )}

      <Dialog
        open={!!deletingPlugin}
        onOpenChange={(open) => !open && setDeletingPlugin(null)}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>确认删除</DialogTitle>
            <DialogDescription>
              确定要删除插件「{deletingPlugin?.name}」吗？
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button
              variant="outline"
              onClick={() => setDeletingPlugin(null)}
              disabled={deleteMutation.isPending}
            >
              取消
            </Button>
            <Button
              variant="destructive"
              onClick={() =>
                deletingPlugin && deleteMutation.mutate(deletingPlugin.id)
              }
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
