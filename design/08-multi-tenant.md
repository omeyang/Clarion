# 08 - 租户体系与认证

---

## 1. 背景与动机

Clarion 当前是完全单租户的，所有数据共享一个命名空间，没有认证机制。要服务多个客户，需要先解决两个基础问题：

1. **认证**（Authentication）：谁在访问？—— 验证请求方身份
2. **数据隔离**（Multi-tenancy）：看到什么？—— 确保每个客户只能访问自己的数据

**认证是多租户的前提**。没有认证的多租户只是在 Header 里自报家门，任何人设置 `X-Tenant-ID` 就能访问任意租户数据。`xtenant` 的定位是**已认证上下文中的传播层**，不是认证层（其 `doc.go` 推荐的中间件顺序是 `Auth → xtenant → Business`）。因此本文档的设计顺序是：**先认证，后隔离**。

### 1.1 当前不做（按需演进）

遵循 `01-goals-and-constraints.md` 的约束 —— 单人开发、成本敏感、快速验证。以下能力当前不实现，但设计上预留了扩展点，达到触发条件时按 §10 演进路线升级：

| 当前不做 | 当前方案 | 演进触发条件 |
|---------|---------|-------------|
| 订阅/计费系统 | 配额字段直接在 tenant 表 | 需要按套餐计费、订阅续期/过期管理时 |
| 管理端 HTTP API | CLI 工具管理 | 需要 Web 管理界面或非技术人员管理租户时 |
| 公共模板 / Fork | 每个租户独立创建模板 | 积累了可复用的行业模板、需要模板市场时 |
| Redis 配额计数器 | DB 查询 + 内存计数 | 并发超过 20 路、单 Worker 扩展为多 Worker 时 |
| 功能开关 | 所有租户同一套功能 | 需要按套餐区分功能（如 trial 无 AI 摘要）时 |
| RBAC / ABAC | 两种角色：管理员（CLI）+ 租户 | 租户内需要区分管理员/操作员/只读角色时 |
| Row-Level Security | 应用层 `WHERE tenant_id` | 合规审计要求数据库级强制隔离时 |
| Schema / DB 隔离 | 共享表 + `tenant_id` | 金融/医疗等合规场景要求物理隔离时 |
| OAuth2 / OIDC | API Key + JWT | 需要对接第三方身份提供商（如企业微信登录）时 |
| Refresh Token | 过期后用 API Key 重新获取 | 客户端需要无感续期（如长时间运行的前端应用）时 |

---

## 2. 认证体系

### 2.1 认证流程

两层设计：API Key（长期凭据）换取 JWT（短期令牌）。

```
                          租户持有
                          ┌──────────────────────────────────────┐
                          │ API Key（长期凭据，创建时展示一次）      │
                          │ ck_live_7Kd9mRqP4xYzN2wL5vBn8jF1hG  │
                          └──────────────────┬───────────────────┘
                                             │
              ┌──────────────────────────────▼─────────────────────────────┐
              │ POST /api/v1/auth/token                                    │
              │ Body: {"api_key": "ck_live_7Kd9mRqP..."}                  │
              │                                                            │
              │ 1. SHA-256(api_key) → 查 api_keys 表匹配 key_hash          │
              │ 2. 检查 key 状态 = active 且 租户状态 = active              │
              │ 3. 签发 JWT(tenant_id, exp)                                │
              └──────────────────────────────┬─────────────────────────────┘
                                             │
                          ┌──────────────────▼───────────────────┐
                          │ JWT（短期令牌，默认 15 分钟）           │
                          │ eyJhbGciOiJIUzI1NiIs...              │
                          └──────────────────┬───────────────────┘
                                             │
              ┌──────────────────────────────▼─────────────────────────────┐
              │ 业务 API 请求                                               │
              │ Authorization: Bearer eyJhbGciOiJIUzI1NiIs...             │
              │                                                            │
              │ 1. HMAC-SHA256 验证签名 + 检查过期（纯计算，无 DB 查询）      │
              │ 2. 提取 tenant_id → 注入 context（通过 xtenant）            │
              │ 3. 业务逻辑从 context 获取 tenant_id                        │
              └────────────────────────────────────────────────────────────┘
```

**设计决策**：

- **API Key + JWT 两层** —— API Key 是长期凭据（低频传输，仅换 token 时用），JWT 是短期令牌（高频传输，每次请求携带）。即使 JWT 泄露，窗口期只有 15 分钟
- **JWT 验证无 DB 查询** —— HMAC-SHA256 签名验证是纯计算，不增加数据库负载，符合「热路径零 DB」原则
- **不做 Refresh Token** —— Token 过期后客户端用 API Key 重新调用 `/auth/token`，一次 HTTP 请求，足够简单

### 2.2 API Key 设计

**格式**：`ck_live_` 前缀 + 32 字节 base62 随机串

```
ck_live_7Kd9mRqP4xYzN2wL5vBn8jF1hG3tS6aE
├─────┤ ├──────────────────────────────────┤
 前缀     32 字节 base62（~190 bit 熵）
```

- `ck_` = Clarion Key
- `live_` = 生产环境（预留 `test_` 用于测试环境）
- 前缀便于在日志中识别、在代码仓库中扫描泄露

**存储**：数据库只存 SHA-256 哈希，不存明文。API Key 在创建时展示一次，之后无法找回。

### 2.3 JWT Token

**签名算法**：HMAC-SHA256（对称密钥，简单高效，适合单服务签发/验证场景）

**Claims 结构**：

