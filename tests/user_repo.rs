//! storage 实现在真实 SQLite 上测：每用例临时文件库（ADR-0003、§8.1）。
#![allow(clippy::expect_used, clippy::unwrap_used)]

mod common;

use chrono::{DateTime, Utc};
use nebula_server::model::password::PasswordHash;
use nebula_server::model::user::{User, UserId};
use nebula_server::storage::{UserRepo, UserRepoError};

fn sample_user() -> User {
    User {
        id: UserId::new(),
        username: "nova".to_owned(),
        email: "nova@nebula.st".to_owned(),
        created_at: DateTime::parse_from_rfc3339("2026-10-25T08:00:00Z")
            .expect("固定字面量必须可解析")
            .with_timezone(&Utc),
        email_verified: false,
    }
}

fn sample_hash() -> PasswordHash {
    PasswordHash::hash("Nebula!Passw0rd-2026").expect("哈希必须成功")
}

#[tokio::test]
async fn insert_then_find_by_email_roundtrips() {
    let (state, _clock, _mailbox, _dir) = common::app_state().await;
    let repo = &state.user_repo;
    let user = sample_user();
    repo.insert(&user, &sample_hash()).await.expect("首次插入必须成功");

    let found = repo.find_by_email("nova@nebula.st").await.expect("查找必须成功");
    assert_eq!(found, Some(user));
}

#[tokio::test]
async fn find_by_email_matches_case_insensitive_and_missing_is_none() {
    let (state, _clock, _mailbox, _dir) = common::app_state().await;
    let repo = &state.user_repo;
    repo.insert(&sample_user(), &sample_hash()).await.expect("插入必须成功");

    assert!(repo.find_by_email("NOVA@NEBULA.ST").await.expect("查找必须成功").is_some());
    assert!(repo.find_by_email("ghost@nebula.st").await.expect("查找必须成功").is_none());
}

#[tokio::test]
async fn find_by_username_matches_case_insensitive_and_missing_is_none() {
    let (state, _clock, _mailbox, _dir) = common::app_state().await;
    let repo = &state.user_repo;
    repo.insert(&sample_user(), &sample_hash()).await.expect("插入必须成功");

    assert!(repo.find_by_username("NOVA").await.expect("查找必须成功").is_some());
    assert!(repo.find_by_username("ghost").await.expect("查找必须成功").is_none());
}

#[tokio::test]
async fn duplicate_email_is_rejected_as_conflict() {
    let (state, _clock, _mailbox, _dir) = common::app_state().await;
    let repo = &state.user_repo;
    repo.insert(&sample_user(), &sample_hash()).await.expect("首次插入必须成功");

    let mut second = sample_user();
    second.id = UserId::new();
    second.username = "other".to_owned();

    let err = repo.insert(&second, &sample_hash()).await.expect_err("同邮箱二次插入必须失败");
    assert!(matches!(err, UserRepoError::Conflict));
}

#[tokio::test]
async fn duplicate_username_is_rejected_as_conflict() {
    let (state, _clock, _mailbox, _dir) = common::app_state().await;
    let repo = &state.user_repo;
    repo.insert(&sample_user(), &sample_hash()).await.expect("首次插入必须成功");

    let mut second = sample_user();
    second.id = UserId::new();
    second.email = "other@nebula.st".to_owned();

    let err = repo.insert(&second, &sample_hash()).await.expect_err("同用户名二次插入必须失败");
    assert!(matches!(err, UserRepoError::Conflict));
}
