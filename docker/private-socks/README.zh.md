# 私有 SOCKS HTTP 代理容器

该镜像在 VPS 上提供一个带认证的标准 HTTP 代理，并将流量转发到 sing-box 的私有 `0x80` 或 `0x82` SOCKS 出站。

镜像仅包含程序和脱敏示例，不包含节点、账号或密码。

## 启动

Linux arm64 VPS 需要安装 Docker 与 Docker Compose 插件。

```bash
mkdir -p private-socks-http
cd private-socks-http
curl -LO https://github.com/ztyawc/sing-box/releases/download/private-socks-v2026.07.22-zty.1/config.example.json
curl -Lo docker-compose.yml https://github.com/ztyawc/sing-box/releases/download/private-socks-v2026.07.22-zty.1/docker-compose.yml
cp config.example.json config.json
```

编辑 `config.json`：

- 将 `server`、`server_port`、私有 SOCKS 用户名和密码替换为真实值。
- `private_auth_method` 只可设为 `0x80` 或 `0x82`。
- 私有 SOCKS 用户名必须恰好为 19 字节，密码不能为空。
- 将 HTTP 入站的 `CHANGE_ME_HTTP_USERNAME` 和 `CHANGE_ME_HTTP_PASSWORD` 换成强凭据。

保存后，将配置文件交给镜像内的非 root 用户并保持仅该用户可读：

```bash
chown 65532:65532 config.json
chmod 600 config.json
```

启动并查看日志：

```bash
docker compose up -d
docker compose logs -f
```

如果日志出现 `open /etc/sing-box/config.json: permission denied`，说明宿主机配置仍属于 `root:root`；重新执行上面的 `chown` 与 `chmod`，然后运行 `docker compose restart`。

如果出站日志出现 `dial tcp ...: operation not permitted`，请确认 `config.json` 的 `route` 中没有启用 `auto_detect_interface`。本容器已移除全部 Linux capabilities，而 Docker bridge 会自动选择 `eth0`，无需在核心中再次绑定接口：

```json
"route": {
  "final": "private-socks-out"
}
```

默认只监听 VPS 自身的 `127.0.0.1:8080`。同一台 VPS 上的软件可以这样使用：

```text
http://HTTP用户名:HTTP密码@127.0.0.1:8080
```

测试：

```bash
curl -x http://HTTP用户名:HTTP密码@127.0.0.1:8080 https://www.baidu.com/ -I
```

## 允许远程设备连接

仅在设置防火墙来源白名单后执行：

```bash
HTTP_BIND_ADDRESS=0.0.0.0 docker compose up -d
```

此时代理地址为：

```text
http://HTTP用户名:HTTP密码@VPS公网IP:8080
```

不要删除 HTTP 入站的 `users`，也不要向整个互联网无条件开放 8080 端口。

## 不使用 Compose

```bash
docker run -d \
  --name private-socks-http \
  --restart unless-stopped \
  --read-only \
  --security-opt no-new-privileges \
  --cap-drop ALL \
  -p 127.0.0.1:8080:8080 \
  -v "$PWD/config.json:/etc/sing-box/config.json:ro" \
  ghcr.io/ztyawc/sing-box-private-socks:v2026.07.22-zty.1
```
