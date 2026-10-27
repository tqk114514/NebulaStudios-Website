//! Nebula v4 认证服务。`main.rs` 负责装配与监听，业务模块挂在本 lib 下，
//! 供 `tests/` 集成测试导入。

pub mod api;
pub mod config;
pub mod context;
pub mod error;
pub mod extractors;
pub mod jobs;
pub mod mailer;
pub mod model;
pub mod storage;
pub mod telemetry;
