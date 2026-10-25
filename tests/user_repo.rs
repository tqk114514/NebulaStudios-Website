//! storage 实现在真实 SQLite 上测：每用例临时文件库（ADR-0003、§8.1）。
//! 迁移在测试装配中执行——启动路径不跑 DDL 约束的是生产，测试不受限。
#![allow(clippy::expect_used, clippy::unwrap_used)]

use chrono::{DateTime, Utc};
use nebula_server::model::user::{User, UserId};
use nebula_server::storage::sqlite::SqliteUserRepo;
use nebula_server::storage::{UserRepo, UserRepoError};
use sqlx::sqlite::{SqliteConnectOptions, SqlitePool};

/// 建临时文件库、跑全部迁移、打开仓储；返回 `TempDir` 钉住文件生命周期。
async fn migrated_repo() -> (SqliteUserRepo, tempfile::TempDir) {
    let dir = tempfile::tempdir().expect("临时目录必须可创建");
    let db_path = dir.path().join("users.db");

    let bootstrap = SqlitePool::connect_with(
        SqliteConnectOptions::new().filename(&db_path).create_if_missing(true),
    )
    .await
    .expect("建库连接必须成功");
    sqlx::migrate!().run(&bootstrap).await.expect("迁移必须成功");
    bootstrap.close().await;

    let repo = SqliteUserRepo::open(&db_path).await.expect("已迁移的库必须可打开");
    (repo, dir)
}

fn sample_user() -> User {
    User {
        id: UserId::new(),
        username: "nova".to_owned(),
        email: "nova@nebula.st".to_owned(),
        created_at: DateTime::parse_from_rfc3339("2026-10-25T08:00:00Z")
            .expect("固定字面量必须可解析")
            .with_timezone(&Utc),
    }
}

#[tokio::test]
async fn insert_then_find_by_email_roundtrips() {
    let (repo, _dir) = migrated_repo().await;
    let user = sample_user();
    repo.insert(&user).await.expect("首次插入必须成功");

    let found = repo.find_by_email("nova@nebula.st").await.expect("查找必须成功");
    assert_eq!(found, Some(user));
}

#[tokio::test]
async fn find_by_email_matches_case_insensitive_and_missing_is_none() {
    let (repo, _dir) = migrated_repo().await;
    repo.insert(&sample_user()).await.expect("插入必须成功");

    assert!(repo.find_by_email("NOVA@NEBULA.ST").await.expect("查找必须成功").is_some());
    assert!(repo.find_by_email("ghost@nebula.st").await.expect("查找必须成功").is_none());
}

#[tokio::test]
async fn find_by_username_matches_case_insensitive_and_missing_is_none() {
    let (repo, _dir) = migrated_repo().await;
    repo.insert(&sample_user()).await.expect("插入必须成功");

    assert!(repo.find_by_username("NOVA").await.expect("查找必须成功").is_some());
    assert!(repo.find_by_username("ghost").await.expect("查找必须成功").is_none());
}

#[tokio::test]
async fn duplicate_email_is_rejected_as_conflict() {
    let (repo, _dir) = migrated_repo().await;
    let first = sample_user();
    repo.insert(&first).await.expect("首次插入必须成功");

    let mut second = sample_user();
    second.id = UserId::new();
    second.username = "other".to_owned();

    let err = repo.insert(&second).await.expect_err("同邮箱二次插入必须失败");
    assert!(matches!(err, UserRepoError::Conflict));
}

#[tokio::test]
async fn duplicate_username_is_rejected_as_conflict() {
    let (repo, _dir) = migrated_repo().await;
    repo.insert(&sample_user()).await.expect("首次插入必须成功");

    let mut second = sample_user();
    second.id = UserId::new();
    second.email = "other@nebula.st".to_owned();

    let err = repo.insert(&second).await.expect_err("同用户名二次插入必须失败");
    assert!(matches!(err, UserRepoError::Conflict));
}
