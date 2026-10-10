# 公司三节点部署

本文是广州这三台云服务器的部署步骤，按顺序做即可。集群行为对齐[官方集群部署](https://docs.newapi.ai/zh/docs/installation/deployment-methods/cluster-deployment)，镜像使用本仓库改过的代码，不使用官方镜像 `calciumion/new-api`。

用户只访问主节点公网地址 `http://8.163.16.200`。从节点和数据机的公网地址不给用户。

## 机器

| 角色 | 规格 | 内网 IP | 公网 IP | 跑什么 |
| --- | --- | --- | --- | --- |
| 数据机 | 4 核 16G | 172.18.238.60 | 8.138.216.222 | PostgreSQL 16、Redis 7 |
| 主节点 | 4 核 8G，100Mbps | 172.18.238.61 | 8.163.16.200 | New API 主节点、Nginx |
| 从节点 | 4 核 8G，100Mbps | 172.18.238.59 | 8.138.254.123 | New API 从节点 |

三台在同一个专有网络。应用访问数据库和 Redis 用内网 IP。登录用控制台里的「远程连接」，不走 SSH。

## 部署顺序

镜像已经打好，文件在本机桌面：`~/Desktop/new-api-20261008.tar.gz`。下面按这个顺序做，不要三台一起开配。

| 顺序 | 在哪做 | 做什么 | 做完的标志 |
| --- | --- | --- | --- |
| 1 | 云控制台安全组 | 按第 1 节收口端口 | 数据库端口只对两台应用的内网 IP 开放，3000 不对公网开放 |
| 2 | 三台都做 | 远程连接进去，安装 Docker | 三台都能执行 `docker compose version` |
| 3 | 只在数据机 | 启动 PostgreSQL 和 Redis，生成三个密钥并抄下来 | `docker compose ps` 里两个容器都是 running |
| 4 | 主节点、从节点 | 上传桌面上的压缩包，执行 `docker load` | 两台 `docker images` 都能看到 `new-api:20261008` |
| 5 | 只在主节点 | 写配置并启动。这一步会建表 | 日志有 `database migration started`，`docker compose ps` 为 healthy |
| 6 | 只在从节点 | 主节点健康之后再写配置并启动 | 日志里是从节点，健康检查通过 |
| 7 | 只在主节点 | 安装 Nginx，把流量分到两台应用 | 本机 `curl http://127.0.0.1/api/status` 里有 `"success":true` |
| 8 | 浏览器 | 打开 `http://8.163.16.200`，按向导创建管理员 | 能登录管理后台 |
| 9 | 后台和数据机 | 日志保留设成 30 到 90 天，数据机加上每天备份 | 备份目录里有当天的压缩包 |

从节点不会建表。主节点还没健康就启动从节点，库是空的。有域名之后再做第 9 节的 HTTPS，现在不要加 `SESSION_COOKIE_SECURE`。

## 主从实际做什么

两台应用都转发对话、生图和文件请求，也都会扣费、写日志。Nginx 做整站转发，不按读、写拆路径。

差别只有这些：

- 只有主节点建表、改表。
- 只有主节点跑定时任务：渠道同步、异步任务轮询、日志清理。
- 从节点必须设置 `NODE_TYPE=slave`。主节点不要设置 `NODE_TYPE`，不设置就是主节点。
- 异步生图、视频要等主节点轮询上游。主节点停机时，新的对话转发仍可由从节点承担，这类后台任务会等主节点恢复。

两台必须满足：

- `SQL_DSN` 指向同一个库。
- `REDIS_CONN_STRING` 指向同一个 Redis。各用各的 Redis 时，限流会按节点分开计算。
- `SESSION_SECRET` 相同。
- `CRYPTO_SECRET` 相同。共享 Redis 时两项都要设，不能只设会话密钥。
- 不要设置 `FRONTEND_BASE_URL`。两台镜像里都有前端页面，设了之后从节点会把页面请求重定向走。
- 不要设置 `MEMORY_CACHE_ENABLED`。配了 Redis 后程序会自己打开内存缓存。

## 1. 安全组

这是部署顺序的第 1 步，在装软件之前做。三台机器靠内网 IP 通信，放行在阿里云安全组里做，不用在机器之间配 SSH。

安全组规则挂在安全组上，组里的每台机器都会套用同一套规则。三台如果还在购买时的同一个安全组里，给数据机放开的 5432 也会出现在另外两台上。所以拆成三个安全组，每台只保留自己需要的入方向。

### 先看现在是不是同一个组

1. 打开 [ECS 控制台](https://ecs.console.aliyun.com/) 的实例列表，地域选华南 3（广州）。
2. 点进一台实例，打开「安全组」页，记下安全组 ID。
3. 另外两台同样看一眼。三个 ID 一样，就是同一组，按下面拆开。已经是三个不同的组，就只给各组加规则。

### 拆成三个安全组

1. 左侧进入「网络与安全」→「安全组」→「创建安全组」。
2. 网络类型选专有网络，专有网络选这三台正在用的那个 VPC。一组建一个，名称分别用 `new-api-data`、`new-api-master`、`new-api-slave`。
3. 创建时如果向导自动加了「全部协议、授权对象 `0.0.0.0/0`」的入方向，先别用。规则留空，创建后再手动加。
4. 回到实例列表，点进对应实例，在「安全组」页选「更换安全组」或「加入安全组」。数据机只留 `new-api-data`，主节点只留 `new-api-master`，从节点只留 `new-api-slave`。换完后从旧组里移除，一台不要同时挂两个组。

出方向保持默认的「全部允许」。删掉出方向放行后，机器访问不了上游模型，也拉不了 Docker 镜像。

入方向每一条都这样填：

- 授权策略：允许
- 优先级：1
- 协议：自定义 TCP
- 授权对象填单个 IP 时必须带 `/32`，例如 `172.18.238.61/32`

22 端口留给控制台「远程连接」。购买时如果已经对 `0.0.0.0/0` 开放了 22，先留着，否则网页远程连接会登不上去。不要为了收紧 22 把这条删掉。

### 数据机 `new-api-data`

实例是 `172.18.238.60`。入方向加这 4 条：

| 端口 | 授权对象 | 用途 |
| --- | --- | --- |
| `5432/5432` | `172.18.238.61/32` | 主节点连 PostgreSQL |
| `5432/5432` | `172.18.238.59/32` | 从节点连 PostgreSQL |
| `6379/6379` | `172.18.238.61/32` | 主节点连 Redis |
| `6379/6379` | `172.18.238.59/32` | 从节点连 Redis |

不要加授权对象为 `0.0.0.0/0` 的 5432 或 6379。公网不访问数据库。

### 主节点 `new-api-master`

实例是 `172.18.238.61`。入方向加这 2 条：

| 端口 | 授权对象 | 用途 |
| --- | --- | --- |
| `80/80` | `0.0.0.0/0` | 用户访问网页和接口 |
| `443/443` | `0.0.0.0/0` | 以后上 HTTPS 时用 |

不要开放 3000。应用只监听本机 `127.0.0.1:3000`，由这台机器上的 Nginx 转发。

### 从节点 `new-api-slave`

实例是 `172.18.238.59`。入方向加这 1 条：

| 端口 | 授权对象 | 用途 |
| --- | --- | --- |
| `3000/3000` | `172.18.238.61/32` | 主节点 Nginx 把请求转发过来 |

不要对公网开放 80、443、3000。用户不直接访问这台机器。

### 加完后核对

每台实例的「安全组」页只挂一个组。入方向里没有「协议全部、授权对象 `0.0.0.0/0`」这种一揽子放行。有的话删掉，否则上面的端口限制不起作用。删之前确认 22 和主节点的 80 已经单独加好。

## 2. 三台都安装 Docker

这是部署顺序的第 2 步。三台都用控制台「远程连接」进去，用 root 操作。Alibaba Cloud Linux 用第一组，Ubuntu 用第二组。

```bash
dnf install -y docker
systemctl enable --now docker
docker compose version || dnf install -y docker-compose-plugin
```

```bash
apt-get update
apt-get install -y docker.io docker-compose-v2
systemctl enable --now docker
```

装完执行 `docker compose version`，能打出版本再继续。

限制容器日志体积。这一步放在启动数据库之前：

```bash
mkdir -p /etc/docker
cat >/etc/docker/daemon.json <<'EOF'
{
  "log-driver": "json-file",
  "log-opts": {
    "max-size": "50m",
    "max-file": "3"
  }
}
EOF
systemctl restart docker
```

## 3. 数据机

这是部署顺序的第 3 步。只在 `172.18.238.60` 上做，两台应用先不要启动。

先看有没有独立数据盘：

```bash
lsblk
```

有未挂载、且确认没有数据的盘，再格式化并挂到 `/data`，把下面的卷路径从 `/opt/new-api-data` 改成 `/data`。看不准就先用系统盘目录，不要格式化。

```bash
mkdir -p /opt/new-api /opt/new-api-data/postgres /opt/new-api-data/redis /opt/new-api-data/backup
cd /opt/new-api
openssl rand -hex 24 > /tmp/db-password
openssl rand -hex 32 > /tmp/session-secret
openssl rand -hex 32 > /tmp/crypto-secret
chmod 600 /tmp/db-password /tmp/session-secret /tmp/crypto-secret
```

把这三个值抄到密码管理器。数据库密码只含十六进制字符，可以直接放进连接串。三个值后面两台应用都要用，数据机重启后 `/tmp` 可能清空，所以必须另存一份。

`/opt/new-api/.env`：

```bash
cat >/opt/new-api/.env <<EOF
POSTGRES_USER=newapi
POSTGRES_PASSWORD=$(cat /tmp/db-password)
POSTGRES_DB=new-api
EOF
chmod 600 /opt/new-api/.env
```

`/opt/new-api/docker-compose.yml`：

```yaml
services:
  postgres:
    image: postgres:16
    container_name: postgres
    restart: always
    environment:
      POSTGRES_USER: ${POSTGRES_USER}
      POSTGRES_PASSWORD: ${POSTGRES_PASSWORD}
      POSTGRES_DB: ${POSTGRES_DB}
    command: >
      postgres
      -c shared_buffers=4GB
      -c max_connections=200
    volumes:
      - /opt/new-api-data/postgres:/var/lib/postgresql/data
    ports:
      - "172.18.238.60:5432:5432"

  redis:
    image: redis:7
    container_name: redis
    restart: always
    command: >
      redis-server
      --requirepass ${POSTGRES_PASSWORD}
      --appendonly yes
      --maxmemory 1gb
      --maxmemory-policy allkeys-lru
    volumes:
      - /opt/new-api-data/redis:/data
    ports:
      - "172.18.238.60:6379:6379"
```

```bash
docker compose up -d
docker compose ps
```

两个容器都是 running 再往下。端口绑在内网 IP 上，公网扫不到。

## 4. 上传已经打好的镜像

这一步在数据机的数据库起来之后做，对应部署顺序第 4 步。镜像文件已经在本机桌面，不用重新构建。代码以后有改动，按第 10 节重新打包。

用云服务器控制台的文件上传，或网盘，把 `~/Desktop/new-api-20261008.tar.gz` 传到主节点和从节点，放到 `/opt/new-api/`。两台都执行：

```bash
gzip -dc /opt/new-api/new-api-20261008.tar.gz | docker load
docker images
```

都能看到 `new-api:20261008` 后再删掉压缩包，避免占磁盘。

## 5. 主节点

这是部署顺序的第 5 步。镜像已经在这台机器上 `docker load` 之后再做。登录 `172.18.238.61`。数据库密码、`SESSION_SECRET`、`CRYPTO_SECRET` 用第 3 步保存的那三个值。

```bash
mkdir -p /opt/new-api/data /opt/new-api/logs
cd /opt/new-api
```

`/opt/new-api/.env`：

```text
SQL_DSN=postgresql://newapi:这里换数据库密码@172.18.238.60:5432/new-api?sslmode=disable
REDIS_CONN_STRING=redis://default:这里换数据库密码@172.18.238.60:6379
SESSION_SECRET=这里换会话密钥
CRYPTO_SECRET=这里换加密密钥
TZ=Asia/Shanghai
SYNC_FREQUENCY=60
BATCH_UPDATE_ENABLED=true
BATCH_UPDATE_INTERVAL=5
NODE_NAME=app-master
ERROR_LOG_ENABLED=true
GLOBAL_API_RATE_LIMIT_ENABLE=false
GLOBAL_WEB_RATE_LIMIT_ENABLE=false
CRITICAL_RATE_LIMIT_ENABLE=false
```

这台 Postgres 容器没有证书，所以连接串带 `sslmode=disable`。官方示例没写这项，不加有可能连不上。

上面最后 3 行关掉按 IP 计数的三道限流。这三道都是「同一个公网 IP 一共多少次」，不是「每个账号多少次」。200 个员工如果从公司网络出来，往往共用一个出口 IP，再乘上两台共用一个 Redis，额度是全公司一份。登录状态大约每 15 分钟刷新一次，也算进登录那一道，所以 200 人同时挂着页面就会顶满。网页和后台接口同理。模型调用走 `/v1`，不在这三道里面，账号额度仍然有效。从节点这 3 行也要写成一样。登录页在公网上，关掉之后同一 IP 可以连续试密码，员工账号要用够长的密码。

`/opt/new-api/docker-compose.yml`：

```yaml
services:
  new-api:
    image: new-api:20261008
    container_name: new-api-master
    restart: always
    command: --log-dir /app/logs
    env_file: .env
    ports:
      - "127.0.0.1:3000:3000"
    volumes:
      - ./data:/data
      - ./logs:/app/logs
    healthcheck:
      test: ["CMD-SHELL", "wget -q -O - http://localhost:3000/api/status | grep -o '\"success\":\\s*true' || exit 1"]
      interval: 30s
      timeout: 10s
      retries: 3
```

先确认数据机能通，再启动主节点：

```bash
docker run --rm redis:7 redis-cli -h 172.18.238.60 -a "$(sed -n 's/^REDIS_CONN_STRING=redis:\/\/default:\([^@]*\)@.*/\1/p' .env)" ping
docker compose up -d
docker compose logs -f new-api
```

`redis-cli` 应返回 `PONG`。应用日志里要出现 `database migration started`，并且 Redis 已连接。`docker compose ps` 变为 healthy 后，按 `Ctrl-C` 退出日志，不要停容器。

从节点不会建表。主节点还没健康就启动从节点，库是空的。

## 6. 从节点

这是部署顺序的第 6 步。主节点已经 healthy 之后，再登录 `172.18.238.59`。

```bash
mkdir -p /opt/new-api/data /opt/new-api/logs
cd /opt/new-api
```

`.env` 与主节点相同，只改最后两行，并加上节点类型：

```text
NODE_TYPE=slave
NODE_NAME=app-slave
```

`SQL_DSN`、`REDIS_CONN_STRING`、`SESSION_SECRET`、`CRYPTO_SECRET` 必须和主节点完全一致。关掉限流的那 3 行也要相同，否则请求打到哪台就按哪台的开关算。

`docker-compose.yml` 与主节点相同，只改容器名和端口：

```yaml
services:
  new-api:
    image: new-api:20261008
    container_name: new-api-slave
    restart: always
    command: --log-dir /app/logs
    env_file: .env
    ports:
      - "172.18.238.59:3000:3000"
    volumes:
      - ./data:/data
      - ./logs:/app/logs
    healthcheck:
      test: ["CMD-SHELL", "wget -q -O - http://localhost:3000/api/status | grep -o '\"success\":\\s*true' || exit 1"]
      interval: 30s
      timeout: 10s
      retries: 3
```

```bash
docker compose up -d
docker compose logs -f new-api
```

日志里节点应为从节点，并且没有去跑数据库迁移。健康检查通过后再配入口。

## 7. 主节点上的 Nginx

这是部署顺序的第 7 步。两台应用都健康之后，只在 `172.18.238.61` 安装。对话是流式输出，默认 60 秒会断开，所以关掉缓冲并加长超时。Nginx 和主节点在同一台机器上，上游写 `127.0.0.1`，不写主节点公网 IP。

```bash
dnf install -y nginx || apt-get install -y nginx
```

`/etc/nginx/conf.d/new-api.conf`：

```nginx
upstream new_api_cluster {
    server 127.0.0.1:3000 weight=3;
    server 172.18.238.59:3000 weight=5;
}

server {
    listen 80;
    server_name _;
    client_max_body_size 100m;

    location / {
        proxy_pass http://new_api_cluster;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_buffering off;
        proxy_read_timeout 3600s;
        proxy_send_timeout 3600s;
        gzip off;
    }
}
```

权重 3 和 5 与官方示例一致，从节点接更多转发，主节点给定时任务留余量。

```bash
nginx -t
systemctl enable --now nginx
curl -s http://127.0.0.1/api/status
```

返回里有 `"success":true` 后，浏览器打开 `http://8.163.16.200`，按初始化向导创建管理员。

向导做完后，主节点和从节点都要重启一次。程序只在启动时读一次数据库里的初始化记录，结果记在这台进程的内存里。提交向导时，Nginx 只会把这次请求交给其中一台，只有那一台会变成「已初始化」。另一台仍是启动时的「未初始化」。刷新页面时 `/api/setup` 再被分到那一台，前端就会打开 `/setup`。

两台都执行：

```bash
cd /opt/new-api
docker compose restart
```

重启后分别确认，两边都要有 `"status":true`：

```bash
# 在主节点上
curl -s http://127.0.0.1:3000/api/setup
# 在从节点上
curl -s http://127.0.0.1:3000/api/setup
```

## 8. 上线后马上做的两件事

在管理后台打开系统设置里的「日志维护」，消耗日志保留 30 到 90 天。数据库和系统盘在一起时，日志不清理会把磁盘写满。

在数据机加每天备份。`/opt/new-api/backup.sh`：

```bash
#!/bin/bash
set -euo pipefail
mkdir -p /opt/new-api-data/backup
docker exec postgres pg_dump -U newapi -d new-api \
  | gzip > "/opt/new-api-data/backup/new-api-$(date +%F).sql.gz"
find /opt/new-api-data/backup -name 'new-api-*.sql.gz' -mtime +14 -delete
```

```bash
chmod 700 /opt/new-api/backup.sh
crontab -e
```

加入一行，每天 3 点备份，保留 14 天：

```text
0 3 * * * /opt/new-api/backup.sh
```

先手动跑一次 `/opt/new-api/backup.sh`，确认 `/opt/new-api-data/backup` 里有当天的压缩包。

## 9. 有域名之后再开 HTTPS

证书放到主节点后，在 Nginx 里增加 443，并把 80 跳转到 HTTPS。然后两台应用的 `.env` 同时加上：

```text
SESSION_COOKIE_SECURE=true
SESSION_COOKIE_TRUSTED_URL=https://你的域名
```

`SESSION_COOKIE_TRUSTED_URL` 写精确的 HTTPS 地址，不要带路径，不要用通配符。两台都改完后分别执行：

```bash
cd /opt/new-api
docker compose up -d
```

在 HTTPS 配好之前不要加这两项，否则 HTTP 下登录会失败。

## 10. 以后更新代码

官方的滚动顺序是先主后从。只有主节点会迁移数据库。

在自己电脑上重新构建，标签换一个新日期：

```bash
docker buildx build --platform linux/amd64 -t new-api:新标签 --load .
docker save new-api:新标签 | gzip -1 > ~/Desktop/new-api-新标签.tar.gz
```

把新的压缩包用控制台或网盘上传到两台应用机，再执行 `gzip -dc 压缩包路径 | docker load`。

先改主节点 `docker-compose.yml` 里的镜像标签，执行 `docker compose up -d`。日志里迁移完成且健康检查通过后，再同样更新从节点。

## 排查

| 现象 | 处理 |
| --- | --- |
| 容器报 `exec format error` | 镜像不是 `linux/amd64`。加上 `--platform linux/amd64` 重新构建 |
| 主节点日志没有 `database migration started` | 主节点 `.env` 里写成了 `NODE_TYPE=slave`。主节点删掉这项后重建容器 |
| 从节点启动报缺表 | 主节点还没迁移完。等主节点 healthy 后重启从节点 |
| Redis ping 失败，或应用起不来并提示 Redis | 安全组没放行，或密码不一致。从主节点访问 `172.18.238.60:6379` |
| 数据库连接报 SSL | 确认 `SQL_DSN` 末尾有 `?sslmode=disable` |
| 登录后刷新又掉线，或两台表现不一致 | 两台的 `SESSION_SECRET`、`CRYPTO_SECRET` 不一致。改成相同值后重启两台 |
| 对话生成到一半断开 | Nginx 没关缓冲，或超时仍是 60 秒。对照第 7 步配置 |
| 页面 301 到另一台地址 | 从节点设了 `FRONTEND_BASE_URL`。删掉后重启从节点 |
| 已经初始化过，刷新有时又进 `/setup` | 完成向导后没有重启两台。两台都 `docker compose restart`，再确认两边 `/api/setup` 都是 `"status":true` |
| 登录提示 `Request failed with status code 429` | 同一出口 IP 超过了按 IP 计数的限流。按第 5 步在两台 `.env` 关掉那 3 个开关后重建容器。如果 `.env` 里还留着以前的 `GLOBAL_API_RATE_LIMIT` 这类数字，删掉，只保留 `ENABLE=false`。关掉之后旧计数不再拦截 |

公司这套部署关掉三道按 IP 的限流，避免 200 个员工共用一个出口时互相挤兑。模型调用的账号额度不受影响。开关生效后，Redis 里的旧计数不再拦截。还是 429，多半是容器没读到新配置，用 `docker compose up -d` 重建，不要只 `restart`。还没改配置、只想先登进去时，可以在数据机清一次计数：

```bash
cd /opt/new-api
docker exec redis redis-cli -a "$(grep '^POSTGRES_PASSWORD=' .env | cut -d= -f2-)" EVAL "local keys = redis.call('KEYS', 'rateLimit:v2:*'); local n = 0; for _, k in ipairs(keys) do redis.call('DEL', k); n = n + 1 end; return n" 0
```

删完再打开登录页，点一次即可。
