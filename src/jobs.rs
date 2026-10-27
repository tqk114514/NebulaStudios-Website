//! 后台任务：按固定间隔清理过期认证数据。
//!
//! 任务只依赖 [`crate::storage::MaintenanceRepo`]，清理范围与事务边界在存储实现内；
//! 启动/停止由装配层负责，任务本身不读取配置、不阻塞监听。

use std::time::Duration;

use tokio::task::JoinHandle;
use tokio::time::MissedTickBehavior;

use crate::api::AppState;

/// 启动过期数据清理任务；返回句柄，停机时应 abort。
///
/// 首次 tick 只用于对齐间隔，不执行清理，避免服务启动瞬间与监听/迁移竞争。
pub fn spawn_cleanup(state: AppState, interval: Duration) -> JoinHandle<()> {
    tokio::spawn(async move {
        let mut ticker = tokio::time::interval(interval);
        ticker.set_missed_tick_behavior(MissedTickBehavior::Delay);
        ticker.tick().await;

        loop {
            ticker.tick().await;
            let now = state.clock.now();
            match state.maintenance_repo.purge_expired(now).await {
                Ok(report) => {
                    metrics::counter!("nebula_cleanup_runs_total", "outcome" => "success")
                        .increment(1);
                    metrics::counter!("nebula_cleanup_deleted_refresh_tokens_total")
                        .increment(report.refresh_tokens);
                    metrics::counter!("nebula_cleanup_deleted_refresh_families_total")
                        .increment(report.refresh_families);
                    metrics::counter!("nebula_cleanup_deleted_email_verifications_total")
                        .increment(report.email_verifications);
                    metrics::counter!("nebula_cleanup_deleted_totp_recovery_codes_total")
                        .increment(report.totp_recovery_codes);
                    metrics::counter!("nebula_cleanup_deleted_totp_login_challenges_total")
                        .increment(report.totp_login_challenges);
                    metrics::counter!("nebula_cleanup_deleted_oauth_authorization_codes_total")
                        .increment(report.oauth_authorization_codes);
                    tracing::info!(?report, "过期数据清理完成");
                }
                Err(err) => {
                    metrics::counter!("nebula_cleanup_runs_total", "outcome" => "error")
                        .increment(1);
                    tracing::error!(error = %err, "过期数据清理失败");
                }
            }
        }
    })
}
