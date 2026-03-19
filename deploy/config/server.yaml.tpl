# Matrix Cloud Security Platform - Server 配置模板
# 所有 __XXX__ 占位符由 deploy.sh 或 dev-docker-start.sh 从 .env 文件替换
# 如需新增配置项，在此模板添加占位符，并在 .env.example 和 deploy.sh 中同步

server:
  grpc:
    host: "0.0.0.0"
    port: 6751
  http:
    host: "0.0.0.0"
    port: 8080
  jwt_secret: "__JWT_SECRET__"

database:
  type: "mysql"
  mysql:
    host: "__MYSQL_HOST__"
    port: __MYSQL_PORT__
    user: "__MYSQL_USER__"
    password: "__MYSQL_PASSWORD__"
    database: "__MYSQL_DATABASE__"
    charset: "utf8mb4"
    parse_time: true
    loc: "Asia/Shanghai"
    max_idle_conns: __DB_MAX_IDLE_CONNS__
    max_open_conns: __DB_MAX_OPEN_CONNS__
    conn_max_lifetime: "__DB_CONN_MAX_LIFETIME__"

redis:
  addr: "__REDIS_ADDR__"
  password: "__REDIS_PASSWORD__"
  db: __REDIS_DB__
  pool_size: __REDIS_POOL_SIZE__

kafka:
  enabled: __KAFKA_ENABLED__
  brokers:
    - "__KAFKA_BROKERS__"
  topic_prefix: "__KAFKA_TOPIC_PREFIX__"

clickhouse:
  enabled: __CLICKHOUSE_ENABLED__
  addrs:
    - "__CLICKHOUSE_ADDR__"
  database: "__CLICKHOUSE_DATABASE__"
  username: "__CLICKHOUSE_USER__"
  password: "__CLICKHOUSE_PASSWORD__"

mtls:
  ca_cert: "/etc/mxsec-platform/certs/ca.crt"
  server_cert: "/etc/mxsec-platform/certs/server.crt"
  server_key: "/etc/mxsec-platform/certs/server.key"

log:
  level: "__LOG_LEVEL__"
  format: "__LOG_FORMAT__"
  file: "/var/log/mxsec-platform/server.log"
  error_file: "/var/log/mxsec-platform/error.log"
  max_age: __LOG_MAX_AGE__

agent:
  heartbeat_interval: __HEARTBEAT_INTERVAL__
  work_dir: "/var/lib/mxsec-agent"

plugins:
  dir: "__PLUGINS_DIR__"
  base_url: "__PLUGINS_BASE_URL__"
