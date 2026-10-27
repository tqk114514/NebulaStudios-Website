//! refresh 家族仓储在真实 SQLite 上测：轮转、重放整族撤销、绑定校验、批量撤销
//! （ADR-0003、ADR-0006、§8.1）。
#![allow(clippy::expect_used, clippy::unwrap_used)]

mod common;

use chrono::{DateTime, TimeDelta, Utc};
use nebula_server::model::password::PasswordHash;
use nebula_server::model::tokens::{NewRefreshFamily, RefreshToken};
use nebula_server::model::user::{User, UserId};
use nebula_server::storage::{ClientBinding, RefreshTokenRepo, RotateRefreshError, UserRepo};
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

/// 生成新家族描述；明文 token 只以哈希形态入结构。
fn fresh_family(
    user_id: UserId,
    family_id: Uuid,
    expires_at: DateTime<Utc>,
    client_id: Option<&str>,
    scope: &str,
) -> NewRefreshFamily {
    NewRefreshFamily {
        family_id,
        user_id,
        client_id: client_id.map(ToOwned::to_owned),
        scope: scope.to_owned(),
        token_hash: RefreshToken::generate().to_hash_hex(),
        expires_at,
    }
}

/// 第三方家族的外键要求客户端行存在。
async fn seed_client(dir: &std::path::Path) {
    let pool = nebula_server::storage::sqlite::connect_pool(&dir.join("users.db"))
        .await
        .expect("测试库必须可打开");
    sqlx::query(
        "INSERT INTO oauth_clients (id, name, redirect_uri, created_at)
         VALUES ('client-1', 'Test Client', 'https://app.example.com/cb', '2026-10-26T00:00:00+00:00')",
    )
    .execute(&pool)
    .await
    .expect("客户端必须可插入");
    pool.close().await;
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
    let first = fresh_family(user, family, now() + TimeDelta::days(30), None, "");
    refreshes.create_family(&first, now()).await.expect("建族必须成功");

    let second_hash = RefreshToken::generate().to_hash_hex();
    let session = refreshes
        .rotate(
            &first.token_hash,
            &second_hash,
            now() + TimeDelta::days(30),
            now(),
            ClientBinding::FirstParty,
        )
        .await
        .expect("第一次轮转必须成功");
    assert_eq!(session.user_id, user);
    assert_eq!(session.scope, "");

    let third_hash = RefreshToken::generate().to_hash_hex();
    let rotated_user = refreshes
        .rotate(
            &second_hash,
            &third_hash,
            now() + TimeDelta::days(30),
            now(),
            ClientBinding::FirstParty,
        )
        .await
        .expect("链上的第二次轮转必须成功");
    assert_eq!(rotated_user.user_id, user);
}

#[tokio::test]
async fn rotation_returns_family_scope_and_enforces_client_binding() {
    let (_users, refreshes, user, dir) = setup().await;
    seed_client(dir.path()).await;

    let family = Uuid::now_v7();
    let first =
        fresh_family(user, family, now() + TimeDelta::days(30), Some("client-1"), "profile");
    refreshes.create_family(&first, now()).await.expect("建族必须成功");

    // 另一个客户端不能兑换；绑定不符时 token 仍保持 active
    let second_hash = RefreshToken::generate().to_hash_hex();
    let mismatch = refreshes
        .rotate(
            &first.token_hash,
            &second_hash,
            now() + TimeDelta::days(30),
            now(),
            ClientBinding::Client("client-2"),
        )
        .await
        .expect_err("其他客户端必须被拒");
    assert!(matches!(mismatch, RotateRefreshError::ClientMismatch));

    // 第一方绑定同样不能兑换第三方家族
    let first_party = refreshes
        .rotate(
            &first.token_hash,
            &RefreshToken::generate().to_hash_hex(),
            now() + TimeDelta::days(30),
            now(),
            ClientBinding::FirstParty,
        )
        .await
        .expect_err("第一方绑定不能兑换第三方家族");
    assert!(matches!(first_party, RotateRefreshError::ClientMismatch));

    // 正确客户端兑换成功，scope 来自家族
    let session = refreshes
        .rotate(
            &first.token_hash,
            &second_hash,
            now() + TimeDelta::days(30),
            now(),
            ClientBinding::Client("client-1"),
        )
        .await
        .expect("归属客户端必须可兑换");
    assert_eq!(session.user_id, user);
    assert_eq!(session.scope, "profile");

    // 第一方家族反过来不能被客户端绑定兑换
    let personal = fresh_family(user, Uuid::now_v7(), now() + TimeDelta::days(30), None, "");
    refreshes.create_family(&personal, now()).await.expect("第一方家族必须可建");
    let client_on_personal = refreshes
        .rotate(
            &personal.token_hash,
            &RefreshToken::generate().to_hash_hex(),
            now() + TimeDelta::days(30),
            now(),
            ClientBinding::Client("client-1"),
        )
        .await
        .expect_err("客户端不能兑换第一方家族");
    assert!(matches!(client_on_personal, RotateRefreshError::ClientMismatch));
}

