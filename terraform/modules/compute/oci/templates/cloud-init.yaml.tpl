#cloud-config
# TMI Application Server - cloud-init for Oracle Linux 9 ARM64 (VM.Standard.A1.Flex)
# Uses Podman (native OL9 container runtime) to run TMI Server + Redis
# Oracle ADB wallet is downloaded via PAR URL and mounted into TMI container

write_files:
  # Environment template with all TMI config
  - path: /etc/tmi/.env_template
    permissions: '0600'
    owner: root:root
    content: |
      TMI_DATABASE_URL=oracle://${db_username}:${db_password_encoded}@${oracle_connect_string}
      TMI_ORACLE_WALLET_LOCATION=/wallet
      TMI_REDIS_URL=redis://:${redis_password_encoded}@127.0.0.1:6379
      TMI_JWT_SECRET=${jwt_secret}
      TMI_BUILD_MODE=dev
      OAUTH_PROVIDERS_TMI_ENABLED=true
      OAUTH_PROVIDERS_TMI_CLIENT_ID=tmi-oci-deployment
      OAUTH_PROVIDERS_TMI_CLIENT_SECRET=${jwt_secret}
      TMI_SECRETS_PROVIDER=oci
      TMI_SECRETS_OCI_VAULT_OCID=${vault_ocid}
      TMI_LOG_LEVEL=${log_level}
      TMI_LOG_DIR=/tmp
      TMI_SERVER_ADDRESS=0.0.0.0:8080
%{ if oci_log_id != "" }      TMI_CLOUD_LOG_ENABLED=true
      TMI_CLOUD_LOG_PROVIDER=oci
      TMI_OCI_LOG_ID=${oci_log_id}
      TMI_CLOUD_LOG_LEVEL=${cloud_log_level}
%{ endif }

  # Redis systemd service (Podman)
  - path: /etc/systemd/system/tmi-redis.service
    permissions: '0644'
    owner: root:root
    content: |
      [Unit]
      Description=TMI Redis Container (Podman)
      After=network-online.target
      Wants=network-online.target

      [Service]
      Type=simple
      Restart=always
      RestartSec=5s
      ExecStartPre=-/usr/bin/podman stop -t 5 tmi-redis
      ExecStartPre=-/usr/bin/podman rm tmi-redis
      ExecStart=/usr/bin/podman run \
        --name tmi-redis \
        --network host \
        --pull=never \
        ${redis_image_url} \
        --requirepass "${redis_password}" \
        --appendonly yes \
        --bind 127.0.0.1 \
        --port 6379
      ExecStop=/usr/bin/podman stop -t 5 tmi-redis

      [Install]
      WantedBy=multi-user.target

  # TMI Server systemd service (Podman)
  - path: /etc/systemd/system/tmi-server.service
    permissions: '0644'
    owner: root:root
    content: |
      [Unit]
      Description=TMI Server Container (Podman)
      After=network-online.target tmi-redis.service
      Wants=network-online.target
      Requires=tmi-redis.service

      [Service]
      Type=simple
      Restart=always
      RestartSec=10s
      ExecStartPre=-/usr/bin/podman stop -t 5 tmi-server
      ExecStartPre=-/usr/bin/podman rm tmi-server
      ExecStart=/usr/bin/podman run \
        --name tmi-server \
        --network host \
        --pull=never \
        -v /wallet:/wallet:ro \
        --env-file /etc/tmi/tmi.env \
        ${tmi_image_url}
      ExecStop=/usr/bin/podman stop -t 5 tmi-server

      [Install]
      WantedBy=multi-user.target

  # Setup script
  - path: /opt/tmi/setup.sh
    permissions: '0755'
    owner: root:root
    content: |
      #!/bin/bash
      set -euo pipefail
      LOG=/var/log/tmi-setup.log
      exec > >(tee -a "$LOG") 2>&1

      echo "[$(date)] TMI First-Boot Setup Starting"

      echo "[$(date)] Ensuring podman and tools are installed..."
      dnf install -y podman unzip curl 2>&1 | tail -5
      echo "[$(date)] Podman: $(podman --version)"

      echo "[$(date)] Downloading Oracle ADB wallet..."
      mkdir -p /wallet /etc/tmi
      curl -fsSL -o /wallet/wallet.b64 "${wallet_par_url}" || {
        echo "ERROR: Failed to download wallet"
        exit 1
      }
      chmod 600 /wallet/wallet.b64
      # Wallet is stored base64-encoded in Object Storage (base64_encode_content=true)
      base64 -d /wallet/wallet.b64 > /wallet/wallet.zip && rm -f /wallet/wallet.b64 || {
        echo "ERROR: Failed to decode wallet"
        exit 1
      }
      chmod 600 /wallet/wallet.zip
      unzip -o /wallet/wallet.zip -d /wallet
      chmod 644 /wallet/*
      echo "[$(date)] Wallet files: $(ls /wallet/)"

      echo "[$(date)] Writing environment file..."
      cp /etc/tmi/.env_template /etc/tmi/tmi.env
      chmod 600 /etc/tmi/tmi.env

      echo "[$(date)] Pulling container images..."
      podman pull ${tmi_image_url} 2>&1 | tail -3 || {
        echo "ERROR: Failed to pull TMI image"
        exit 1
      }
      podman pull ${redis_image_url} 2>&1 | tail -3 || {
        echo "ERROR: Failed to pull Redis image"
        exit 1
      }
      echo "[$(date)] Images: $(podman images --format '{{.Repository}}:{{.Tag}}')"

      echo "[$(date)] Starting services..."
      systemctl daemon-reload
      systemctl enable tmi-redis tmi-server
      systemctl start tmi-redis
      sleep 15
      systemctl start tmi-server

      echo "[$(date)] TMI Setup Complete!"

  # Systemd service for first-boot setup
  - path: /etc/systemd/system/tmi-setup.service
    permissions: '0644'
    owner: root:root
    content: |
      [Unit]
      Description=TMI First-Boot Setup
      After=network-online.target
      Wants=network-online.target
      ConditionPathExists=!/var/lib/tmi-setup-complete

      [Service]
      Type=oneshot
      RemainAfterExit=yes
      ExecStart=/opt/tmi/setup.sh
      ExecStartPost=/usr/bin/touch /var/lib/tmi-setup-complete
      TimeoutStartSec=1200
      StandardOutput=journal+console
      StandardError=journal+console

      [Install]
      WantedBy=multi-user.target

runcmd:
  - setenforce 0 || true
  - sed -i 's/^SELINUX=enforcing/SELINUX=permissive/' /etc/selinux/config
  # Disable firewalld - OCI NSGs/security lists handle network security
  - systemctl stop firewalld || true
  - systemctl disable firewalld || true
  - systemctl mask firewalld || true
  - systemctl daemon-reload
  - systemctl enable tmi-setup
  - systemctl start tmi-setup

final_message: |
  TMI cloud-init submitted setup service. Check /var/log/tmi-setup.log for details.
  Services: tmi-redis and tmi-server (Podman containers on host network, port 8080)