```json
{
  "tid": "0192d5e8-7a3b-7def-9c1a-1234567890ab",
  "kid": 42,
  "iat": 1710403200,
  "exp": 1710404100
}
```

| 字段 | 含义 | 说明 |
|------|------|------|
| `tid` | tenant_id | UUID v7 字符串，注入 context 后用于所有数据查询 |
| `kid` | api_key ID | 关联签发此 token 的 key，用于未来吊销判断 |
| `iat` | 签发时间 | 标准 JWT 字段 |
| `exp` | 过期时间 | `iat + token_ttl`（默认 15 分钟） |

**设计决策**：

- Claim 名缩短（`tid` 而非 `tenant_id`）—— JWT 每次请求传输，减小体积
- 包含 `kid`（key ID）—— 吊销 API Key 后，可选择性拒绝该 key 签发的在途 token（当前阶段不做，15 分钟 TTL 足够）
- 不包含 `role` 字段 —— 当前只有租户通过 HTTP 访问，管理员通过 CLI 操作数据库，无需在 JWT 中区分角色

### 2.4 数据模型：api_keys 表

```sql
CREATE TABLE IF NOT EXISTS api_keys (
    id           BIGSERIAL   PRIMARY KEY,
    tenant_id    UUID        NOT NULL REFERENCES tenants(id),
    key_prefix   TEXT        NOT NULL,     -- 前 8 字符，用于展示（如 "ck_live_7K"）
    key_hash     TEXT        NOT NULL,     -- SHA-256(完整 API Key)
    name         TEXT        NOT NULL DEFAULT '',  -- 描述，如 "生产环境"
    status       TEXT        NOT NULL DEFAULT 'active',  -- active / revoked
    last_used_at TIMESTAMPTZ,              -- 最近使用时间（换 token 时更新）
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_api_keys_hash ON api_keys (key_hash);
CREATE INDEX IF NOT EXISTS idx_api_keys_tenant ON api_keys (tenant_id);
```

**设计决策**：

- `key_prefix` 保留前 8 字符 —— 管理时展示 `ck_live_7K******`，让管理员辨认是哪个 key
- `key_hash` 唯一索引 —— Token 端点通过哈希快速定位 key 记录
- `last_used_at` —— 帮助管理员识别不再使用的 key，适时吊销

### 2.5 Go 实现

```go
// internal/auth/token.go

// Claims 是 JWT 中携带的租户认证信息。
type Claims struct {
    TenantID string `json:"tid"`
    KeyID    int64  `json:"kid"`
    jwt.RegisteredClaims
}

// Issuer 负责签发和验证 JWT。
type Issuer struct {
    secret []byte
    ttl    time.Duration
}

// Issue 签发 JWT。
func (i *Issuer) Issue(tenantID string, keyID int64) (string, time.Time, error) {
    now := time.Now()
    exp := now.Add(i.ttl)

    claims := Claims{
        TenantID: tenantID,
        KeyID:    keyID,
        RegisteredClaims: jwt.RegisteredClaims{
            IssuedAt:  jwt.NewNumericDate(now),
            ExpiresAt: jwt.NewNumericDate(exp),
        },
    }

    token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
    signed, err := token.SignedString(i.secret)
    if err != nil {
        return "", time.Time{}, fmt.Errorf("签发 token: %w", err)
    }
    return signed, exp, nil
}

// Verify 验证 JWT 签名和过期时间，返回 Claims。
func (i *Issuer) Verify(tokenStr string) (*Claims, error) {
    token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(_ *jwt.Token) (any, error) {
        return i.secret, nil
    })
    if err != nil {
        return nil, fmt.Errorf("验证 token: %w", err)
    }

    claims, ok := token.Claims.(*Claims)
    if !ok || !token.Valid {
        return nil, ErrInvalidToken
    }
    return claims, nil
}
```

```go
// internal/auth/handler.go

// HandleToken 处理 API Key → JWT 的换取请求。
func (h *Handler) HandleToken(w http.ResponseWriter, r *http.Request) {
    var req struct {
        APIKey string `json:"api_key"`
    }
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        writeJSON(w, http.StatusBadRequest, errorResp("请求体格式错误"))
        return
    }

    // 1. 查找 API Key
    keyHash := sha256Hex(req.APIKey)
    key, err := h.store.GetByHash(r.Context(), keyHash)
    if err != nil {
        writeJSON(w, http.StatusUnauthorized, errorResp("无效的 API Key"))
        return
    }
    if key.Status != "active" {
        writeJSON(w, http.StatusUnauthorized, errorResp("API Key 已吊销"))
        return
    }

    // 2. 检查租户状态
    tenant, err := h.tenantStore.Get(r.Context(), key.TenantID)
    if err != nil || tenant.Status != "active" {
        writeJSON(w, http.StatusForbidden, errorResp("租户已暂停"))
        return
    }

    // 3. 签发 JWT
    token, expiresAt, err := h.issuer.Issue(key.TenantID, key.ID)
    if err != nil {
        writeJSON(w, http.StatusInternalServerError, errorResp("签发 token 失败"))
        return
    }

    // 4. 更新 last_used_at（异步，不阻塞响应）
    go h.store.TouchLastUsed(context.Background(), key.ID)

    writeJSON(w, http.StatusOK, map[string]any{
        "token":      token,
        "expires_at": expiresAt,
        "tenant_id":  key.TenantID,
    })
}
```

### 2.6 认证中间件

