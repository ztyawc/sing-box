# sing-box 私有 SOCKS 0x80/0x82 跨平台版

这是基于 `ztyawc/sing-box` 的定制构建，在标准 sing-box SOCKS 出站中增加私有 `0x80`、`0x82` 认证，同时保留未配置 `private_auth_method` 时的标准 SOCKS4/4a/5 行为。

发布包与镜像只包含程序、脱敏配置和教程，不包含真实服务器、用户名、密码或节点配置。

## 发布内容

当前版本：

```text
v2026.07.23-zty.2
```

独立内核：

| 文件 | 适用系统 |
| --- | --- |
| `sing-box-private-socks-linux-amd64.tar.gz` | Intel/AMD 64 位 Linux、绝大多数 x86 VPS |
| `sing-box-private-socks-linux-arm64.tar.gz` | ARM64/aarch64 Linux VPS、开发板 |
| `sing-box-private-socks-linux-armv7.tar.gz` | 32 位 ARMv7 Linux |
| `sing-box-private-socks-windows-amd64.zip` | Windows 10/11 x64 |
| `sing-box-private-socks-windows-arm64.zip` | Windows 11 on ARM |
| `sing-box-private-socks-darwin-amd64.tar.gz` | Intel Mac |
| `sing-box-private-socks-darwin-arm64.tar.gz` | Apple Silicon Mac |

Docker 镜像：

```text
ghcr.io/ztyawc/sing-box-private-socks:v2026.07.23-zty.2
```

同一个镜像标签支持 `linux/amd64` 和 `linux/arm64`。`latest` 会指向最新已验证版本；生产环境建议固定版本标签或镜像摘要。

Release：

https://github.com/ztyawc/sing-box/releases/tag/private-socks-v2026.07.23-zty.2

## 配置说明

示例会在 VPS 上提供一个带用户名和密码的标准 HTTP 代理，再通过私有 SOCKS 出站访问目标网站。

关键出站配置：

```json
{
  "type": "socks",
  "tag": "private-socks-out",
  "server": "PRIVATE_SOCKS_SERVER",
  "server_port": 10800,
  "version": "5",
  "username": "1234567890123456789",
  "password": "PRIVATE_SOCKS_PASSWORD",
  "private_auth_method": "0x80"
}
```

要求：

- `version` 必须为 `"5"`。
- `private_auth_method` 只接受 `"0x80"` 或 `"0x82"`。
- 私有 SOCKS 用户名必须恰好为 19 字节；纯数字或 ASCII 用户名的字节数等于字符数。
- 密码不能为空。
- 不要把真实节点、密码、配置或抓包提交到公开仓库。
- Docker 配置中的 `route` 不需要 `auto_detect_interface`；Docker bridge 会自动选择出口。

HTTP 入站的账号与私有 SOCKS 出站账号互相独立。客户端软件只填写 HTTP 入站账号，不需要知道私有 SOCKS 凭据。

## Docker Compose 快速启动

要求：

- Docker Engine 24 或更高版本。
- Docker Compose v2，即 `docker compose` 命令。
- Linux amd64 或 arm64。

下载发布文件：

```bash
mkdir -p ~/private-socks-http
cd ~/private-socks-http

curl -LO https://github.com/ztyawc/sing-box/releases/download/private-socks-v2026.07.23-zty.2/config.example.json
curl -Lo docker-compose.yml https://github.com/ztyawc/sing-box/releases/download/private-socks-v2026.07.23-zty.2/docker-compose.yml
curl -Lo .env https://github.com/ztyawc/sing-box/releases/download/private-socks-v2026.07.23-zty.2/env.example

cp config.example.json config.json
```

编辑 `config.json`：

```bash
nano config.json
```

必须替换：

- 私有 SOCKS 的 `server`、`server_port`、`username`、`password` 和 `private_auth_method`。
- HTTP 入站的 `CHANGE_ME_HTTP_USERNAME` 与 `CHANGE_ME_HTTP_PASSWORD`。

保存后设置文件属主和权限。镜像使用非 root 用户 `UID/GID 65532`：

