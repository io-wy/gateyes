# 变更日志

## 未发布

- **破坏性变更**：将 ingress controller、微服务网关代理、服务发现（consul/etcd/nacos/k8s）以及 K8s CRD operator（ProviderReconciler）迁移到 `archive/ingress-and-msgw` 归档分支。主分支现在是纯 AI 网关 —— go.mod 中不再包含 k8s.io/controller-runtime/consul/etcd/nacos 依赖，请求路径中不再包含 ingress middleware，provider 管理仅通过配置/数据库/Admin API 进行
- 新增 CI 基线工作流
- 新增 release 工作流：多架构镜像构建、SBOM、签名与来源证明
- 新增 Dockerfile、docker-compose、Helm chart 与部署文档
- 移除默认配置中提交的 provider 密钥，将示例改为环境变量占位符
