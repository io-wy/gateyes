# Ingress Controller

Gateyes 同时是一个 **K8s Ingress Controller** 和 **微服务网关**。它 watch K8s `networking.k8s.io/v1 Ingress` 资源，将 Host+Path 规则动态加载到内存路由表，通过 `httputil.ReverseProxy` 转发到后端服务。

核心设计目标：**与 Nginx Ingress Controller 注解兼容**，平滑迁移已有 Ingress 配置。

---

## 1. 架构

```
Request ──► [Gin IngressMiddleware] ──命中?──► ReverseProxy ──► Backend
                        │
                        未命中
                        ▼
                 [Auth/Guard/Metrics]
                        │
                        ▼
              AI Handler (/v1/chat/completions 等)
```

- `IngressMiddleware` 位于 Gin 链最前端，先查 `RouteTable`
- 命中 Ingress 规则 → 走通用反向代理（不受 AI 网关鉴权/限流影响）
- 未命中 → 继续走现有 AI 路由，行为完全不变
- 同一个端口同时服务 AI + 通用 Ingress 流量

### 1.1 核心组件

| 组件 | 文件 | 职责 |
|---|---|---|
| `Controller` | `internal/ingress/controller.go` | controller-runtime Reconciler，watch Ingress，更新 RouteTable |
| `RouteTable` | `internal/ingress/route_table.go` | 线程安全的路由表，支持热替换 |
| `Middleware` | `internal/ingress/gin_middleware.go` | Gin handler，查表 + 代理 |
| `Proxy` | `internal/proxy/proxy.go` | ReverseProxy 封装，支持 retry、CORS、rewrite、gzip、WebSocket |
| `BackendSelector` | `internal/ingress/selector.go` | 后端选择：canary、affinity、WRR、IP whitelist |
| `IngressLimiter` | `internal/ingress/limiter.go` | 每路由 token bucket RPS + 并发连接限制 |
| `TLSManager` | `internal/ingress/tls_manager.go` | 从 K8s Secret 加载证书，支持 SNI + wildcard |

---

## 2. 部署模式

### 2.1 IngressClass（推荐）

Gateyes 只处理 `ingressClassName: gateyes` 的 Ingress，不干扰现有 Controller。

```yaml
apiVersion: networking.k8s.io/v1
kind: IngressClass
metadata:
  name: gateyes
spec:
  controller: gateway.gateyes.io/ingress-controller
```

Helm 中开启：

```yaml
ingressController:
  enabled: true
  class: gateyes
```

### 2.2 配置开关

```yaml
ingress:
  enabled: true
  class: gateyes
  watchNamespace: ""        # 空字符串 = 所有 namespace
  tlsEnabled: false
  tlsSecretNamespace: ""    # TLS Secret 所在 namespace
  defaultBackend: ""        # fallback service

proxy:
  connectTimeout: 5
  readTimeout: 60
  sendTimeout: 60
  idleConnTimeout: 90
  maxIdleConns: 100
  maxConnsPerHost: 10
  maxBodySize: 0            # 0 = unlimited

discovery:
  type: kubernetes          # kubernetes | consul | nacos | etcd | static
```

---

## 3. Nginx 注解兼容矩阵

| Nginx 注解 | Gateyes 支持 | 说明 |
|---|---|---|
| `rewrite-target` | ✅ | URL Path 前缀重写 |
| `ssl-redirect` | ✅ | HTTP → HTTPS 301 |
| `proxy-body-size` | ✅ | 请求体大小限制（返回 413） |
| `proxy-read-timeout` | ✅ | 读超时 |
| `proxy-send-timeout` | ✅ | 写超时 |
| `proxy-connect-timeout` | ✅ | 连接超时 |
| `enable-cors` + `cors-*` | ✅ | CORS 响应头 + OPTIONS 预检 |
| `limit-rps` | ✅ | 每客户端 RPS 限流（token bucket） |
| `limit-connections` | ✅ | 每客户端并发连接限制 |
| `affinity` (cookie) | ✅ | Session 亲和，基于 cookie |
| `session-cookie-name` | ✅ | 亲和 cookie 名称 |
| `canary` + `canary-weight` | ✅ | 金丝雀按权重分流 |
| `canary-by-header` | ✅ | 按 header 强制走 canary |
| `whitelist-source-range` | ✅ | IP 白名单（其余 403） |
| `backend-protocol` | ✅ | `HTTP` / `HTTPS` |
| `proxy-next-upstream` | ✅ | 5xx / 连接失败时重试下一后端 |
| `proxy-next-upstream-tries` | ✅ | 最大重试次数 |
| `enable-gzip` | ✅ | 响应 gzip 压缩 |

### 不支持（需手动迁移）

| Nginx 注解 | 原因 |
|---|---|
| `configuration-snippet` | 无 Nginx Lua/script 执行环境 |
| `server-snippet` | 同上 |
| `modsecurity-*` | WAF 不在当前 scope |
| `auth-type: basic` | 需评估是否映射到现有 API Key 体系 |

