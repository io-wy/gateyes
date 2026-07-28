export interface ApiResponse<T> {
  code: number
  success: boolean
  message: string
  data: T
}

export interface PaginatedList<T> {
  data: T[]
  total: number
}

export interface ListData<T> {
  Items: T[]
  Total: number
}

export interface ListParams {
  limit?: number
  offset?: number
}
