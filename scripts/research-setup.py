#!/usr/bin/env python3
"""Create a private local configuration without printing generated secrets."""
from pathlib import Path
import secrets

root = Path(__file__).resolve().parents[1]
path = root / ".env"
if path.exists():
    print("保留现有 .env；未覆盖配置或密钥。")
    raise SystemExit(0)
settings = {
    "WEKNORA_VERSION": "v0.8.3",
    "FRONTEND_PORT": "8090",
    "RESEARCH_BIND_ADDRESS": "127.0.0.1",
    "APP_PORT": "8091",
    "GIN_MODE": "release",
    "LOG_LEVEL": "info",
    "TZ": "Asia/Shanghai",
    "WEKNORA_LANGUAGE": "zh-CN",
    "DEFAULT_LOCALE": "zh-CN",
    "DB_DRIVER": "postgres",
    "DB_HOST": "postgres",
    "DB_PORT": "5432",
    "DB_USER": "research",
    "DB_PASSWORD": secrets.token_hex(24),
    "DB_NAME": "research",
    "DB_SSL_MODE": "disable",
    "REDIS_ADDR": "redis:6379",
    "REDIS_PASSWORD": secrets.token_hex(24),
    "REDIS_DB": "0",
    "STREAM_MANAGER_TYPE": "redis",
    "RETRIEVE_DRIVER": "postgres",
    "STORAGE_TYPE": "local",
    "STORAGE_ALLOW_LIST": "local",
    "LOCAL_STORAGE_BASE_DIR": "/data/files",
    "JWT_SECRET": secrets.token_hex(32),
    "SYSTEM_AES_KEY": secrets.token_hex(16),
    "DOCREADER_ADDR": "docreader:50051",
    "DOCREADER_TRANSPORT": "grpc",
    "GRPC_AUTH_TOKEN": secrets.token_hex(24),
    "OLLAMA_OPTIONAL": "true",
    "NEO4J_ENABLE": "false",
    "LANGFUSE_ENABLED": "false",
    "WEKNORA_SANDBOX_DOCKER_ENABLED": "false",
    "CONCURRENCY_POOL_SIZE": "2",
    "WEKNORA_ASYNQ_CORE_CONCURRENCY": "2",
    "WEKNORA_ASYNQ_POSTPROCESS_CONCURRENCY": "1",
    "WEKNORA_ASYNQ_ENRICHMENT_CONCURRENCY": "2",
    "WEKNORA_ASYNQ_MAINTENANCE_CONCURRENCY": "2",
    "WEKNORA_ASYNQ_SHARED_CONCURRENCY": "1",
    "WEKNORA_WIKI_ASYNQ_CONCURRENCY": "1",
    "AUTO_MIGRATE": "true",
    "AUTO_RECOVER_DIRTY": "false",
    "FRONTEND_BASE_URL": "http://localhost:8090",
    "FRONTEND_URL": "http://localhost:8090",
}
with path.open("x") as out:
    out.write("# 本机独立开发环境；生产部署请使用团队私网和 HTTPS。\n")
    for key, value in settings.items():
        out.write(f"{key}={value}\n")
path.chmod(0o600)
print("已创建 .env（权限 0600）。模型凭据在平台设置页配置。")
