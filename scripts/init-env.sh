#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
python3 - <<'PY'
import os,secrets
try:
    fd=os.open('.env',os.O_WRONLY|os.O_CREAT|os.O_EXCL,0o600)
except FileExistsError:
    print('.env уже существует; значения сохранены.')
else:
    with os.fdopen(fd,'w') as file:
        for name in ('POSTGRES_PASSWORD','RABBITMQ_PASSWORD','JWT_SECRET','GRAFANA_PASSWORD'):
            file.write(name+'='+secrets.token_hex(32)+'\n')
        file.write('APP_PORT=8080\nGRAFANA_PORT=3000\nPROMETHEUS_PORT=9090\nRABBITMQ_MANAGEMENT_PORT=15672\nDEMO_ENABLED=false\n')
    print('.env создан с новыми случайными значениями.')
PY
