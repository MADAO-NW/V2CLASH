#!/usr/bin/env bash
set -euo pipefail

REPO="${REPO:-yourname/link2clash}"
VERSION="${VERSION:-latest}"
STATIC_REF="${STATIC_REF:-auto}"
DOMAIN="${DOMAIN:-}"
ENABLE_TLS="${ENABLE_TLS:-0}"
APP_DIR="${APP_DIR:-/opt/link2clash}"
BINARY_NAME="${BINARY_NAME:-link2clash}"
DOWNLOAD_URL="${DOWNLOAD_URL:-}"
APP_USER="${APP_USER:-www-data}"
AUTO_REMOVE_DEFAULT="${AUTO_REMOVE_DEFAULT:-0}"

if [ "$REPO" = "yourname/link2clash" ]; then
  echo "Set REPO=owner/repo before running."
  exit 1
fi

if [ "$ENABLE_TLS" -eq 1 ] && [ -z "$DOMAIN" ]; then
  echo "ENABLE_TLS=1 requires DOMAIN."
  exit 1
fi

if [ "${EUID:-$(id -u)}" -eq 0 ]; then
  SUDO=""
else
  SUDO="sudo"
fi

ARCH="$(uname -m)"
case "$ARCH" in
  x86_64|amd64)
    ARCH="amd64"
    ;;
  aarch64|arm64)
    ARCH="arm64"
    ;;
  *)
    echo "Unsupported architecture: $ARCH"
    exit 1
    ;;
esac

if [ -n "$DOWNLOAD_URL" ]; then
  BIN_URL="$DOWNLOAD_URL"
else
  if [ "$VERSION" = "latest" ]; then
    BIN_URL="https://github.com/$REPO/releases/latest/download/${BINARY_NAME}_linux_${ARCH}"
  else
    BIN_URL="https://github.com/$REPO/releases/download/$VERSION/${BINARY_NAME}_linux_${ARCH}"
  fi
fi

if [ "$STATIC_REF" = "auto" ]; then
  if [ "$VERSION" = "latest" ]; then
    STATIC_REF="main"
  else
    STATIC_REF="$VERSION"
  fi
fi

STATIC_BASE="https://raw.githubusercontent.com/$REPO/$STATIC_REF/static"

$SUDO apt-get update
packages=(nginx curl ca-certificates)
if [ "$ENABLE_TLS" -eq 1 ]; then
  packages+=(certbot python3-certbot-nginx)
fi
$SUDO apt-get install -y "${packages[@]}"

TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT

curl -fsSL "$BIN_URL" -o "$TMP_DIR/$BINARY_NAME"
chmod +x "$TMP_DIR/$BINARY_NAME"

mkdir -p "$TMP_DIR/static"
curl -fsSL "$STATIC_BASE/index.html" -o "$TMP_DIR/static/index.html"
curl -fsSL "$STATIC_BASE/app.js" -o "$TMP_DIR/static/app.js"
curl -fsSL "$STATIC_BASE/style.css" -o "$TMP_DIR/static/style.css"

$SUDO mkdir -p "$APP_DIR/static"
$SUDO install -m 0755 "$TMP_DIR/$BINARY_NAME" "$APP_DIR/$BINARY_NAME"
$SUDO cp -r "$TMP_DIR/static/." "$APP_DIR/static/"

$SUDO tee /etc/systemd/system/link2clash.service > /dev/null <<EOF
[Unit]
Description=link2clash API
After=network.target

[Service]
Type=simple
WorkingDirectory=$APP_DIR
ExecStart=$APP_DIR/$BINARY_NAME
Restart=always
User=$APP_USER
Environment=PORT=7625

[Install]
WantedBy=multi-user.target
EOF

$SUDO systemctl daemon-reload
$SUDO systemctl enable --now link2clash

if [ "$AUTO_REMOVE_DEFAULT" -eq 1 ]; then
  $SUDO rm -f /etc/nginx/sites-enabled/default /etc/nginx/sites-enabled/000-default || true
fi

SERVER_NAME="${DOMAIN:-_}"
LISTEN_DIRECTIVE="listen 80;"
if [ -z "$DOMAIN" ]; then
  LISTEN_DIRECTIVE="listen 80 default_server;"
fi

$SUDO tee /etc/nginx/sites-available/link2clash.conf > /dev/null <<EOF
limit_req_zone \$binary_remote_addr zone=api_rate:10m rate=30r/m;

server {
    $LISTEN_DIRECTIVE
    server_name $SERVER_NAME;

    client_max_body_size 256k;

    add_header X-Content-Type-Options nosniff;
    add_header X-Frame-Options DENY;
    add_header Referrer-Policy no-referrer;

    location / {
        root $APP_DIR/static;
        try_files \$uri /index.html;
    }

    location /api/ {
        limit_req zone=api_rate burst=10 nodelay;
        proxy_pass http://127.0.0.1:7625;
        proxy_http_version 1.1;
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
    }
}
EOF

$SUDO ln -sf /etc/nginx/sites-available/link2clash.conf /etc/nginx/sites-enabled/link2clash.conf
$SUDO nginx -t
$SUDO systemctl reload nginx

if [ "$ENABLE_TLS" -eq 1 ]; then
  $SUDO certbot --nginx -d "$DOMAIN"

  $SUDO tee /etc/nginx/sites-available/link2clash.conf > /dev/null <<EOF
limit_req_zone \$binary_remote_addr zone=api_rate:10m rate=30r/m;

server {
    listen 80;
    server_name $DOMAIN;
    return 301 https://\$host\$request_uri;
}

server {
    listen 443 ssl http2;
    server_name $DOMAIN;

    ssl_certificate /etc/letsencrypt/live/$DOMAIN/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/$DOMAIN/privkey.pem;
    ssl_protocols TLSv1.2 TLSv1.3;

    client_max_body_size 256k;

    add_header X-Content-Type-Options nosniff;
    add_header X-Frame-Options DENY;
    add_header Referrer-Policy no-referrer;

    location / {
        root $APP_DIR/static;
        try_files \$uri /index.html;
    }

    location /api/ {
        limit_req zone=api_rate burst=10 nodelay;
        proxy_pass http://127.0.0.1:7625;
        proxy_http_version 1.1;
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
    }
}
EOF

  $SUDO nginx -t
  $SUDO systemctl reload nginx
  echo "Verify: curl https://$DOMAIN/api/convert"
else
  echo "Verify: curl http://<server-ip>/api/convert"
fi
