#cloud-config
# TMI Application Server - cloud-init for Oracle Linux 9 x86-64 (VM.Standard.E5.Flex)
# Uses Podman (native OL9 container runtime) to run TMI Server + PostgreSQL + Redis
# PostgreSQL runs as a container on the same VM; no external database required

write_files:
  # Environment template with all TMI config
  - path: /etc/tmi/.env_template
    permissions: '0600'
    owner: root:root
    content: |
      TMI_DATABASE_URL=postgres://tmi:${postgres_password_encoded}@127.0.0.1:5432/tmi
      TMI_REDIS_URL=redis://:${redis_password_encoded}@127.0.0.1:6379
      TMI_JWT_SECRET=${jwt_secret}
      TMI_BUILD_MODE=production
      OAUTH_PROVIDERS_TMI_ENABLED=true
      OAUTH_PROVIDERS_TMI_CLIENT_ID=tmi-oci-deployment
      OAUTH_PROVIDERS_TMI_CLIENT_SECRET=${oauth_client_secret}
      TMI_SECRETS_PROVIDER=env
      TMI_LOG_LEVEL=${log_level}
      TMI_LOG_DIR=/tmp
      TMI_SERVER_ADDRESS=0.0.0.0:8080
      # TMI_OAUTH_CALLBACK_URL must be set manually after first boot:
      # echo "TMI_OAUTH_CALLBACK_URL=http://<VM_PUBLIC_IP>:8080/oauth2/callback" >> /etc/tmi/tmi.env
      # systemctl restart tmi-server

  # PostgreSQL systemd service (Podman)
  - path: /etc/systemd/system/tmi-postgres.service
    permissions: '0644'
    owner: root:root
    content: |
      [Unit]
      Description=TMI PostgreSQL Container (Podman)
      After=network-online.target
      Wants=network-online.target

      [Service]
      Type=simple
      Restart=always
      RestartSec=5s
      ExecStartPre=-/usr/bin/podman stop -t 5 tmi-postgres
      ExecStartPre=-/usr/bin/podman rm tmi-postgres
      ExecStart=/usr/bin/podman run \
        --name tmi-postgres \
        --network host \
        --pull=never \
        -e POSTGRES_USER=tmi \
        -e POSTGRES_PASSWORD="${postgres_password}" \
        -e POSTGRES_DB=tmi \
        -v tmi-pgdata:/var/lib/postgresql/data \
        ${postgres_image_url}
      ExecStop=/usr/bin/podman stop -t 5 tmi-postgres

      [Install]
      WantedBy=multi-user.target

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
      After=network-online.target tmi-redis.service tmi-postgres.service
      Wants=network-online.target
      Requires=tmi-redis.service tmi-postgres.service

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
      dnf install -y podman curl 2>&1 | tail -5
      echo "[$(date)] Podman: $(podman --version)"

      echo "[$(date)] Writing environment file..."
      mkdir -p /etc/tmi
      cp /etc/tmi/.env_template /etc/tmi/tmi.env
      chmod 600 /etc/tmi/tmi.env

      echo "[$(date)] Logging into OCIR..."
      OCIR_REGISTRY=$(echo "${postgres_image_url}" | cut -d/ -f1)
      echo "${ocir_auth_token}" | podman login "$OCIR_REGISTRY" \
        -u "${ocir_username}" --password-stdin 2>&1 || {
        echo "WARNING: OCIR login failed - image pulls may fail for private registries"
      }

      echo "[$(date)] Pulling container images..."
      podman pull ${postgres_image_url} 2>&1 | tail -3 || {
        echo "ERROR: Failed to pull PostgreSQL image"
        exit 1
      }
      podman pull ${redis_image_url} 2>&1 | tail -3 || {
        echo "ERROR: Failed to pull Redis image"
        exit 1
      }
      podman pull ${tmi_image_url} 2>&1 | tail -3 || {
        echo "ERROR: Failed to pull TMI image"
        exit 1
      }
      podman pull ${tmi_ux_image_url} 2>&1 | tail -3 || {
        echo "ERROR: Failed to pull TMI-UX image"
        exit 1
      }
      echo "[$(date)] Images: $(podman images --format '{{.Repository}}:{{.Tag}}')"

      echo "[$(date)] Starting services..."
      systemctl daemon-reload
      systemctl enable tmi-postgres tmi-redis tmi-server tmi-ux

      echo "[$(date)] Starting PostgreSQL..."
      systemctl start tmi-postgres
      echo "[$(date)] Waiting 20s for PostgreSQL to initialize..."
      sleep 20

      echo "[$(date)] Starting Redis..."
      systemctl start tmi-redis
      sleep 5

      echo "[$(date)] Starting TMI Server..."
      systemctl start tmi-server
      sleep 5

      echo "[$(date)] Starting TMI-UX..."
      systemctl start tmi-ux

      echo "[$(date)] TMI Setup Complete!"
      echo "[$(date)] NOTE: Set TMI_OAUTH_CALLBACK_URL manually after verifying the VM's public IP:"
      echo "  echo 'TMI_OAUTH_CALLBACK_URL=http://<VM_PUBLIC_IP>:8080/oauth2/callback' >> /etc/tmi/tmi.env"
      echo "  systemctl restart tmi-server"

  # TMI-UX Frontend systemd service (Podman, port 4200)
  - path: /etc/systemd/system/tmi-ux.service
    permissions: '0644'
    owner: root:root
    content: |
      [Unit]
      Description=TMI-UX Frontend Container (Podman)
      After=network-online.target
      Wants=network-online.target

      [Service]
      Type=simple
      Restart=always
      RestartSec=10s
      ExecStartPre=-/usr/bin/podman stop -t 5 tmi-ux
      ExecStartPre=-/usr/bin/podman rm tmi-ux
      ExecStart=/usr/bin/podman run \
        --name tmi-ux \
        --network host \
        --pull=never \
        -e PORT=4200 \
        -e TMI_API_URL=${tmi_ux_api_url} \
        ${tmi_ux_image_url}
      ExecStop=/usr/bin/podman stop -t 5 tmi-ux

      [Install]
      WantedBy=multi-user.target

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
  Services: tmi-postgres, tmi-redis, tmi-server, tmi-ux (Podman containers on host network)
  After boot: set TMI_OAUTH_CALLBACK_URL in /etc/tmi/tmi.env with the VM's public IP.
