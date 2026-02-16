# Matrix Cloud Security Platform - Server 配置模板
# 所有 __XXX__ 占位符由 deploy.sh 从 .env 文件替换
# 如需新增配置项，在此模板添加占位符，并在 .env 和 deploy.sh 中同步

server:
  grpc:
    host: "0.0.0.0"
    port: 6751
  http:
    host: "0.0.0.0"
    port: 8080

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
  dir: "/opt/mxsec-platform/plugins"
  base_url: "__PLUGINS_BASE_URL__"
