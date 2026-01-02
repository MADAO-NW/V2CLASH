# Link2Clash

最简 VLESS/VMESS -> Clash 转换器。

## 本地开发（Windows）

使用 PowerShell 在 Windows 上构建并运行：

```powershell
cd F:\Desktop\v2clash
New-Item -ItemType Directory -Force dist
go build -o dist\link2clash.exe
New-Item -ItemType Directory -Force dist\static
Copy-Item .\static\* .\dist\static\ -Recurse -Force
.\dist\link2clash.exe
```

打开 `http://127.0.0.1:7625/`。

## 一键部署（GitHub，一条命令）

说明：脚本会从 GitHub Releases 下载预编译二进制，不需要本地构建，也不需要在服务器安装 Go。

### 发布二进制要求

在 GitHub Releases 中准备以下文件名（Linux）：

- `link2clash_linux_amd64`
- `link2clash_linux_arm64`

### HTTP（无域名）

```bash
curl -fsSL https://raw.githubusercontent.com/OWNER/REPO/main/deploy.sh \
  | env REPO=OWNER/REPO ENABLE_TLS=0 AUTO_REMOVE_DEFAULT=1 bash
```

### HTTPS（有域名）

```bash
curl -fsSL https://raw.githubusercontent.com/OWNER/REPO/main/deploy.sh \
  | env REPO=OWNER/REPO DOMAIN=yourdomain.com ENABLE_TLS=1 AUTO_REMOVE_DEFAULT=1 bash
```

常用可选参数（环境变量）：

- `VERSION`：默认 `latest`，也可指定 tag。
- `DOWNLOAD_URL`：直接指定二进制下载地址（覆盖自动拼接）。
- `APP_DIR`：默认 `/opt/link2clash`。
- `APP_USER`：默认 `www-data`。
- `STATIC_REF`：静态文件来源的 git ref，默认自动使用 `main` 或 `VERSION`。

脚本默认适配 Ubuntu/Debian（使用 `apt-get`）。

## 部署（Windows 编译 + Linux VPS）

检查 VPS 架构：

```bash
uname -m
```

### 1) Windows 上交叉编译 Linux 二进制（PowerShell）

```powershell
cd F:\Desktop\v2clash
$env:GOOS="linux"
$env:GOARCH="amd64"   # VPS 是 aarch64 时改为 arm64
$env:CGO_ENABLED="0"

New-Item -ItemType Directory -Force dist
go build -o dist\link2clash
New-Item -ItemType Directory -Force dist\static
Copy-Item .\static\* .\dist\static\ -Recurse -Force

Remove-Item Env:GOOS
Remove-Item Env:GOARCH
Remove-Item Env:CGO_ENABLED
```

上传 `dist/` 到 VPS，例如：

```powershell
scp -r .\dist\* user@your_vps_ip:/tmp/link2clash/
```

### 2) VPS 安装依赖（Ubuntu/Debian）

```bash
sudo apt update
sudo apt install -y nginx certbot python3-certbot-nginx
sudo systemctl enable --now nginx
```

### 3) 部署二进制和静态文件

```bash
sudo mkdir -p /opt/link2clash/static
sudo install -m 0755 /tmp/link2clash/link2clash /opt/link2clash/link2clash
sudo cp -r /tmp/link2clash/static/. /opt/link2clash/static/
```

### 4) 创建 systemd 服务

```bash
sudo tee /etc/systemd/system/link2clash.service > /dev/null <<'EOF'
[Unit]
Description=link2clash API
After=network.target

[Service]
Type=simple
WorkingDirectory=/opt/link2clash
ExecStart=/opt/link2clash/link2clash
Restart=always
User=www-data
Environment=PORT=7625

[Install]
WantedBy=multi-user.target
EOF

sudo systemctl daemon-reload
sudo systemctl enable --now link2clash
sudo systemctl status link2clash --no-pager
```

### 5) Nginx 仅 HTTP（无域名）

```bash
sudo tee /etc/nginx/sites-available/link2clash.conf > /dev/null <<'EOF'
limit_req_zone $binary_remote_addr zone=api_rate:10m rate=30r/m;

server {
    listen 80 default_server;
    server_name _;

    client_max_body_size 256k;

    add_header X-Content-Type-Options nosniff;
    add_header X-Frame-Options DENY;
    add_header Referrer-Policy no-referrer;

    location / {
        root /opt/link2clash/static;
        try_files $uri /index.html;
    }

    location /api/ {
        limit_req zone=api_rate burst=10 nodelay;
        proxy_pass http://127.0.0.1:7625;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    }
}
EOF

sudo ln -sf /etc/nginx/sites-available/link2clash.conf /etc/nginx/sites-enabled/link2clash.conf
sudo nginx -t
sudo systemctl reload nginx
```

如果看到 `conflicting server name "_"` 警告，请移除默认站点：

```bash
sudo rm /etc/nginx/sites-enabled/default
sudo nginx -t
sudo systemctl reload nginx
```

### 6) 验证

```bash
curl -X POST http://127.0.0.1:7625/api/convert \
  -H "Content-Type: application/json" \
  -d '{"input":"vless://uuid@host:443?encryption=none#node1"}'
```

访问 `http://your_vps_ip/`。

## API

```json
POST /api/convert
{"input":"..."}
```