---

## 4. 服务发现

Ingress `spec.rules.http.paths.backend.service` 中的 service name 通过 `ServiceDiscovery` 接口解析为后端地址。

### 4.1 Kubernetes（默认）

```yaml
discovery:
  type: kubernetes
```

- 通过 controller-runtime client 实时查询 `Endpoints` / `EndpointSlice`
- 自动跟踪 Pod IP 变化

### 4.2 Consul

```yaml
discovery:
  type: consul
  consul:
    addr: "localhost:8500"
    datacenter: "dc1"
    token: ""           # ACL token，可选
```

### 4.3 Nacos

```yaml
discovery:
  type: nacos
  nacos:
    serverAddr: "localhost:8848"
    namespaceId: ""
    group: "DEFAULT_GROUP"
```

### 4.4 Etcd

```yaml
discovery:
  type: etcd
  etcd:
    endpoints:
      - "localhost:2379"
    username: ""
    password: ""
```

### 4.5 Static

静态地址始终作为 fallback 注册，无需显式配置。

---

## 5. TLS 终止

### 5.1 从 Ingress TLS Secret 加载

当 `ingress.tlsEnabled: true` 时，Controller 会解析 Ingress `spec.tls` 字段：

```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: api
spec:
  ingressClassName: gateyes
  tls:
    - hosts:
        - api.example.com
      secretName: api-tls-secret
  rules:
    - host: api.example.com
      http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              service:
                name: api-svc
                port:
                  number: 8080
```

Gateyes 会从 `api-tls-secret` 中读取 `tls.crt` 和 `tls.key`，通过 SNI 匹配请求域名，支持 `*.example.com` wildcard 回退。

### 5.2 证书热更新

Ingress reconcile 时会重新加载 TLS Secret，无需重启进程。

---

## 6. 金丝雀发布

### 6.1 按权重分流

```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: api-canary
  annotations:
    nginx.ingress.kubernetes.io/canary: "true"
    nginx.ingress.kubernetes.io/canary-weight: "20"
spec:
  ingressClassName: gateyes
  rules:
    - host: api.example.com
      http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              service:
                name: api-canary-svc
                port:
                  number: 8080
```

Gateyes 将 stable Ingress 和 canary Ingress 合并到同一条 `RouteRule`，20% 流量进入 `api-canary-svc`。

### 6.2 按 Header 强制分流

```yaml
annotations:
  nginx.ingress.kubernetes.io/canary: "true"
  nginx.ingress.kubernetes.io/canary-by-header: "X-Canary"
```

请求携带 `X-Canary: always` 时，100% 走 canary 后端。

---

## 7. 示例：从 Nginx 迁移

### 迁移前（Nginx）

```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: api
  annotations:
    nginx.ingress.kubernetes.io/rewrite-target: /v2
    nginx.ingress.kubernetes.io/ssl-redirect: "true"
    nginx.ingress.kubernetes.io/proxy-body-size: 10m
    nginx.ingress.kubernetes.io/enable-cors: "true"
    nginx.ingress.kubernetes.io/limit-rps: "100"
    nginx.ingress.kubernetes.io/affinity: "cookie"
    nginx.ingress.kubernetes.io/session-cookie-name: "INGRESS_SESSION"
spec:
  ingressClassName: nginx
  rules:
    - host: api.example.com
      http:
        paths:
          - path: /api
            pathType: Prefix
            backend:
              service:
                name: api-svc
                port:
                  number: 8080
```

### 迁移后（Gateyes）

只需改 `ingressClassName`：

```yaml
spec:
  ingressClassName: gateyes   # <-- 只改这一行
```

其余注解全部兼容，无需修改。

---

## 8. 监控

Ingress 流量独立指标：

| 指标 | labels | 说明 |
|---|---|---|
| `ingress_requests_total` | `host,path,status` | Ingress 请求总数 |
| `ingress_request_duration_seconds` | `host,path` | 请求延迟 |
| `ingress_backend_errors_total` | `host,backend` | 后端错误数 |

与 LLM 指标 namespace 隔离，避免互相干扰。

---

## 9. 限制与已知问题

1. **单端口 TLS**：当前 TLS 启用时，`ListenAndServeTLS` 与 HTTP 共享同一端口。生产环境如需同时暴露 80/443，建议前置 LoadBalancer 做端口分流。
2. **Regex rewrite**：`rewrite-target` 中的 `$1` 等捕获组暂不支持，按字面量处理。
3. **GRPC**：`backend-protocol: GRPC` 已解析但未做特殊协议处理，目前按 HTTP/2 代理。
4. **多副本 status 冲突**：多副本同时更新 Ingress status 可能产生冲突，建议生产环境启用 controller-runtime leader election（默认未开启）。
