#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
docker compose exec -T postgres psql -v ON_ERROR_STOP=1 -U orbita -d orbita <<'SQL'
SELECT 'CREATE DATABASE orbita_tests' WHERE NOT EXISTS (SELECT 1 FROM pg_database WHERE datname='orbita_tests')\gexec
SQL
docker build --target build -t orbita-tests:local .
python3 - <<'PY'
import os,pathlib,subprocess
values=dict(line.split('=',1) for line in pathlib.Path('.env').read_text().splitlines() if '=' in line and not line.startswith('#'))
env=dict(os.environ)
env['TEST_DATABASE_URL']='postgres://orbita:'+values['POSTGRES_PASSWORD']+'@postgres:5432/orbita_tests?sslmode=disable'
subprocess.run(['docker','run','--rm','--network','orbita_default','-e','TEST_DATABASE_URL','orbita-tests:local','go','test','-v','./internal/repository','-run','TestPostgres'],env=env,check=True)
PY
