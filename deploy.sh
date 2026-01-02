#!/usr/bin/env bash
set -euo pipefail

DOMAIN="yourdomain.com"
ENABLE_TLS=1
APP_DIR="/opt/link2clash"
SRC_DIR="$(pwd)/dist"

SERVER_NAME="$DOMAIN"
if [ "$ENABLE_TLS" -ne 1 ] && [ "$DOMAIN" = "yourdomain.com" ]; then
  SERVER_NAME="_"
fi

if [ "$ENABLE_TLS" -eq 1 ] && [ "$DOMAIN" = "yourdomain.com" ]; then
  echo "Set DOMAIN or set ENABLE_TLS=0 for HTTP only."
  exit 1
fi

if [ ! -x "$SRC_DIR/link2clash" ]; then
  echo "Missing binary: $SRC_DIR/link2clash"
  exit 1
fi

if [ ! -d "$SRC_DIR/static" ]; then
  echo "Missing static directory: $SRC_DIR/static"
  exit 1
fi

sudo apt-get update
sudo apt-get install -y nginx certbot python3-certbot-nginx

sudo mkdir -p "$APP_DIR"
sudo install -m 0755 "$SRC_DIR/link2clash" "$APP_DIR/link2clash"
sudo mkdir -p "$APP_DIR/static"
sudo cp -r "$SRC_DIR/static/." "$APP_DIR/static/"

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

sudo tee /etc/nginx/sites-available/link2clash.conf > /dev/null <<EOF
limit_req_zone \$binary_remote_addr zone=api_rate:10m rate=30r/m;

server {
    listen 80;
    server_name $SERVER_NAME;

    client_max_body_size 256k;

    add_header X-Content-Type-Options nosniff;
    add_header X-Frame-Options DENY;
    add_header Referrer-Policy no-referrer;

    location / {
        root /opt/link2clash/static;
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

sudo ln -sf /etc/nginx/sites-available/link2clash.conf /etc/nginx/sites-enabled/link2clash.conf
sudo nginx -t
sudo systemctl reload nginx

if [ "$ENABLE_TLS" -eq 1 ]; then
  sudo certbot --nginx -d "$DOMAIN"

  sudo tee /etc/nginx/sites-available/link2clash.conf > /dev/null <<EOF
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
        root /opt/link2clash/static;
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

  sudo nginx -t
  sudo systemctl reload nginx
  echo "Verify: curl https://$DOMAIN/api/convert"
else
  echo "Verify: curl http://<server-ip>/api/convert"
fi
