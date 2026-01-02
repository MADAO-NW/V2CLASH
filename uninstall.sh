#!/usr/bin/env bash
set -euo pipefail

APP_DIR="${APP_DIR:-/opt/link2clash}"
SERVICE_NAME="${SERVICE_NAME:-link2clash}"
REMOVE_NGINX_CONF="${REMOVE_NGINX_CONF:-1}"
REMOVE_PACKAGES="${REMOVE_PACKAGES:-0}"

if [ "${EUID:-$(id -u)}" -eq 0 ]; then
  SUDO=""
else
  SUDO="sudo"
fi

have_systemctl=0
if command -v systemctl >/dev/null 2>&1; then
  have_systemctl=1
fi

have_nginx=0
if command -v nginx >/dev/null 2>&1; then
  have_nginx=1
fi

if [ "$have_systemctl" -eq 1 ]; then
  $SUDO systemctl stop "$SERVICE_NAME" || true
  $SUDO systemctl disable "$SERVICE_NAME" || true
  $SUDO rm -f "/etc/systemd/system/${SERVICE_NAME}.service"
  $SUDO systemctl daemon-reload
fi

$SUDO rm -rf "$APP_DIR"

if [ "$REMOVE_NGINX_CONF" -eq 1 ]; then
  $SUDO rm -f "/etc/nginx/sites-enabled/${SERVICE_NAME}.conf"
  $SUDO rm -f "/etc/nginx/sites-available/${SERVICE_NAME}.conf"
  if [ "$have_nginx" -eq 1 ]; then
    $SUDO nginx -t
    $SUDO systemctl reload nginx
  fi
fi

if [ "$REMOVE_PACKAGES" -eq 1 ]; then
  $SUDO apt-get remove -y nginx certbot python3-certbot-nginx
  $SUDO apt-get autoremove -y
fi

echo "Removed $SERVICE_NAME."
