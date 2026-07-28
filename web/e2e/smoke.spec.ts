import { test, expect, Page, APIRequestContext } from '@playwright/test'

const ADMIN_TOKEN = 'admin-key-001:local-admin-secret'
const ADMIN_AUTH = { Authorization: `Bearer ${ADMIN_TOKEN}` }

async function login(page: Page) {
  await page.goto('/login')
  await page.getByPlaceholder('admin-key-001:your-secret').fill(ADMIN_TOKEN)
  await page.getByRole('button', { name: '登录' }).click()
  await expect(page.getByRole('heading', { name: 'Dashboard' })).toBeVisible({ timeout: 10000 })
}

interface ServiceSummary {
  id: string
  name: string
  request_prefix: string
}

async function cleanupE2EServices(request: APIRequestContext, prefix: string) {
  const res = await request.get('http://localhost:8028/admin/v1/services', {
    headers: ADMIN_AUTH,
  })
  if (!res.ok()) return
  const body = await res.json().catch(() => ({ success: false, data: [] }))
  const services: ServiceSummary[] = body.data || []
  for (const service of services) {
    if (service.name === prefix || service.request_prefix === prefix) {
      await request.delete(`http://localhost:8028/admin/v1/services/${service.id}`, {
        headers: ADMIN_AUTH,
      })
    }
  }
}