#[tokio::test]
async fn replaying_rotated_token_revokes_whole_family() {
    let (_users, refreshes, user, _dir) = setup().await;
    let family = Uuid::now_v7();
    let first = fresh_family(user, family, now() + TimeDelta::days(30), None, "");
    refreshes.create_family(&first, now()).await.expect("建族必须成功");

    let second_hash = RefreshToken::generate().to_hash_hex();
    refreshes
        .rotate(
            &first.token_hash,
            &second_hash,
            now() + TimeDelta::days(30),
            now(),
            ClientBinding::FirstParty,
        )
        .await
        .expect("正常轮转必须成功");

    // 重放已轮转的 first：判定重放，整族撤销
    let replay = refreshes
        .rotate(
            &first.token_hash,
            &RefreshToken::generate().to_hash_hex(),
            now() + TimeDelta::days(30),
            now(),
            ClientBinding::FirstParty,
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
            ClientBinding::FirstParty,
        )
        .await
        .expect_err("已撤销家族内的 token 不可再轮转");
    assert!(matches!(after, RotateRefreshError::Invalid));
}

#[tokio::test]
async fn unknown_hash_is_invalid() {
    let (_users, refreshes, user, _dir) = setup().await;
    let family = Uuid::now_v7();
    let first = fresh_family(user, family, now() + TimeDelta::days(30), None, "");
    refreshes.create_family(&first, now()).await.expect("建族必须成功");

    let err = refreshes
        .rotate(
            "deadbeef",
            &RefreshToken::generate().to_hash_hex(),
            now() + TimeDelta::days(30),
            now(),
            ClientBinding::FirstParty,
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
    let expired = fresh_family(user, family, now() - TimeDelta::seconds(1), None, "");
    refreshes.create_family(&expired, now()).await.expect("建族必须成功");

    let err = refreshes
        .rotate(
            &expired.token_hash,
            &RefreshToken::generate().to_hash_hex(),
            now() + TimeDelta::days(30),
            now(),
            ClientBinding::FirstParty,
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

    let alice_entry = fresh_family(alice, Uuid::now_v7(), now() + TimeDelta::days(30), None, "");
    let bob_entry = fresh_family(bob, Uuid::now_v7(), now() + TimeDelta::days(30), None, "");
    refreshes.create_family(&alice_entry, now()).await.expect("建族必须成功");
    refreshes.create_family(&bob_entry, now()).await.expect("建族必须成功");

    refreshes.revoke_all_for_user(&alice, now()).await.expect("批量撤销必须成功");

    let alice_err = refreshes
        .rotate(
            &alice_entry.token_hash,
            &RefreshToken::generate().to_hash_hex(),
            now() + TimeDelta::days(30),
            now(),
            ClientBinding::FirstParty,
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
            ClientBinding::FirstParty,
        )
        .await
        .expect("他人家族不受影响，必须可轮转");
}