```go
// internal/auth/middleware.go

// Middleware 验证 JWT 并将租户信息注入 context。
// 有 token → 验证，注入 Claims + xtenant。
// 无 token → 放行（由下游 RequireTenant 决定是否拒绝）。
// token 无效 → 立即 401。
func Middleware(issuer *Issuer) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            tokenStr := extractBearerToken(r)
            if tokenStr == "" {
                next.ServeHTTP(w, r)
                return
            }

            claims, err := issuer.Verify(tokenStr)
            if err != nil {
                writeJSON(w, http.StatusUnauthorized, errorResp("无效或过期的 token"))
                return
            }

            ctx := WithClaims(r.Context(), claims)
            ctx, _ = xtenant.WithTenantID(ctx, claims.TenantID)
            next.ServeHTTP(w, r.WithContext(ctx))
        })
    }
}

// RequireTenant 要求请求必须携带有效的租户 JWT。
// 用于包装业务 API 路由组。
func RequireTenant(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        claims := ClaimsFromContext(r.Context())
        if claims == nil || claims.TenantID == "" {
            writeJSON(w, http.StatusUnauthorized, errorResp("需要认证"))
            return
        }
        next.ServeHTTP(w, r)
    })
}
```

**设计决策**：

- **Middleware 只验证不拒绝无 token 请求** —— 公开端点（healthz、token）不需要 token，拒绝逻辑交给 `RequireTenant`
- **token 无效立即 401** —— 有 token 但验证失败说明客户端认为自己已认证，应该明确告知失败
- **Claims 注入 context 后同时调用 xtenant.WithTenantID** —— 下游 Service 层可以继续用 `xtenant.RequireTenantID(ctx)` 获取 tenant_id，与 xtenant 的设计模式一致

### 2.7 路由结构

```go
// internal/api/router.go

func Router(logger *slog.Logger, deps *Dependencies) http.Handler {
    mux := http.NewServeMux()

    // ── 公开端点（无认证）───────────────────────
    mux.HandleFunc("GET /healthz", handleHealthz)
    mux.HandleFunc("POST /api/v1/auth/token", deps.AuthHandler.HandleToken)

    // ── 业务端点（认证 + 租户身份）──────────────────
    tenantMux := http.NewServeMux()
    handler.NewContactHandler(deps.Services.Contacts).Register(tenantMux)
    handler.NewTemplateHandler(deps.Services.Templates).Register(tenantMux)
    handler.NewTaskHandler(deps.Services.Tasks).Register(tenantMux)
    handler.NewCallHandler(deps.Services.Calls).Register(tenantMux)
    mux.Handle("/api/v1/", auth.RequireTenant(tenantMux))

    // ── 全局中间件 ──────────────────────────────
    var h http.Handler = mux
    h = RequestIDMiddleware(h)
    h = CORSMiddleware(h)
    h = auth.Middleware(deps.Issuer)(h)  // JWT 验证 + Claims 注入
    h = LoggingMiddleware(logger)(h)
    h = RecoveryMiddleware(logger)(h)

    return h
}
```

**设计决策**：

- **路由分组替代路径字符串判断** —— 不在中间件内用 `strings.HasPrefix` 判断跳过路径，而是用 ServeMux 层级隔离，结构清晰且不会遗漏
- **公开端点直接挂在顶层 mux** —— `/healthz` 和 `/auth/token` 不经过 `RequireTenant`，Auth Middleware 放行无 token 的请求
- **Go 1.22+ ServeMux 精确匹配** —— `/api/v1/auth/token` 注册了具体路径，优先于 `/api/v1/` 前缀匹配，不会被 RequireTenant 拦截

---

## 3. 租户管理

### 3.1 租户 ID 设计

租户 ID 是全系统最高频的外键 —— 出现在每一行 contacts、calls、tasks、templates、opportunities 中。ID 的选型直接影响索引效率、分片均匀性和排序能力。

| 方案 | 可排序 | 分布均匀 | 大小 | 可读性 | 标准化 |
|------|:---:|:---:|:---:|:---:|:---:|
| TEXT slug (`xian-fangchan`) | ✗ | ✗ | 变长 | 好 | ✗ |
| BIGSERIAL | ✓ | ✗（顺序，热点） | 8B | 差 | ✗ |
| UUID v4 | ✗ | ✓ | 16B | 差 | RFC 9562 |
| **UUID v7** | **✓** | **✓** | **16B** | 差 | **RFC 9562** |
| ULID | ✓ | ✓ | 16B | 较好 | 非 RFC |
| Snowflake | ✓ | ✗（需协调） | 8B | 差 | Twitter |

**选择 UUID v7**（RFC 9562），理由：

- **时间有序**：前 48 bit 是毫秒级 Unix 时间戳，B-tree 索引插入顺序，天然按创建时间排序
- **分布均匀**：后 74 bit 是密码学随机数，hash 分片时均匀分布，无热点
- **定长紧凑**：PostgreSQL `UUID` 类型 16 字节，比 TEXT slug 更紧凑，比较更快
- **RFC 标准**：`gofrs/uuid/v5` 原生支持 UUID v7，且保证毫秒内单调递增（monotonic counter），同一毫秒生成的多个 UUID 仍然有序，B-tree 插入零回退
- **无协调**：不像 Snowflake 需要 worker ID 分配，任意节点可独立生成

人类可读性通过独立的 `slug` 字段解决（日志、CLI、URL 中使用），不牺牲 ID 的技术属性。

### 3.2 数据模型：tenants 表

