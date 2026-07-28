import client from './client'

export const settingsApi = {
  reloadConfig: () => client.post<{ reloaded: boolean }>('/reload'),
}
