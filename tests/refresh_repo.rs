//! refresh 家族仓储在真实 SQLite 上测：轮转、重放整族撤销、批量撤销（ADR-0003、§8.1）。
#![allow(clippy::expect_used, clippy::unwrap_used)]

mod common;

use chrono::{DateTime, TimeDelta, Utc};
use nebula_server::model::password::PasswordHash;
use nebula_server::model::tokens::{NewRefreshToken, RefreshToken};
use nebula_server::model::user::{User, UserId};
use nebula_server::storage::{RefreshTokenRepo, RotateRefreshError, UserRepo};
use uuid::Uuid;

fn now() -> DateTime<Utc> {
    DateTime::parse_from_rfc3339("2026-10-26T08:00:00Z")
        .expect("固定字面量必须可解析")
        .with_timezone(&Utc)
}

fn seed_user(user_id: UserId) -> User {
    User {
        id: user_id,
        username: format!("user-{user_id}"),
        email: format!("{user_id}@nebula.st"),
        created_at: now(),
        email_verified: false,
    }
}

/// 生成一对（明文 token，落库描述）；家族号由调用方持有以便串联轮转。
fn fresh_entry(user_id: UserId, family_id: Uuid, expires_at: DateTime<Utc>) -> NewRefreshToken {
    NewRefreshToken {
        family_id,
        user_id,
        token_hash: RefreshToken::generate().to_hash_hex(),
        expires_at,
    }
}

async fn setup() -> (
    nebula_server::storage::sqlite::SqliteUserRepo,
    nebula_server::storage::sqlite::SqliteRefreshRepo,
    UserId,
    tempfile::TempDir,
) {
    let (users, refreshes, dir) = common::repos().await;
    let user_id = UserId::new();
    users
        .insert(
            &seed_user(user_id),
            &PasswordHash::hash("Nebula!Passw0rd-2026").expect("哈希必须成功"),
        )
        .await
        .expect("种子用户必须可插入");
    (users, refreshes, user_id, dir)
}

#[tokio::test]
async fn rotation_chains_within_active_family() {
    let (_users, refreshes, user, _dir) = setup().await;
    let family = Uuid::now_v7();
    let first = fresh_entry(user, family, now() + TimeDelta::days(30));
    refreshes.create_family(&first, now()).await.expect("建族必须成功");

    let second_hash = RefreshToken::generate().to_hash_hex();
    refreshes
        .rotate(&first.token_hash, &second_hash, now() + TimeDelta::days(30), now())
        .await
        .expect("第一次轮转必须成功");

    let third_hash = RefreshToken::generate().to_hash_hex();
    let rotated_user = refreshes
        .rotate(&second_hash, &third_hash, now() + TimeDelta::days(30), now())
        .await
        .expect("链上的第二次轮转必须成功");
    assert_eq!(rotated_user, user);
}

#[tokio::test]
async fn replaying_rotated_token_revokes_whole_family() {
    let (_users, refreshes, user, _dir) = setup().await;
    let family = Uuid::now_v7();
    let first = fresh_entry(user, family, now() + TimeDelta::days(30));
    refreshes.create_family(&first, now()).await.expect("建族必须成功");

    let second_hash = RefreshToken::generate().to_hash_hex();
    refreshes
        .rotate(&first.token_hash, &second_hash, now() + TimeDelta::days(30), now())
        .await
        .expect("正常轮转必须成功");

    // 重放已轮转的 first：判定重放，整族撤销
    let replay = refreshes
        .rotate(
            &first.token_hash,
            &RefreshToken::generate().to_hash_hex(),
            now() + TimeDelta::days(30),
            now(),
        )
        .await
        .expect_err("重放必须被拒");
    assert!(matches!(replay, RotateRefreshError::Replay));

    // 撤销波及链上最新的合法 token
    let after = refreshes
        .rotate(
            &second_hash,
            &RefreshToken::generate().to_hash_hex(),
            now() + TimeDelta::days(30),
            now(),
        )
        .await
        .expect_err("已撤销家族内的 token 不可再轮转");
    assert!(matches!(after, RotateRefreshError::Invalid));
}

#[tokio::test]
async fn unknown_hash_is_invalid() {
    let (_users, refreshes, user, _dir) = setup().await;
    let family = Uuid::now_v7();
    let first = fresh_entry(user, family, now() + TimeDelta::days(30));
    refreshes.create_family(&first, now()).await.expect("建族必须成功");

    let err = refreshes
        .rotate(
            "deadbeef",
            &RefreshToken::generate().to_hash_hex(),
            now() + TimeDelta::days(30),
            now(),
        )
        .await
        .expect_err("未知 token 必须被拒");
    assert!(matches!(err, RotateRefreshError::Invalid));
}

#[tokio::test]
async fn expired_active_token_is_invalid() {
    let (_users, refreshes, user, _dir) = setup().await;
    let family = Uuid::now_v7();
    // 已过期的 active token
    let expired = fresh_entry(user, family, now() - TimeDelta::seconds(1));
    refreshes.create_family(&expired, now()).await.expect("建族必须成功");

    let err = refreshes
        .rotate(
            &expired.token_hash,
            &RefreshToken::generate().to_hash_hex(),
            now() + TimeDelta::days(30),
            now(),
        )
        .await
        .expect_err("过期 token 必须被拒");
    assert!(matches!(err, RotateRefreshError::Invalid));
}

#[tokio::test]
async fn revoke_all_for_user_blocks_only_that_user() {
    let (users, refreshes, alice, _dir) = setup().await;
    let bob = UserId::new();
    users
        .insert(&seed_user(bob), &PasswordHash::hash("Nebula!Passw0rd-2026").expect("哈希必须成功"))
        .await
        .expect("第二个用户必须可插入");

    let alice_entry = fresh_entry(alice, Uuid::now_v7(), now() + TimeDelta::days(30));
    let bob_entry = fresh_entry(bob, Uuid::now_v7(), now() + TimeDelta::days(30));
    refreshes.create_family(&alice_entry, now()).await.expect("建族必须成功");
    refreshes.create_family(&bob_entry, now()).await.expect("建族必须成功");

    refreshes.revoke_all_for_user(&alice, now()).await.expect("批量撤销必须成功");

    let alice_err = refreshes
        .rotate(
            &alice_entry.token_hash,
            &RefreshToken::generate().to_hash_hex(),
            now() + TimeDelta::days(30),
            now(),
        )
        .await
        .expect_err("被撤销用户的 token 不可轮转");
    assert!(matches!(alice_err, RotateRefreshError::Invalid));

    refreshes
        .rotate(
            &bob_entry.token_hash,
            &RefreshToken::generate().to_hash_hex(),
            now() + TimeDelta::days(30),
            now(),
        )
        .await
        .expect("他人家族不受影响，必须可轮转");
}