```sql
CREATE TABLE IF NOT EXISTS tenants (
    id                UUID        PRIMARY KEY,  -- UUID v7，Go 端 uuid.NewV7() 生成
    slug              TEXT        NOT NULL UNIQUE,  -- 人类可读标识，如 "xian-fangchan"
    name              TEXT        NOT NULL,     -- 显示名称，如 "西安美好房产"
    contact_person    TEXT        NOT NULL DEFAULT '',
    contact_phone     TEXT        NOT NULL DEFAULT '',
    status            TEXT        NOT NULL DEFAULT 'active',  -- active / suspended
    daily_call_limit  INT         NOT NULL DEFAULT 100,       -- 每日外呼上限
    max_concurrent    INT         NOT NULL DEFAULT 3,         -- 最大并发通话数
    settings          JSONB       NOT NULL DEFAULT '{}',      -- 租户级配置（通知渠道等）
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

**设计决策**：

- **`id` UUID v7 + `slug` TEXT** —— ID 用于所有外键和查询（技术属性优先），slug 用于人类交互（CLI 参数、日志输出、URL 路径）。两者都唯一，但 slug 可修改，id 不可变
- **配额直接在 tenant 表** —— 不需要独立的 subscription 表。调配额就是 `UPDATE tenants SET daily_call_limit = 200`，简单直接
- **只保留两个关键配额** —— `daily_call_limit`（控成本）和 `max_concurrent`（控资源），足以满足当前需求。月度限额、联系人上限等在真正需要时再加列
- **status 只有 active / suspended** —— 不需要 expired / cancelled 等细分状态，暂停即停用
- `settings` JSONB —— 存租户级配置（企业微信 webhook URL 等），避免频繁加列

### 3.3 Go 模型

```go
// internal/model/tenant.go

// Tenant 表示平台租户。
type Tenant struct {
    ID             string          `json:"id"`   // UUID v7 字符串
    Slug           string          `json:"slug"`
    Name           string          `json:"name"`
    ContactPerson  string          `json:"contact_person"`
    ContactPhone   string          `json:"contact_phone"`
    Status         string          `json:"status"`
    DailyCallLimit int             `json:"daily_call_limit"`
    MaxConcurrent  int             `json:"max_concurrent"`
    Settings       json.RawMessage `json:"settings"`
    CreatedAt      time.Time       `json:"created_at"`
    UpdatedAt      time.Time       `json:"updated_at"`
}

// APIKey 表示租户的 API 访问凭据（不含密钥明文）。
type APIKey struct {
    ID         int64      `json:"id"`
    TenantID   string     `json:"tenant_id"`  // UUID v7 字符串
    KeyPrefix  string     `json:"key_prefix"`
    KeyHash    string     `json:"-"`           // 不序列化
    Name       string     `json:"name"`
    Status     string     `json:"status"`
    LastUsedAt *time.Time `json:"last_used_at"`
    CreatedAt  time.Time  `json:"created_at"`
}
```

**Go 中 tenant ID 统一用 `string` 类型**（UUID 的字符串表示），理由：
- `xtenant.TenantID(ctx)` 返回 `string`，保持一致
- pgx v5 可以将 PostgreSQL `UUID` 列直接扫描到 Go `string`
- 避免在每个包中引入 `uuid.UUID` 类型依赖
- UUID 生成只在一个地方（创建租户时 `uuid.Must(uuid.NewV7()).String()`），其余代码只传递字符串

### 3.4 现有模型变更

所有业务模型增加 `TenantID` 字段：

```go
type Contact struct {
    ID       int64  `json:"id"`
    TenantID string `json:"tenant_id"` // 新增
    // ... 其余字段不变
}

type ScenarioTemplate struct {
    ID       int64  `json:"id"`
    TenantID string `json:"tenant_id"` // 新增
    // ... 其余字段不变
}

type CallTask struct {
    ID       int64  `json:"id"`
    TenantID string `json:"tenant_id"` // 新增
    // ... 其余字段不变
}

type Call struct {
    ID       int64  `json:"id"`
    TenantID string `json:"tenant_id"` // 新增
    // ... 其余字段不变
}
```

### 3.5 管理方式：CLI 工具

当前阶段通过 CLI 子命令管理租户，不做管理端 HTTP API。CLI 参数使用 `slug`（人类可读），内部自动生成 UUID v7 作为 `id`：

```bash
# 创建租户（自动生成 UUID v7 作为 id）
clarion admin create-tenant \
    --slug xian-fangchan \
    --name "西安美好房产" \
    --contact "张三" \
    --phone "13800138000" \
    --daily-limit 200 \
    --max-concurrent 5
# → 租户已创建：id=0192d5e8-7a3b-7def-9c1a-1234567890ab slug=xian-fangchan

# 创建 API Key（用 slug 指定租户，输出完整 key，仅展示一次）
clarion admin create-apikey --tenant xian-fangchan --name "生产环境"
# → API Key: ck_live_7Kd9mRqP4xYzN2wL5vBn8jF1hG3tS6aE
# → 请妥善保存，此密钥无法再次查看

# 查看租户列表（显示 id + slug + name + status + 配额）
clarion admin list-tenants

# 暂停 / 恢复租户（支持 slug 或 id）
clarion admin set-tenant-status --tenant xian-fangchan --status suspended

# 吊销 API Key
clarion admin revoke-apikey --id 42

