import type { Provider } from './provider'
import type { Service } from './service'

export interface CatalogCounts {
  providers: number
  services: number
}

export interface CatalogView {
  tenant_id: string
  counts: CatalogCounts
  providers: Provider[]
  services: Service[]
}
