# GitHub Actions Docker 镜像构建指南

## 📦 现有配置

项目已配置好完整的 Docker 镜像构建 workflow，包含：

### 1. 主构建流程 (`.github/workflows/docker-build.yml`)

**触发方式：**
- 自动触发：推送任何 git tag
- 手动触发：在 GitHub Actions 页面手动运行

**功能特性：**
- ✅ 多平台构建（linux/amd64, linux/arm64）
- ✅ 双镜像仓库推送（Docker Hub + GitHub Container Registry）
- ✅ 智能标签策略（latest、beta、版本号）
- ✅ 版本号自动注入
- ✅ 构建缓存加速

**构建产物：**
```
docker.io/tbphp/gpt-load:1.4.8
docker.io/tbphp/gpt-load:latest
ghcr.io/tbphp/gpt-load:1.4.8
ghcr.io/tbphp/gpt-load:latest
```

### 2. 开发分支构建 (`.github/workflows/docker-build-dev.yml`)

**触发方式：**
- 推送到 `main` 或 `develop` 分支

**功能特性：**
- ✅ 仅推送到 GitHub Container Registry
- ✅ 使用分支名和 commit SHA 作为标签
- ✅ main 分支自动标记为 `dev`

**构建产物：**
```
ghcr.io/tbphp/gpt-load:main
ghcr.io/tbphp/gpt-load:main-abc1234
ghcr.io/tbphp/gpt-load:dev
```

## 🚀 使用方法

### 方式一：推送 Git Tag（推荐用于正式发布）

```bash
# 1. 更新版本号
vim internal/version/version.go
# 修改: var Version = "1.4.8"

# 2. 提交更改
git add internal/version/version.go
git commit -m "chore: bump version to 1.4.8"
git push origin main

# 3. 创建并推送 tag
git tag v1.4.8
git push origin v1.4.8

# ✅ 自动触发构建！
```

**标签规则：**
- `v1.4.8` → 镜像标签：`1.4.8`, `latest`
- `v1.5.0-beta` → 镜像标签：`1.5.0-beta`, `beta`
- `v1.5.0-alpha.1` → 镜像标签：`1.5.0-alpha.1`（不会标记为 latest）

### 方式二：GitHub Release 发布

1. 进入 GitHub 仓库页面
2. **Releases → Draft a new release**
3. **Choose a tag** → 输入 `v1.4.8` → **Create new tag**
4. 填写 Release title 和描述
5. **Publish release**

✅ 自动触发构建并创建 GitHub Release

### 方式三：手动触发构建

1. 进入 **Actions → Build Docker Image**
2. 点击 **Run workflow**
3. 填写参数：
   - **Version tag**: `1.4.8`
   - **Push to registry**: ☑️
4. 点击 **Run workflow**

适用场景：
- 测试构建配置
- 重新构建旧版本
- 不想创建 git tag 的临时构建

### 方式四：推送到开发分支（自动测试）

```bash
git add .
git commit -m "feat: add new feature"
git push origin main

# ✅ 自动构建测试镜像 ghcr.io/tbphp/gpt-load:main
```

## 🔧 初次配置（仅需一次）

### 1. 配置 Docker Hub Secrets

**导航：** Settings → Secrets and variables → Actions → New repository secret

添加以下 Secrets：

| Name | Value | 获取方式 |
|------|-------|---------|
| `DOCKERHUB_USERNAME` | 你的 Docker Hub 用户名 | Docker Hub 账号 |
| `DOCKERHUB_TOKEN` | Docker Hub Access Token | 见下方说明 |

