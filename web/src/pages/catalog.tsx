import { useQuery } from '@tanstack/react-query'
import { Box, CheckCircle2, Store } from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { catalogApi } from '@/api/catalog'

export function CatalogPage() {
  const { data: catalog, isLoading } = useQuery({
    queryKey: ['service-catalog'],
    queryFn: () => catalogApi.get({ publish_status: 'published' }),
  })

  const services =
    catalog?.services.filter(
      (service) => service.enabled && service.publish_status === 'published'
    ) ?? []
  const total = services?.length ?? 0

  return (
    <div className="space-y-6">
      <div className="flex flex-col gap-3 sm:flex-row sm:items-end sm:justify-between">
        <div className="flex items-center gap-3">
          <div className="bg-primary text-primary-foreground flex h-10 w-10 items-center justify-center rounded-lg">
            <Store className="h-5 w-5" />
          </div>
          <div>
            <h1 className="text-2xl font-semibold">服务目录</h1>
            <p className="text-muted-foreground mt-1 text-sm">
              当前租户已发布、可调用的 MaaS 服务。
            </p>
          </div>
        </div>
        <Badge variant="outline" className="w-fit">
          {isLoading
            ? '加载中'
            : `${total} 个服务 / ${catalog?.counts.providers ?? 0} Provider`}
        </Badge>
      </div>

      {isLoading ? (
        <div className="text-muted-foreground rounded-xl border border-dashed p-8 text-center text-sm">
          加载中...
        </div>
      ) : total === 0 ? (
        <div className="text-muted-foreground rounded-xl border border-dashed p-8 text-center text-sm">
          暂无已发布服务
        </div>
      ) : (
        <div className="grid gap-4 lg:grid-cols-2 2xl:grid-cols-3">
          {services?.map((service) => {
            const surfaces = service.config?.surfaces ?? ['responses']
            return (
              <article
                key={service.id}
                className="bg-card hover:border-foreground/20 flex min-h-56 flex-col justify-between rounded-xl border p-4 shadow-sm transition-colors"
              >
                <div className="min-w-0">
                  <div className="flex items-start justify-between gap-3">
                    <div className="min-w-0">
                      <div className="flex items-center gap-2">
                        <Box className="text-muted-foreground h-4 w-4 shrink-0" />
                        <h2 className="truncate text-base font-semibold">
                          {service.name}
                        </h2>
                      </div>
                      {service.description && (
                        <p className="text-muted-foreground mt-2 line-clamp-2 text-sm">
                          {service.description}
                        </p>
                      )}
                    </div>
                    <Badge className="shrink-0">
                      <CheckCircle2 className="h-3 w-3" />
                      已发布
                    </Badge>
                  </div>

                  <div className="mt-4 grid gap-3 sm:grid-cols-2">
                    <div className="bg-muted/50 rounded-lg p-3">
                      <div className="text-muted-foreground text-xs">
                        Prefix
                      </div>
                      <div className="mt-1 truncate font-mono text-sm">
                        /service/{service.request_prefix}
                      </div>
                    </div>
                    <div className="bg-muted/50 rounded-lg p-3">
                      <div className="text-muted-foreground text-xs">
                        默认模型
                      </div>
                      <div className="mt-1 truncate text-sm">
                        {service.default_model || '-'}
                      </div>
                    </div>
                  </div>
                </div>

                <div className="mt-4 flex flex-wrap gap-1">
                  {surfaces.map((surface) => (
                    <Badge key={surface} variant="outline">
                      {surface}
                    </Badge>
                  ))}
                </div>
              </article>
            )
          })}
        </div>
      )}
    </div>
  )
}