```bash
chown 65532:65532 config.json
chmod 600 config.json
```

检查 Compose 最终配置并启动：

```bash
docker compose config
docker compose pull
docker compose up -d
docker compose ps
docker compose logs --tail=50
```

正常日志包含：

```text
tcp server started at 0.0.0.0:8080
sing-box started
```

默认映射关系：

```text
VPS 127.0.0.1:8080  →  容器 0.0.0.0:8080
```

本机程序使用：

```text
http://HTTP用户名:HTTP密码@127.0.0.1:8080
```

### 修改宿主机端口

容器内部始终监听 `8080`，只需要修改 `.env` 中的宿主机端口。例如宿主机使用 `8790`：

```dotenv
HTTP_BIND_ADDRESS=127.0.0.1
HTTP_PORT=8790
```

应用新端口映射：

```bash
docker compose up -d --force-recreate
docker compose ps
```

映射将变为：

```text
VPS 127.0.0.1:8790  →  容器 0.0.0.0:8080
```

不要为了修改宿主机端口而改动 `config.json` 中的 `listen_port`。

### 测试代理

建议使用 `--proxy-user`，避免用户名或密码中的特殊字符破坏 URL：

```bash
curl -x http://127.0.0.1:8790 \
  --proxy-user 'HTTP用户名:HTTP密码' \
  -o /dev/null -sS --max-time 20 \
  -w '状态码=%{http_code} 首包=%{time_starttransfer}s 总耗时=%{time_total}s\n' \
  https://www.baidu.com/
```

查看经过私有 SOCKS 后的出口 IP：

```bash
curl -x http://127.0.0.1:8790 \
  --proxy-user 'HTTP用户名:HTTP密码' \
  --max-time 20 \
  https://myip.ipip.net
```

### 允许远程设备访问

先在 VPS 防火墙中仅放行可信来源 IP，再把 `.env` 改为：

```dotenv
HTTP_BIND_ADDRESS=0.0.0.0
HTTP_PORT=8790
```

UFW 白名单示例：

```bash
ufw allow from YOUR_TRUSTED_PUBLIC_IP to any port 8790 proto tcp
```

重新创建容器：

```bash
docker compose up -d --force-recreate
```

远程软件填写：

```text
类型：HTTP
服务器：VPS 公网 IP
端口：8790
用户名：HTTP 入站用户名
密码：HTTP 入站密码
```

不要无条件向整个互联网开放 HTTP 代理端口。

### 更新 Docker 镜像

固定版本升级时，先修改 `docker-compose.yml` 中的镜像标签，然后执行：

```bash
docker compose pull
docker compose up -d --force-recreate
docker image prune
```

`config.json` 是宿主机挂载文件，正常升级不会删除配置。

## 不使用 Compose

假设当前目录已经准备好 `config.json`：

```bash
docker run -d \
  --name private-socks-http \
  --restart unless-stopped \
  --read-only \
  --security-opt no-new-privileges \
  --cap-drop ALL \
  --tmpfs /tmp:size=16m,mode=1777 \
  -p 127.0.0.1:8790:8080 \
  -v "$PWD/config.json:/etc/sing-box/config.json:ro" \
  ghcr.io/ztyawc/sing-box-private-socks:v2026.07.23-zty.2
```

## 独立内核使用

### Linux

查看架构：

```bash
uname -m
```

对应关系：

- `x86_64`：下载 `linux-amd64`。
- `aarch64` 或 `arm64`：下载 `linux-arm64`。
- `armv7l`：下载 `linux-armv7`。

以 Linux amd64 为例：

```bash
curl -LO https://github.com/ztyawc/sing-box/releases/download/private-socks-v2026.07.23-zty.2/sing-box-private-socks-linux-amd64.tar.gz
tar -xzf sing-box-private-socks-linux-amd64.tar.gz
cd sing-box-private-socks-linux-amd64

cp config.example.json config.json
nano config.json

./sing-box check -c config.json
./sing-box run -c config.json
```

安装到系统路径：