test.describe('Gateyes Admin Frontend', () => {
  test('login page renders', async ({ page }) => {
    await page.goto('/login')
    await expect(page.getByText('Gateyes 控制台')).toBeVisible()
    await expect(page.getByPlaceholder('admin-key-001:your-secret')).toBeVisible()
  })

  test('dashboard loads after login', async ({ page }) => {
    await login(page)
    await expect(page.getByRole('heading', { name: 'Dashboard' })).toBeVisible()
    await expect(page.getByText('Gateyes 控制台')).toBeVisible()
  })

  test('navigate through all main pages', async ({ page }) => {
    await login(page)

    const pages = [
      { name: 'Provider', heading: 'Provider 管理' },
      { name: 'API Key', heading: 'API Key 管理' },
      { name: 'Virtual Key', heading: 'Virtual Key 管理' },
      { name: 'Project', heading: 'Project 管理' },
      { name: 'User', heading: 'User 管理' },
      { name: 'Tenant', heading: 'Tenant 管理' },
      { name: 'Service', heading: 'Service 管理' },
      { name: 'Plugin', heading: 'Plugin 市场' },
      { name: 'Response', heading: '响应记录' },
      { name: 'Audit', heading: '审计日志' },
      { name: 'Settings', heading: '系统设置' },
    ]

    for (const nav of pages) {
      await page.getByRole('link', { name: nav.name }).click()
      await expect(page.getByText(nav.heading).first()).toBeVisible({ timeout: 5000 })
    }
  })

  test('provider page shows create button and table headers', async ({ page }) => {
    await login(page)
    await page.getByRole('link', { name: 'Provider' }).click()
    await expect(page.getByRole('button', { name: '创建 Provider' })).toBeVisible()
    await expect(page.getByText('健康检查')).toBeVisible()
    await expect(page.getByText('名称')).toBeVisible()
    await expect(page.getByText('模型')).toBeVisible()
  })

  test('open provider create dialog', async ({ page }) => {
    await login(page)
    await page.getByRole('link', { name: 'Provider' }).click()
    await page.getByRole('button', { name: '创建 Provider' }).click()
    await expect(page.getByRole('dialog', { name: '创建 Provider' })).toBeVisible()
    await expect(page.getByLabel('名称 *')).toBeVisible()
    await expect(page.getByLabel('模型 *')).toBeVisible()
  })

  test('service page shows config tabs and can create service with config', async ({ page, request }) => {
    const runId = Date.now().toString(36)
    const serviceName = `e2e-test-service-${runId}`
    const servicePrefix = `e2e-test-prefix-${runId}`

    // Ensure idempotency: remove any leftover service from a previous interrupted run.
    await cleanupE2EServices(request, serviceName)

    await login(page)
    await page.getByRole('link', { name: 'Service' }).click()
    await expect(page.getByRole('button', { name: '创建 Service' })).toBeVisible()

    await page.getByRole('button', { name: '创建 Service' }).click()
    await expect(page.getByRole('dialog', { name: '创建 Service' })).toBeVisible()

    await page.getByLabel('名称 *').fill(serviceName)
    await page.getByLabel('Request Prefix *').fill(servicePrefix)
    await page.getByLabel('Tenant ID（超级管理员）').fill('default')

    await page.getByRole('button', { name: 'Surfaces' }).click()
    await expect(page.getByText('responses')).toBeVisible()
    await page.getByRole('checkbox', { name: 'responses' }).check()
    await page.getByRole('checkbox', { name: 'chat' }).check()

    await page.getByRole('button', { name: 'Prompt Template' }).click()
    await page.getByLabel('System Template').fill('You are a helpful assistant.')
    await page.getByLabel('User Template').fill('Say hello to {{name}}')

    await page.getByRole('button', { name: 'Policy' }).click()
    await page.getByRole('switch', { name: '启用 Policy' }).check()

    await page.getByRole('button', { name: '保存' }).click()

    await expect(page.getByText(serviceName)).toBeVisible()
    await expect(page.getByText('draft').first()).toBeVisible()

    // Edit and verify config is persisted
    const row = page.locator('table tbody tr', { hasText: serviceName })
    await row.getByRole('button', { name: '编辑' }).click()
    await expect(page.getByRole('dialog', { name: '编辑 Service' })).toBeVisible()

    await page.getByRole('button', { name: 'Surfaces' }).click()
    await expect(page.getByRole('checkbox', { name: 'responses' })).toBeChecked()
    await expect(page.getByRole('checkbox', { name: 'chat' })).toBeChecked()

    await page.getByRole('button', { name: 'Prompt Template' }).click()
    await expect(page.getByLabel('System Template')).toHaveValue(
      'You are a helpful assistant.'
    )

    // Cancel edit
    await page.getByRole('button', { name: '取消' }).click()
    await expect(page.getByRole('dialog', { name: '编辑 Service' })).not.toBeVisible()

    // Delete the test service
    await row.getByRole('button', { name: '删除' }).click()
    await page.getByRole('button', { name: '删除', exact: true }).click()
    await expect(page.getByRole('dialog', { name: '确认删除' })).not.toBeVisible()
    await expect(row).not.toBeVisible()
  })

  test('plugin page renders and can register gRPC plugin', async ({ page }) => {
    const runId = Date.now().toString(36)
    const pluginName = `e2e-grpc-${runId}`

    await login(page)
    await page.getByRole('link', { name: 'Plugin' }).click()
    await expect(page.getByRole('heading', { name: 'Plugin 市场' })).toBeVisible()

    // Register gRPC plugin via form
    await page.getByRole('button', { name: '注册 gRPC' }).click()
    await expect(page.getByText('注册 gRPC 插件')).toBeVisible()

    await page.getByLabel('名称 *').fill(pluginName)
    await page.getByLabel('gRPC 地址 *').fill('localhost:50052')

    // Select phases via badges in the form (scope to '注册 gRPC 插件' section)
    const formSection = page.locator('h3:has-text("注册 gRPC 插件")').locator('..')
    await formSection.getByText('post_upstream').click()
    await formSection.getByText('audit').click()

    await page.getByRole('button', { name: '注册', exact: true }).click()

    // Switch to installed tab to see the new plugin
    await page.getByRole('button', { name: '已安装' }).click()
    await expect(page.getByText(pluginName)).toBeVisible({ timeout: 10000 })

    // Toggle enable
    const row = page.locator('table tbody tr', { hasText: pluginName })
    await row.getByRole('switch').click()

    // Delete
    await row.getByRole('button', { name: '删除' }).click()
    await page.getByRole('button', { name: '删除', exact: true }).click()
    await expect(page.getByRole('dialog', { name: '确认删除' })).not.toBeVisible()
    await expect(row).not.toBeVisible({ timeout: 5000 })
  })
})