# 调整配额
clarion admin set-quota --tenant xian-fangchan --daily-limit 500 --max-concurrent 10
```

**设计决策**：

- **CLI 而非 HTTP API** —— 管理操作低频（创建租户一个月几次），CLI 直连数据库执行，不需要额外的 HTTP 认证层
- **CLI 复用 config 和 store** —— 通过 `cmd/clarion admin` 子命令实现，读取 `clarion.toml` 连接数据库
- **后续可升级** —— 当需要 Web 管理界面时，在 store 层之上暴露 HTTP API + 管理员 JWT 即可

---

## 4. 多租户数据隔离

### 4.1 隔离策略：共享表 + tenant_id

采用**共享数据库、共享表、逻辑隔离**方案。所有业务表新增 `tenant_id` 列，所有查询强制带 `WHERE tenant_id = ?`。

| 方案 | 优点 | 缺点 | 适合场景 |
|------|------|------|----------|
| **共享表 + tenant_id** | 简单、成本低、易维护 | 需应用层保证隔离 | 租户数 < 1000，数据量中等 |
| Schema 隔离 | 物理隔离更强 | 迁移复杂度翻倍、连接池膨胀 | 合规要求高（金融、医疗） |
| 数据库隔离 | 完全物理隔离 | 运维成本高、跨租户查询困难 | 大企业独占部署 |

Clarion 选方案一。如果未来有合规要求，可以升级到 Schema 隔离（`tenant_id` 列是前置条件，不白做）。

### 4.2 现有表变更

```sql
-- 003_multi_tenant.up.sql

-- ═══ 1. 新增表 ═══

