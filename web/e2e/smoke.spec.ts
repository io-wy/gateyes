import { test, expect, Page, APIRequestContext } from '@playwright/test'

const ADMIN_KEY = process.env.GATEYES_ADMIN_BOOTSTRAP_KEY || 'admin-key-001'
const ADMIN_SECRET =
  process.env.GATEYES_ADMIN_BOOTSTRAP_SECRET || 'local-admin-secret'
const ADMIN_TOKEN = `${ADMIN_KEY}:${ADMIN_SECRET}`
const ADMIN_AUTH = { Authorization: `Bearer ${ADMIN_TOKEN}` }

async function login(page: Page) {
  await page.goto('/login')
  await page.getByPlaceholder('admin-key-001:your-secret').fill(ADMIN_TOKEN)
  await page.getByRole('button', { name: '登录' }).click()
  await expect(page.getByRole('heading', { name: 'Dashboard' })).toBeVisible({ timeout: 10000 })
}

async function loginWithToken(page: Page, token: string) {
  await page.goto('/login')
  await page.getByPlaceholder('admin-key-001:your-secret').fill(token)
  await page.getByRole('button', { name: '登录' }).click()
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

async function cleanupE2EProvider(request: APIRequestContext, name: string) {
  await request.delete(`http://localhost:8028/admin/v1/providers/${name}`, {
    headers: ADMIN_AUTH,
  })
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

  test('tenant user sees MaaS console navigation only', async ({ page, request }) => {
    const runId = Date.now().toString(36)
    const userID = `e2e-user-${runId}`

    const userResp = await request.post('http://localhost:8028/admin/v1/users', {
      headers: ADMIN_AUTH,
      data: {
        tenant_id: 'default',
        name: userID,
        email: `${userID}@example.com`,
        role: 'tenant_user',
      },
    })
    const userBody = await userResp.json()
    const createdUserID = (userBody.data?.id || userBody.id) as string
    const userToken = (userBody.data?.token || userBody.token) as string

    await loginWithToken(page, userToken)
    await expect(page).toHaveURL(/\/playground/)
    await expect(page.getByRole('link', { name: 'Playground' })).toBeVisible()
    await expect(page.getByRole('link', { name: 'API Key' })).toBeVisible()
    await expect(page.getByRole('link', { name: 'Virtual Key' })).toBeVisible()
    await expect(page.getByRole('link', { name: '服务目录' })).toBeVisible()
    await expect(page.getByRole('link', { name: '调用记录' })).toBeVisible()
    await expect(page.getByRole('link', { name: 'Provider' })).toHaveCount(0)
    await expect(page.getByRole('link', { name: 'User' })).toHaveCount(0)
    await expect(page.getByRole('link', { name: 'Tenant' })).toHaveCount(0)

    await request.delete(`http://localhost:8028/admin/v1/users/${createdUserID}`, {
      headers: ADMIN_AUTH,
    })
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

  test('playground lets users choose a model from providers', async ({ page }) => {
    await login(page)
    await page.getByRole('link', { name: 'Playground' }).click()
    await expect(page.getByRole('heading', { name: 'API Test Playground' })).toBeVisible()

    const modelSelect = page.getByLabel('选择模型')
    await expect(modelSelect).toBeVisible()
    await modelSelect.click()

    const firstOption = page.getByRole('option').first()
    await expect(firstOption).toBeVisible()
    const selectedText =
      (await firstOption.locator('.font-medium').first().textContent())?.trim() || ''
    await firstOption.click()

    if (selectedText) {
      await expect(modelSelect).toContainText(selectedText)
    }
  })

  test('playground renders stream text from SSE events', async ({ page, request }) => {
    const runId = Date.now().toString(36)
    const serviceName = `e2e-playground-stream-${runId}`
    const servicePrefix = `e2e-playground-stream-${runId}`

    await cleanupE2EServices(request, serviceName)
    await request.post('http://localhost:8028/admin/v1/services', {
      headers: ADMIN_AUTH,
      data: {
        tenant_id: 'default',
        name: serviceName,
        request_prefix: servicePrefix,
        default_provider: 'deepseek',
        default_model: 'deepseek-v4-flash',
        enabled: true,
        auto_publish: true,
        config: { surfaces: ['responses'] },
      },
    })

    await page.route(`**/service/${servicePrefix}/responses`, async (route) => {
      await route.fulfill({
        status: 200,
        headers: { 'content-type': 'text/event-stream' },
        body:
          'event: response.output_text.delta\r\n' +
          'data: {"type":"response.output_text.delta","delta":"Hello"}\r\n\r\n' +
          'event: response.output_text.delta\r\n' +
          'data: {"type":"response.output_text.delta","delta":{"text":" world"}}\r\n\r\n' +
          'data: [DONE]\r\n\r\n',
      })
    })

    await login(page)
    await page.getByRole('link', { name: 'Playground' }).click()
    await page.getByLabel('选择 service').click()
    await page.getByRole('option', { name: new RegExp(serviceName) }).click()
    await page.getByRole('button', { name: '运行' }).click()

    await expect(page.getByText('Hello world')).toBeVisible()

    await cleanupE2EServices(request, serviceName)
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

  test('can create provider from provider page', async ({ page, request }) => {
    const runId = Date.now().toString(36)
    const providerName = `e2e-provider-${runId}`

    await cleanupE2EProvider(request, providerName)

    await login(page)
    await page.getByRole('link', { name: 'Provider' }).click()
    await page.getByRole('button', { name: '创建 Provider' }).click()
    await expect(page.getByRole('dialog', { name: '创建 Provider' })).toBeVisible()

    await page.getByLabel('名称 *').fill(providerName)
    await page.getByLabel('模型 *').fill('e2e-model')
    await page.getByLabel('类型').fill('openai')
    await page.getByLabel('厂商').fill('openai')
    await page.getByLabel('Base URL').fill('http://127.0.0.1:18080/v1')
    await page.getByLabel('Endpoint').fill('chat')
    await page.getByLabel('API Key').fill('e2e-key')
    await page.getByRole('checkbox', { name: 'Chat Completions' }).check()
    await page.getByRole('checkbox', { name: 'Responses API' }).check()
    await page.getByRole('checkbox', { name: 'Streaming' }).check()

    await page.getByRole('button', { name: '保存' }).click()

    await expect(page.getByRole('dialog', { name: '创建 Provider' })).not.toBeVisible({
      timeout: 10000,
    })
    await expect(page.locator('table tbody tr', { hasText: providerName })).toBeVisible()

    await cleanupE2EProvider(request, providerName)
    await page.reload()
    await expect(page.locator('table tbody tr', { hasText: providerName })).not.toBeVisible()
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
    await expect(page.locator('table tbody tr', { hasText: serviceName })).toContainText(
      '未发布'
    )

    const row = page.locator('table tbody tr', { hasText: serviceName })

    // Create a new version and publish it from the version dialog.
    await row.getByRole('button', { name: '版本' }).click()
    await expect(page.getByText(`Service 版本：${serviceName}`)).toBeVisible()
    await expect(page.locator('[role="dialog"] table tbody tr', { hasText: 'v1' })).toContainText(
      'draft'
    )

    await page.getByRole('button', { name: '创建新版本' }).click()
    const versionTwoRow = page.locator('[role="dialog"] table tbody tr', {
      hasText: 'v2',
    })
    await expect(versionTwoRow).toContainText('draft')
    await versionTwoRow.getByRole('button', { name: '发布' }).click()
    await expect(versionTwoRow).toContainText('published')

    await page.keyboard.press('Escape')
    await expect(page.getByText(`Service 版本：${serviceName}`)).not.toBeVisible()
    await expect(row).toContainText('已发布')

    // Edit and verify config is persisted
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
