-- 初始化数据库脚本
-- 此脚本会在 MySQL 容器首次启动时执行

-- 创建数据库（如果不存在）
CREATE DATABASE IF NOT EXISTS mxsec CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;

-- 使用数据库
USE mxsec;

-- 注意：实际的表结构会通过 Gorm AutoMigrate 自动创建
-- 这里只做数据库初始化
