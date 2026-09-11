import client from './client'
import type { CatalogView } from '@/types/catalog'

export const catalogApi = {
  get: (params?: { project_id?: string; publish_status?: string }) =>
    client.get<CatalogView>('/catalog', { params }),
}
