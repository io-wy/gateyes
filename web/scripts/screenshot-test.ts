import { chromium } from '@playwright/test'

const ADMIN_TOKEN = 'admin-key-001:local-admin-secret'

async function main() {
  const browser = await chromium.launch()
  const context = await browser.newContext()
  const page = await context.newPage()
  await page.setViewportSize({ width: 1280, height: 720 })

  // 1. 登录页
  await page.goto('http://localhost:5173/login')
  await page.waitForTimeout(500)
  await page.screenshot({ path: 'tmp/e2e/login.png', fullPage: false })

  // 2. 登录
  await page.getByPlaceholder('admin-key-001:your-secret').fill(ADMIN_TOKEN)
  await page.getByRole('button', { name: '登录' }).click()
  await page.waitForSelector('h1:has-text("Dashboard")', { timeout: 10000 })
  await page.waitForTimeout(500)
  await page.screenshot({ path: 'tmp/e2e/dashboard.png', fullPage: false })

  // 3. Provider 页
  await page.getByRole('link', { name: 'Provider' }).click()
  await page.waitForSelector('text=Provider 管理', { timeout: 5000 })
  await page.waitForTimeout(500)
  await page.screenshot({ path: 'tmp/e2e/providers.png', fullPage: false })

  // 4. API Key 页
  await page.getByRole('link', { name: 'API Key' }).click()
  await page.waitForSelector('text=API Key 管理', { timeout: 5000 })
  await page.waitForTimeout(500)
  await page.screenshot({ path: 'tmp/e2e/keys.png', fullPage: false })

  // 5. Response 页
  await page.getByRole('link', { name: 'Response' }).click()
  await page.waitForSelector('text=响应记录', { timeout: 5000 })
  await page.waitForTimeout(500)
  await page.screenshot({ path: 'tmp/e2e/responses.png', fullPage: false })

  // 输出 console 报错
  await browser.close()
  console.log('screenshots saved to tmp/e2e/')
}

main().catch((err) => {
  console.error(err)
  process.exit(1)
})
