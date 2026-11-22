//! 后台清理任务的调度测试：用假 `MaintenanceRepo` 确认按间隔触发，不依赖真实过期数据。
#![allow(clippy::expect_used, clippy::unwrap_used)]

use std::sync::Arc;
use std::sync::atomic::{AtomicUsize, Ordering};
use std::time::Duration;

mod common;

use async_trait::async_trait;
use chrono::{DateTime, Utc};
use nebula_server::jobs::spawn_cleanup;
use nebula_server::storage::{CleanupReport, MaintenanceRepo, StorageError};

struct CountingCleanup(Arc<AtomicUsize>);

struct FailingCleanup(Arc<AtomicUsize>);

#[async_trait]
impl MaintenanceRepo for CountingCleanup {
    async fn purge_expired(&self, _now: DateTime<Utc>) -> Result<CleanupReport, StorageError> {
        self.0.fetch_add(1, Ordering::SeqCst);
        Ok(CleanupReport::default())
    }
}

#[async_trait]
impl MaintenanceRepo for FailingCleanup {
    async fn purge_expired(&self, _now: DateTime<Utc>) -> Result<CleanupReport, StorageError> {
        self.0.fetch_add(1, Ordering::SeqCst);
        Err(StorageError::wrap(std::io::Error::other("cleanup failed")))
    }
}

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
async fn cleanup_task_runs_after_interval() {
    let (mut state, _clock, _mailbox, _dir) = common::app_state().await;
    let calls = Arc::new(AtomicUsize::new(0));
    state.maintenance_repo = Arc::new(CountingCleanup(calls.clone()));

    let handle = spawn_cleanup(state, Duration::from_millis(20));
    for _ in 0..100 {
        if calls.load(Ordering::SeqCst) > 0 {
            break;
        }
        tokio::time::sleep(Duration::from_millis(10)).await;
    }
    handle.abort();

    assert!(calls.load(Ordering::SeqCst) > 0, "清理任务必须至少运行一次");
}

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
async fn cleanup_task_survives_repository_error() {
    let (mut state, _clock, _mailbox, _dir) = common::app_state().await;
    let calls = Arc::new(AtomicUsize::new(0));
    state.maintenance_repo = Arc::new(FailingCleanup(calls.clone()));

    let handle = spawn_cleanup(state, Duration::from_millis(20));
    for _ in 0..100 {
        if calls.load(Ordering::SeqCst) > 0 {
            break;
        }
        tokio::time::sleep(Duration::from_millis(10)).await;
    }
    handle.abort();

    assert!(calls.load(Ordering::SeqCst) > 0, "错误后循环必须继续运行");
}