```bash
sudo install -m 0755 sing-box /usr/local/bin/sing-box-private
sudo install -d -m 0750 /etc/sing-box-private
sudo install -m 0600 config.json /etc/sing-box-private/config.json

sudo /usr/local/bin/sing-box-private check -c /etc/sing-box-private/config.json
sudo /usr/local/bin/sing-box-private run -c /etc/sing-box-private/config.json
```

### Windows

下载与系统匹配的 ZIP，解压后打开 PowerShell：

```powershell
Copy-Item .\config.example.json .\config.json
notepad .\config.json

.\sing-box.exe check -c .\config.json
.\sing-box.exe run -c .\config.json
```

HTTP 代理监听端口需要通过 Windows Defender 防火墙放行时，只允许可信来源。使用 TUN 等需要系统权限的功能时，以管理员身份运行；仅使用 HTTP 入站通常不需要管理员权限。

### macOS

下载与 CPU 匹配的 tar.gz 并解压。若系统提示文件来自身份不明开发者，可仅对这个已校验文件移除下载隔离标记：

```bash
xattr -d com.apple.quarantine ./sing-box
chmod 0755 ./sing-box

cp config.example.json config.json
./sing-box check -c config.json
./sing-box run -c config.json
```

本发布的 macOS 文件是命令行内核，不是 iOS IPA，也不是带图形界面的 macOS App。

## 校验下载文件

Release 同时提供每个平台压缩包的 `.sha256` 文件、统一的 `SHA256SUMS` 和 `BUILD-MANIFEST.txt`。

Linux/macOS：

```bash
sha256sum -c sing-box-private-socks-linux-amd64.tar.gz.sha256
```

Windows PowerShell：

```powershell
Get-FileHash .\sing-box-private-socks-windows-amd64.zip -Algorithm SHA256
```

Docker 查看多架构清单：

```bash
docker buildx imagetools inspect ghcr.io/ztyawc/sing-box-private-socks:v2026.07.23-zty.2
```

## 常见问题

### `config.json: permission denied`

宿主机配置属于 `root:root` 且权限是 `600`，镜像内的 `65532` 用户无法读取：

```bash
chown 65532:65532 config.json
chmod 600 config.json
docker compose restart
```

### `dial tcp ...: operation not permitted`

受限容器不允许核心强制绑定网卡。删除 `route.auto_detect_interface`，保留：

```json
"route": {
  "final": "private-socks-out"
}
```

不建议通过增加 `NET_ADMIN` 或移除全部安全限制来绕过。

### `Connection refused`

先检查：

```bash
docker compose ps
docker compose logs --tail=100
docker compose port private-socks-http 8080
```

日志中的 `0.0.0.0:8080` 是容器内部端口。实际连接端口以 `docker compose ps` 显示的宿主机映射为准。

### `port is already allocated` 或 `address already in use`

修改 `.env` 的 `HTTP_PORT`，不要修改容器内部 `listen_port`：

```dotenv
HTTP_PORT=8790
```

然后执行：

```bash
docker compose up -d --force-recreate
```

### curl 返回 `SSL_ERROR_SYSCALL`

这通常是表面错误，真正原因在容器日志中：

```bash
docker compose logs --since=5m
```

重点检查私有 SOCKS 服务器、端口、19 字节用户名、密码、认证类型，以及 `authentication failed`、`connection rejected`、`EOF` 等信息。

### `exec format error`

下载或镜像架构不匹配。运行：

```bash
uname -m
docker image inspect ghcr.io/ztyawc/sing-box-private-socks:v2026.07.23-zty.2 --format '{{.Architecture}}'
```

然后选择正确的平台文件，或让 Docker 自动拉取对应的 amd64/arm64 镜像。

## 安全建议

- HTTP 入站必须设置强用户名和强密码。
- 默认监听 `127.0.0.1`；只有确有远程访问需求时才监听 `0.0.0.0`。
- 远程访问必须设置防火墙来源白名单。
- 配置文件保持 `600`，不要上传到网盘、日志或公开仓库。
- 日志排障时先检查是否包含服务器地址、用户名或其他敏感信息。
- 生产环境固定版本标签或镜像摘要，并在升级前备份 `config.json`。