CREATE TABLE IF NOT EXISTS tenants (
    id                UUID        PRIMARY KEY,  -- UUID v7，Go 端生成
    slug              TEXT        NOT NULL UNIQUE,
    name              TEXT        NOT NULL,
    contact_person    TEXT        NOT NULL DEFAULT '',
    contact_phone     TEXT        NOT NULL DEFAULT '',
    status            TEXT        NOT NULL DEFAULT 'active',
    daily_call_limit  INT         NOT NULL DEFAULT 100,
    max_concurrent    INT         NOT NULL DEFAULT 3,
    settings          JSONB       NOT NULL DEFAULT '{}',
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS api_keys (
    id           BIGSERIAL   PRIMARY KEY,
    tenant_id    UUID        NOT NULL REFERENCES tenants(id),
    key_prefix   TEXT        NOT NULL,
    key_hash     TEXT        NOT NULL,
    name         TEXT        NOT NULL DEFAULT '',
    status       TEXT        NOT NULL DEFAULT 'active',
    last_used_at TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_api_keys_hash ON api_keys (key_hash);
CREATE INDEX IF NOT EXISTS idx_api_keys_tenant ON api_keys (tenant_id);

-- ═══ 2. 默认租户（归属已有数据）═══
-- 使用固定 UUID 作为默认租户 ID，便于跨环境一致迁移

INSERT INTO tenants (id, slug, name, status)
VALUES ('00000000-0000-0000-0000-000000000000', 'default', '默认租户', 'active')
ON CONFLICT (id) DO NOTHING;

-- ═══ 3. 业务表增加 tenant_id ═══
-- 分三步：加 nullable 列 → 填充 → 改 NOT NULL

-- Step 1: 加 nullable 列
ALTER TABLE contacts ADD COLUMN IF NOT EXISTS tenant_id UUID REFERENCES tenants(id);
ALTER TABLE scenario_templates ADD COLUMN IF NOT EXISTS tenant_id UUID REFERENCES tenants(id);
ALTER TABLE call_tasks ADD COLUMN IF NOT EXISTS tenant_id UUID REFERENCES tenants(id);
ALTER TABLE calls ADD COLUMN IF NOT EXISTS tenant_id UUID REFERENCES tenants(id);
ALTER TABLE opportunities ADD COLUMN IF NOT EXISTS tenant_id UUID REFERENCES tenants(id);

-- Step 2: 已有数据归属默认租户
UPDATE contacts SET tenant_id = '00000000-0000-0000-0000-000000000000' WHERE tenant_id IS NULL;
UPDATE scenario_templates SET tenant_id = '00000000-0000-0000-0000-000000000000' WHERE tenant_id IS NULL;
UPDATE call_tasks SET tenant_id = '00000000-0000-0000-0000-000000000000' WHERE tenant_id IS NULL;
UPDATE calls SET tenant_id = '00000000-0000-0000-0000-000000000000' WHERE tenant_id IS NULL;
UPDATE opportunities SET tenant_id = '00000000-0000-0000-0000-000000000000' WHERE tenant_id IS NULL;

-- Step 3: 改 NOT NULL
ALTER TABLE contacts ALTER COLUMN tenant_id SET NOT NULL;
ALTER TABLE scenario_templates ALTER COLUMN tenant_id SET NOT NULL;
ALTER TABLE call_tasks ALTER COLUMN tenant_id SET NOT NULL;
ALTER TABLE calls ALTER COLUMN tenant_id SET NOT NULL;
ALTER TABLE opportunities ALTER COLUMN tenant_id SET NOT NULL;

-- ═══ 4. 更新索引 ═══

-- contacts: 同一租户内手机号唯一
DROP INDEX IF EXISTS idx_contacts_phone_hash;
CREATE UNIQUE INDEX idx_contacts_tenant_phone ON contacts (tenant_id, phone_hash);
CREATE INDEX idx_contacts_tenant_status ON contacts (tenant_id, current_status);

-- scenario_templates
CREATE INDEX idx_templates_tenant_status ON scenario_templates (tenant_id, status);

-- call_tasks
CREATE INDEX idx_tasks_tenant_status ON call_tasks (tenant_id, status);

-- calls
CREATE INDEX idx_calls_tenant_created ON calls (tenant_id, created_at);
DROP INDEX IF EXISTS uq_calls_contact_task;
CREATE UNIQUE INDEX uq_calls_tenant_contact_task
    ON calls (tenant_id, contact_id, task_id)
    WHERE status NOT IN ('failed', 'no_answer');

-- opportunities
CREATE INDEX idx_opportunities_tenant_score ON opportunities (tenant_id, score DESC);
```

**注意**：`dialogue_turns` 和 `call_events` 不加 `tenant_id` —— 通过 `call_id` FK 间接关联租户，加冗余列收益小、维护成本高。按租户查询时 JOIN `calls` 表即可。

### 4.3 三道防线

认证层取代了原设计中对 HTTP Header 的信任：

```
请求 → Auth Middleware → RequireTenant → Service 层 → Store 层
         │                    │              │            │
         │ JWT 验证            │ 拒绝未认证    │ 从 ctx     │ SQL 强制
         │ Claims → context   │              │ 取 tenant  │ WHERE tenant_id
         │ xtenant.WithTenantID│              │            │
```

1. **认证层**（`auth.Middleware`）：验证 JWT → 从 Claims 提取 `tenant_id` → 通过 `xtenant.WithTenantID` 注入 context。**tenant_id 来自系统验证的凭据，不是客户端自报**
2. **Service 层**：所有业务方法调用 `xtenant.RequireTenantID(ctx)` 获取 tenant_id，缺失则返回 error
3. **Store 层**：所有 SQL 查询强制 `WHERE tenant_id = $N`，联合唯一索引包含 `tenant_id`

```go
// Service 层示例
func (s *TemplateSvc) List(ctx context.Context, filter ListFilter) ([]model.ScenarioTemplate, error) {
    tenantID, err := xtenant.RequireTenantID(ctx)
    if err != nil {
        return nil, fmt.Errorf("获取租户 ID: %w", err)
    }
    return s.store.ListByTenant(ctx, tenantID, filter)
}

// Store 层示例
func (s *PgTemplateStore) ListByTenant(ctx context.Context, tenantID string, filter ListFilter) ([]model.ScenarioTemplate, error) {
    rows, err := s.pool.Query(ctx,
        `SELECT id, tenant_id, name, domain, ...
         FROM scenario_templates
         WHERE tenant_id = $1 AND status = $2
         ORDER BY updated_at DESC LIMIT $3 OFFSET $4`,
        tenantID, filter.Status, filter.Limit, filter.Offset,
    )
    // ...
}
```

### 4.4 ER 关系

```
TENANTS ──1:N──▶ API_KEYS
TENANTS ──1:N──▶ CONTACTS
TENANTS ──1:N──▶ SCENARIO_TEMPLATES
TENANTS ──1:N──▶ CALL_TASKS
TENANTS ──1:N──▶ CALLS
TENANTS ──1:N──▶ OPPORTUNITIES

（以下关系不变）
SCENARIO_TEMPLATES ──1:N──▶ TEMPLATE_SNAPSHOTS
SCENARIO_TEMPLATES ──1:N──▶ CALL_TASKS
TEMPLATE_SNAPSHOTS ──1:N──▶ CALLS
CONTACTS           ──1:N──▶ CALLS
CALL_TASKS         ──1:N──▶ CALLS
CALLS              ──1:N──▶ DIALOGUE_TURNS
CALLS              ──1:N──▶ CALL_EVENTS
CALLS              ──1:N──▶ OPPORTUNITIES
```

---

## 5. 配额控制

简化版：从 tenant 记录读配额，不使用 Redis 计数器。

### 5.1 检查时机

| 检查项 | API Server | Call Worker |
|--------|:---:|:---:|
| 租户状态 active | 换 token 时 | 拨号前 |
| 每日通话上限 | — | 拨号前 |
| 并发通话数 | — | 拨号前 |

### 5.2 实现

```go
// internal/call/worker.go

func (w *Worker) checkQuota(ctx context.Context, tenantID string) error {
    tenant, err := w.tenantCache.Get(ctx, tenantID)
    if err != nil {
        return fmt.Errorf("查询租户: %w", err)
    }
    if tenant.Status != "active" {
        return ErrTenantSuspended
    }

    // 每日通话数：查 calls 表当日记录数
    todayCount, err := w.callStore.CountToday(ctx, tenantID)
    if err != nil {
        return fmt.Errorf("查询当日通话数: %w", err)
    }
    if todayCount >= tenant.DailyCallLimit {
        return ErrDailyLimitExceeded
    }

    // 并发数：从 Worker 内存中的 activeCalls 按 tenant 分组计数
    if w.concurrentCalls(tenantID) >= tenant.MaxConcurrent {
        return ErrConcurrentLimitExceeded
    }

    return nil
}
```

**设计决策**：

- **每日通话数查 PostgreSQL** —— 当前并发 1-5 路、每天最多几百通话，`COUNT(*)` 性能完全够用
- **并发数从内存统计** —— Worker 已有 `activeCalls` map 跟踪活跃通话，按 tenant_id 分组计数，零额外开销
- **租户信息缓存** —— tenant 记录变更极低频（一个月改几次配额），用 `sync.Map` + TTL 5 分钟缓存，避免每次拨号都查 DB
- **不做 check-then-act 原子操作** —— 当前并发极低（单 Worker、1-5 路），竞态风险可忽略。未来并发提升时再引入 Redis INCR 原子计数

---

## 6. 三进程的租户传播

租户信息贯穿三个进程，通过任务 payload 和事件传递：

```
API Server                    Call Worker                   Post-Processor
    │                              │                              │
    │ JWT → context → tenant_id    │                              │
    │ ──→ Asynq payload ────────→  │ payload.TenantID             │
    │                              │ ──→ Redis Stream event ───→  │ event.TenantID
    │                              │                              │ → xtenant.WithTenantID
```

### 6.1 Asynq 任务 payload

```go
// internal/scheduler/task.go

type CallTaskPayload struct {
    TenantID  string `json:"tenant_id"` // 新增
    TaskID    int64  `json:"task_id"`
    ContactID int64  `json:"contact_id"`
    // ...
}
```

### 6.2 Redis Stream 事件

```go
// internal/postprocess/event.go

type CallCompletionEvent struct {
    TenantID  string // 新增
    CallID    int64
    ContactID int64
    TaskID    int64
    // ...
}
```

### 6.3 Post-Processor 还原租户上下文

```go
// internal/postprocess/worker.go

func (w *Worker) handleEvent(event CallCompletionEvent) error {
    ctx, _ := xtenant.WithTenantID(context.Background(), event.TenantID)
    return w.writer.WriteCallResult(ctx, event)
}
```

---

## 7. 配置变更

```toml
# clarion.toml 新增

[auth]
jwt_secret = "${CLARION_AUTH_JWT_SECRET}"   # JWT 签名密钥（必须通过环境变量注入）
token_ttl = "15m"                           # Token 有效期
```

---

## 8. 实施计划

分两个阶段，每阶段交付可独立验证的增量：

### Phase 1：认证 + 数据隔离

**目标**：所有 API 请求需认证，数据按 tenant_id 隔离。

- [ ] 数据库 migration 003：tenants 表、api_keys 表、业务表加 tenant_id
- [ ] 新增 `internal/auth/` 包：Issuer、Middleware、RequireTenant、Handler
- [ ] 新增 `internal/model/tenant.go`：Tenant、APIKey
- [ ] 新增 `internal/store/tenant_store.go`、`apikey_store.go`
- [ ] Token 端点：`POST /api/v1/auth/token`
- [ ] 现有 model 增加 `TenantID` 字段
- [ ] Service 层所有方法增加 `xtenant.RequireTenantID`
- [ ] Store 层所有 SQL 增加 `WHERE tenant_id = ?`
- [ ] 路由重构：路由分组 + 认证中间件
- [ ] CLI 工具：`admin create-tenant`、`admin create-apikey`
- [ ] Asynq payload 和 Redis Stream event 增加 tenant_id
- [ ] 测试：认证流程 + 多租户数据隔离

### Phase 2：配额控制

**目标**：Worker 拨号前检查租户配额。

- [ ] Worker 拨号前 quota 检查
- [ ] 租户信息 TTL 缓存
- [ ] CLI 工具：`admin set-quota`、`admin set-tenant-status`、`admin revoke-apikey`
- [ ] 测试：配额超限、租户暂停场景

---

## 9. 变更影响评估

| 组件 | 变更程度 | 说明 |
|------|:---:|------|
| `internal/auth/` | 新建 | JWT 签发/验证、认证中间件、Token handler |
| `internal/model/` | 中 | 新增 Tenant / APIKey，现有 model 加 TenantID |
| `internal/store/` | 大 | 所有 SQL 加 WHERE tenant_id，新增 tenant / apikey store |
| `internal/service/` | 中 | 所有方法加 RequireTenantID |
| `internal/api/` | 中 | 路由分组重构、新增 token handler |
| `internal/call/` | 小 | Worker 加配额检查、payload 加 tenant_id |
| `internal/postprocess/` | 小 | Event 加 tenant_id |
| `internal/scheduler/` | 小 | Payload 加 tenant_id |
| `internal/config/` | 小 | 新增 [auth] 配置段 |
| `cmd/clarion/` | 小 | 新增 admin 子命令 |
| `schema/` | 中 | 新增 003 migration |

### 风险点

1. **数据迁移**：migration 003 在一个事务中完成三步（加列 → 填充 → 设 NOT NULL），已有数据自动归属 `default` 租户
2. **测试更新**：所有现有测试需要增加 tenant context（工作量较大但机械化）
3. **JWT 密钥管理**：`jwt_secret` 必须通过环境变量注入，泄露即全部 token 可伪造

---

## 10. 演进路线

当前设计是最小可行的认证 + 多租户方案。以下是各维度的演进路径，每一步都建立在前一步的基础上，**不需要推翻重来**。

### 10.1 认证演进

```
当前                          ↓ 触发条件                         演进方案
─────────────────────────────────────────────────────────────────────────────
API Key + JWT (15min)         需要无感续期                       + Refresh Token
                              （前端长会话）                      （api_keys 表加 refresh_token_hash 列，
                                                                  新增 POST /auth/refresh 端点）

API Key + JWT + Refresh       租户需要对接企业微信/                + OAuth2 / OIDC Provider
                              钉钉等第三方登录                    （新增 internal/auth/oauth/ 包，
                                                                  JWT Claims 加 identity_source 字段）

                              租户内需要区分                      + JWT Claims 加 role 字段
                              管理员/操作员/只读角色               （RequireRole 中间件，
                                                                  api_keys 表加 role 列或新增 users 表）
```

**扩展点**：JWT Claims 结构体可直接加字段（`Role`、`Permissions`），不影响已签发的 token（缺失字段零值处理）。`auth.Middleware` 只负责验证和注入，角色检查由独立的 `RequireRole` 中间件承担。

### 10.2 租户管理演进

```
当前                          ↓ 触发条件                         演进方案
─────────────────────────────────────────────────────────────────────────────
CLI 管理                      非技术人员需要管理租户               + 管理端 HTTP API
                                                                  （store 层已有 CRUD，加 handler 暴露即可，
                                                                  JWT Claims 加 role=admin，RequireAdmin 中间件）

CLI / HTTP                    需要按套餐计费、                    + tenant_subscriptions 表
                              订阅续期/过期管理                   （从 tenant 表拆出配额字段到 subscription，
                                                                  tenant 表保留身份信息，subscription 管理生命周期）

                              需要 Web 管理界面                   + 前端 SPA
                                                                  （管理端 API 已就绪，加前端即可）
```

**扩展点**：`internal/store/tenant_store.go` 的 CRUD 方法可直接被 HTTP handler 调用。加 subscription 表时，tenant 表的 `daily_call_limit`、`max_concurrent` 字段迁移到 subscription，tenant 表只保留身份和状态。

### 10.3 配额演进

```
当前                          ↓ 触发条件                         演进方案
─────────────────────────────────────────────────────────────────────────────
DB COUNT + 内存并发计数        并发 > 20 路                       + Redis 原子计数器
                              或多 Worker 实例                    （INCR + EXPIRE，Lua 脚本保证原子
                                                                  check-and-increment，替代 DB COUNT）

Redis 计数器                  需要月度限额、联系人上限             + tenant 表或 subscription 表加配额列
                              模板数上限等                        （checkQuota 方法加检查项）

                              需要精确用量统计和账单               + 独立的 usage 表 + 定时聚合
                                                                  （按小时/天聚合通话数、时长、成本，
                                                                  为计费提供数据源）
```

**扩展点**：`checkQuota` 方法的签名 `(ctx, tenantID) → error` 不变。内部实现从 DB COUNT 切换到 Redis INCR 是纯内部重构，调用方无感知。

### 10.4 模板体系演进

```
当前                          ↓ 触发条件                         演进方案
─────────────────────────────────────────────────────────────────────────────
每个租户独立创建模板            积累了可复用行业模板                + 公共模板（tenant_id = '_platform'）
                                                                  （List 查询加 WHERE tenant_id IN ($1, '_platform')，
                                                                  公共模板对所有租户只读可见）

公共模板                      租户需要基于公共模板定制              + Fork API
                                                                  （复制公共模板到租户名下，
                                                                  新增 forked_from 字段追溯来源）
```

**扩展点**：`scenario_templates` 表已有 `tenant_id` 列。公共模板只是用一个保留值（`_platform`）标识，不需要表结构变更。

### 10.5 数据隔离演进

```
当前                          ↓ 触发条件                         演进方案
─────────────────────────────────────────────────────────────────────────────
共享表 + 应用层 WHERE          合规审计要求 DB 级隔离              + PostgreSQL RLS
                                                                  （CREATE POLICY ... USING (tenant_id = current_setting('app.tenant_id'))，
                                                                  应用层 SET app.tenant_id 后 RLS 自动过滤）

共享表 + RLS                  大客户要求物理隔离                   + Schema 隔离
                                                                  （每个租户一个 schema，连接时 SET search_path，
                                                                  tenant_id 列是迁移数据的前置条件）
```

**扩展点**：`tenant_id` 列是所有隔离方案升级的基础。从共享表升级到 RLS 或 Schema 隔离时，应用层的 `WHERE tenant_id` 逻辑可以保留作为双重保险。

### 10.6 演进原则

1. **每步增量** —— 每次演进是一个独立的 PR/migration，不需要大规模重构
2. **向后兼容** —— 新字段用零值/默认值处理，旧 token 不失效，旧数据不丢失
3. **触发驱动** —— 不预测需求，等触发条件真正出现时再实施
4. **基础不白做** —— 当前的 `tenant_id` 列、JWT Claims 结构、Store 接口都是后续演进的前置条件

---

## 附录

### A. 新增依赖

| 库 | 用途 |
|-----|------|
| `golang-jwt/jwt/v5` | JWT 签发与验证 |

### B. 代码索引

| 文件路径 | 状态 | 说明 |
|---------|:---:|------|
| `internal/auth/token.go` | 新建 | Claims 定义、Issuer（签发/验证） |
| `internal/auth/middleware.go` | 新建 | Middleware、RequireTenant |
| `internal/auth/context.go` | 新建 | WithClaims、ClaimsFromContext |
| `internal/auth/handler.go` | 新建 | HandleToken |
| `internal/model/tenant.go` | 新建 | Tenant、APIKey |
| `internal/store/tenant_store.go` | 新建 | TenantStore CRUD |
| `internal/store/apikey_store.go` | 新建 | APIKeyStore |
| `schema/003_multi_tenant.up.sql` | 新建 | 完整 migration |
| `internal/api/router.go` | 修改 | 路由分组 + 认证中间件 |
| `internal/model/*.go` | 修改 | 加 TenantID |
| `internal/store/*.go` | 修改 | SQL 加 tenant_id |
| `internal/service/*.go` | 修改 | 加 RequireTenantID |
| `internal/config/config.go` | 修改 | 加 [auth] 段 |
| `internal/scheduler/task.go` | 修改 | Payload 加 TenantID |
| `internal/postprocess/event.go` | 修改 | Event 加 TenantID |
