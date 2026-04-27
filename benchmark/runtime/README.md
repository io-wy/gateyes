# Gateyes 本地压测手册

## 一、环境准备（每次重启后）

### 1. 确保端口空闲
```bash
netstat -ano | grep ':8083' | grep LISTEN
```
如果有输出，杀掉占用进程：
```bash
taskkill //PID <PID> //F
```

### 2. 清理旧数据（关键！）
**必须删掉旧数据库**，否则配置修改不会生效：
```bash
rm -f gateyes_bench2.db gateway.log gateway.pid
```
> 原因：gateway 启动后会将 provider 配置写入数据库的 `provider_registry` 表。如果之前启动过且 BaseURL 为空，旧记录会覆盖新配置。删库是唯一确保重新加载的方式。

---

## 二、启动 Gateway

在 `benchmark/deploy` 目录下执行：

```bash
cd /c/code/gateyes/benchmark/deploy
./gateway.exe -config bench.yaml
```

或者后台运行（Git Bash）：
```bash
cd /c/code/gateyes/benchmark/deploy
./gateway.exe -config bench.yaml > gateway.log 2>&1 &
echo $! > gateway.pid
```

### 验证启动成功
```bash
curl -s http://localhost:8083/health
```

发一个 chat 请求验证 provider 连通：
```bash
curl -s -X POST http://localhost:8083/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer bench-key-001:bench-secret-001" \
  -d '{"model":"glm-5.1","messages":[{"role":"user","content":"hi"}],"max_tokens":10}'
```

---

## 三、运行压测

### 单场景压测（loadtest）
```bash
./loadtest.exe -url http://localhost:8083/v1/chat/completions \
  -key bench-key-001 -secret bench-secret-001 \
  -d 30s -warmup 3s
```

参数说明：
- `-d`：每个并发级别持续多久（默认 30s）
- `-warmup`：预热时间（默认 3s）
- `-body`：自定义请求体（默认模型是 `glm-5.1`）

### 多场景压测（multiscenario）
```bash
./multiscenario.exe -url http://localhost:8083 \
  -key bench-key-001 -secret bench-secret-001 \
  -d 30s
```

### k6 压测（如已安装 k6）
```bash
k6 run --env API_KEY=bench-key-001 --env API_SECRET=bench-secret-001 k6.js
```

---

## 四、检查日志

```bash
tail -f gateway.log
```

关键错误模式：
- `unsupported protocol scheme ""` → BaseURL 为空，env 文件没加载上
- `401 authentication_error` → API Key 被上游拒绝（provider 配置问题）
- `all retries exhausted` → 所有 provider 都失败了

---

## 五、修改 provider 配置后必做

如果改了 `bench.yaml` 或 `*.env` 文件，**务必先删库再重启**：
```bash
taskkill //IM gateway.exe //F
rm -f gateyes_bench2.db gateway.log
cd /c/code/gateyes/benchmark/deploy
./gateway.exe -config bench.yaml
```

---

## 六、当前目录文件说明

| 文件 | 用途 |
|------|------|
| `gateway.exe` | Gateway 服务 |
| `bench.yaml` | Gateway 配置文件 |
| `1.env` / `2.env` / `3.env` | Provider 密钥配置 |
| `loadtest.exe` | 单场景阶梯压测 |
| `multiscenario.exe` | 多场景混合压测 |
| `k6.js` | k6 压测脚本 |
| `gateyes_bench2.db` | SQLite 数据库（运行时生成） |
| `gateway.log` | 运行日志（运行时生成） |
| `gateway.pid` | 进程 PID（运行时生成） |