**获取 Docker Hub Token：**
1. 登录 [Docker Hub](https://hub.docker.com/)
2. 点击右上角头像 → **My Account**
3. 左侧菜单 → **Security** 或 **Personal access tokens**
4. 点击 **New Access Token** 或 **Generate New Token**
5. 填写 Token 信息：
   - **Description**: `GitHub Actions`
   - **Access permissions**: `Read, Write, Delete`（或 `Read & Write`）
6. 点击 **Generate**
7. **立即复制 token**（仅显示一次，关闭后无法再查看）

### 2. 启用 GitHub Container Registry

**自动启用**，无需额外配置。`GITHUB_TOKEN` 由 GitHub Actions 自动提供。

**查看已推送的镜像：**
- 仓库页面右侧 → **Packages** → `gpt-load`

## 📊 查看构建状态

### 方法一：GitHub Actions 页面

**Actions → Build Docker Image → 最新运行**

可查看：
- 构建日志
- 构建时间
- 推送的镜像标签
- 错误信息（如有）

### 方法二：徽章显示

在 README.md 中添加徽章：

```markdown
[![Docker Image](https://github.com/tbphp/gpt-load/actions/workflows/docker-build.yml/badge.svg)](https://github.com/tbphp/gpt-load/actions/workflows/docker-build.yml)
```

效果：![Docker Image](https://github.com/tbphp/gpt-load/actions/workflows/docker-build.yml/badge.svg)

## 🐛 常见问题排查

### 问题1：Docker Hub 认证失败

**错误信息：**
```
Error: failed to solve: failed to authorize: failed to fetch oauth token
```

**解决方法：**
1. 检查 `DOCKERHUB_USERNAME` 和 `DOCKERHUB_TOKEN` 是否正确
2. 确认 Token 权限包含 `Read, Write, Delete`
3. Token 可能过期，重新生成

### 问题2：GHCR 推送失败

**错误信息：**
```
Error: failed to push: unexpected status: 403 Forbidden
```

**解决方法：**
1. 确认 workflow 中有 `packages: write` 权限
2. 检查仓库设置：Settings → Actions → General → Workflow permissions
3. 确保选择了 `Read and write permissions`

### 问题3：构建超时

**错误信息：**
```
Error: The operation was canceled.
```

**解决方法：**
1. 增加 `timeout-minutes`（当前设置：45 分钟）
2. 检查网络问题（依赖下载慢）
3. 使用构建缓存（已启用）

### 问题4：多平台构建失败

**错误信息：**
```
ERROR: failed to solve: failed to prepare xxxxxxx: not found
```

**解决方法：**
1. 检查 Dockerfile 是否使用 `--platform=$BUILDPLATFORM`
2. 确认 QEMU 正确设置
3. 临时禁用某个平台测试：`platforms: linux/amd64`

### 问题5：版本号注入失败

**现象：** 构建的镜像显示版本号为 `1.0.0`

**解决方法：**
1. 确认 tag 格式正确（如 `v1.4.8`）
2. 手动触发时填写正确的版本号
3. 检查 Dockerfile 中 `ARG VERSION` 是否正确使用

## 🔐 安全最佳实践

### 1. Token 权限最小化

**Docker Hub Token：**
- 仅授予必要权限（Read, Write）
- 定期轮换 Token
- 为 CI/CD 创建专用 Token

### 2. 镜像签名（可选）

添加镜像签名验证：

```yaml
- name: Sign the image with sigstore
  run: cosign sign --yes docker.io/tbphp/gpt-load:${{ github.ref_name }}
```

### 3. 漏洞扫描

添加镜像安全扫描：

```yaml
- name: Run Trivy vulnerability scanner
  uses: aquasecurity/trivy-action@master
  with:
    image-ref: 'docker.io/tbphp/gpt-load:${{ github.ref_name }}'
    format: 'sarif'
    output: 'trivy-results.sarif'
```

## 📈 构建优化建议

### 1. 使用构建缓存

✅ 已启用 GitHub Actions 缓存：
```yaml
cache-from: type=gha
cache-to: type=gha,mode=max
```

### 2. 多阶段构建优化

当前 Dockerfile 已使用多阶段构建：
- `node:20-alpine` → 构建前端
- `golang:1.25-alpine` → 构建后端
- `alpine` → 最终运行镜像

### 3. 并行构建多个变体

如需构建不同配置的镜像（如不同基础镜像），使用矩阵策略：

```yaml
strategy:
  matrix:
    base: [alpine, ubuntu]
```

## 📝 版本发布流程（推荐）

### 完整发布检查清单

1. ✅ 更新版本号
   - `internal/version/version.go`
   - 提交代码

2. ✅ 更新 CHANGELOG
   - 记录新功能、Bug 修复、Breaking Changes

3. ✅ 运行测试
   ```bash
   go test ./...
   ```

4. ✅ 本地构建测试
   ```bash
   docker build --build-arg VERSION=1.4.8 -t gpt-load:test .
   docker run --rm gpt-load:test
   ```

5. ✅ 创建 Git Tag
   ```bash
   git tag v1.4.8
   git push origin v1.4.8
   ```

6. ✅ 等待 CI/CD 构建完成
   - 查看 GitHub Actions
   - 确认镜像推送成功

7. ✅ 创建 GitHub Release
   - 填写 Release Notes
   - 附加二进制文件（如有）

8. ✅ 验证镜像
   ```bash
   docker pull tbphp/gpt-load:1.4.8
   docker run --rm tbphp/gpt-load:1.4.8
   ```

9. ✅ 更新文档
   - 更新安装指南中的版本号
   - 发布公告

## 🎯 总结

当前配置已经非常完善，支持：

✅ 自动化构建（推送 tag）
✅ 手动触发构建
✅ 多平台支持（amd64 + arm64）
✅ 双仓库推送（Docker Hub + GHCR）
✅ 智能标签管理
✅ 构建缓存加速
✅ 开发分支自动构建

**推荐发布流程：** 使用 Git Tag + GitHub Release 方式，既规范又自动化。
