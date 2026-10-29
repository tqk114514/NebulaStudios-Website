//! 社交身份与 OAuth 中转态仓储测试：绑定唯一性、解绑、级联删除、state 单次消费
//! （ADR-0012、ADR-0007、§8.1 真实 SQLite）。
#![allow(clippy::expect_used, clippy::unwrap_used)]

mod common;

use chrono::{DateTime, TimeDelta, Utc};
use nebula_server::model::password::PasswordHash;
use nebula_server::model::social::{
    NewSocialIdentity, SocialLoginAction, SocialLoginState, SocialProvider,
};
use nebula_server::model::user::{User, UserId};
use nebula_server::storage::sqlite::{SqliteSocialRepo, SqliteUserRepo};
use nebula_server::storage::{LinkIdentityError, SocialRepo, UserRepo};
use sqlx::SqlitePool;

fn now() -> DateTime<Utc> {
    DateTime::parse_from_rfc3339("2026-10-26T08:00:00Z")
        .expect("固定字面量必须可解析")
        .with_timezone(&Utc)
}

async fn fixture() -> (SqliteSocialRepo, SqliteUserRepo, SqlitePool, tempfile::TempDir) {
    let dir = tempfile::tempdir().expect("临时目录必须可创建");
    let bootstrap = common::migrated_pool(dir.path()).await;
    bootstrap.close().await;
    // connect_pool 显式 foreign_keys=ON，级联删除测试依赖它（ADR-0003）
    let pool = nebula_server::storage::sqlite::connect_pool(&dir.path().join("users.db"))
        .await
        .expect("已迁移的库必须可打开");
    (SqliteSocialRepo::from_pool(pool.clone()), SqliteUserRepo::from_pool(pool.clone()), pool, dir)
}

async fn seed_user(users: &SqliteUserRepo, username: &str, email: &str) -> UserId {
    let user = User {
        id: UserId::new(),
        username: username.to_owned(),
        email: email.to_owned(),
        created_at: now(),
        email_verified: false,
    };
    users
        .insert(&user, &PasswordHash::hash("Nebula!Passw0rd-2026").expect("哈希必须成功"))
        .await
        .expect("种子用户必须可插入");
    user.id
}

fn identity(
    user_id: UserId,
    provider: SocialProvider,
    provider_user_id: &str,
) -> NewSocialIdentity {
    NewSocialIdentity {
        user_id,
        provider,
        provider_user_id: provider_user_id.to_owned(),
        display_name: Some("Nova".to_owned()),
    }
}

#[tokio::test]
async fn link_then_find_roundtrips() {
    let (social, users, _pool, _dir) = fixture().await;
    let user = seed_user(&users, "nova", "nova@nebula.st").await;

    social
        .link_identity(&identity(user, SocialProvider::Google, "google-123"), now())
        .await
        .expect("绑定必须成功");

    let found = social
        .find_identity(SocialProvider::Google, "google-123")
        .await
        .expect("查找必须成功")
        .expect("绑定必须存在");
    assert_eq!(found.user_id, user);
    assert_eq!(found.provider, SocialProvider::Google);
    assert_eq!(found.display_name.as_deref(), Some("Nova"));
    assert_eq!(found.created_at, now());
}

#[tokio::test]
async fn same_identity_cannot_bind_twice_even_to_another_user() {
    let (social, users, _pool, _dir) = fixture().await;
    let first = seed_user(&users, "nova", "nova@nebula.st").await;
    let second = seed_user(&users, "other", "other@nebula.st").await;

    social
        .link_identity(&identity(first, SocialProvider::Microsoft, "ms-1"), now())
        .await
        .expect("首次绑定必须成功");
    let again = social
        .link_identity(&identity(second, SocialProvider::Microsoft, "ms-1"), now())
        .await
        .expect_err("同一 provider 身份不得重复绑定");

    assert!(matches!(again, LinkIdentityError::Conflict));
    // 冲突不夺走原绑定
    let found = social
        .find_identity(SocialProvider::Microsoft, "ms-1")
        .await
        .expect("查找必须成功")
        .expect("原绑定必须仍在");
    assert_eq!(found.user_id, first);
}

#[tokio::test]
async fn same_id_across_providers_is_independent() {
    let (social, users, _pool, _dir) = fixture().await;
    let user = seed_user(&users, "nova", "nova@nebula.st").await;

    social
        .link_identity(&identity(user, SocialProvider::Google, "shared"), now())
        .await
        .expect("google 绑定必须成功");
    social
        .link_identity(&identity(user, SocialProvider::Microsoft, "shared"), now())
        .await
        .expect("microsoft 同 id 必须可绑定（唯一约束含 provider）");

    let all = social.list_for_user(&user).await.expect("列表必须成功");
    assert_eq!(all.len(), 2);
}

#[tokio::test]
async fn unlink_removes_binding_and_missing_is_false() {
    let (social, users, _pool, _dir) = fixture().await;
    let user = seed_user(&users, "nova", "nova@nebula.st").await;
    social
        .link_identity(&identity(user, SocialProvider::Google, "g-1"), now())
        .await
        .expect("绑定必须成功");

    assert!(social.unlink(&user, SocialProvider::Google).await.expect("解绑必须成功"));
    assert!(
        social.find_identity(SocialProvider::Google, "g-1").await.expect("查找必须成功").is_none()
    );
    assert!(!social.unlink(&user, SocialProvider::Google).await.expect("重复解绑必须成功返回"));
}

#[tokio::test]
async fn deleting_user_cascades_identities_and_states() {
    let (social, users, pool, _dir) = fixture().await;
    let user = seed_user(&users, "nova", "nova@nebula.st").await;
    social
        .link_identity(&identity(user, SocialProvider::Google, "g-1"), now())
        .await
        .expect("绑定必须成功");
    // link 用途的中转态带 user_id（CHECK 要求非空）
    social
        .put_state(
            &SocialLoginState {
                state: "state-link".to_owned(),
                provider: SocialProvider::Google,
                code_verifier: "verifier".to_owned(),
                action: SocialLoginAction::Link,
                user_id: Some(user),
                expires_at: now() + TimeDelta::minutes(10),
            },
            now(),
        )
        .await
        .expect("中转态必须可落库");

    sqlx::query("DELETE FROM users WHERE id = ?1")
        .bind(user.as_uuid())
        .execute(&pool)
        .await
        .expect("删除用户必须成功");

    let identities: i64 = sqlx::query_scalar("SELECT COUNT(*) FROM user_social_identities")
        .fetch_one(&pool)
        .await
        .unwrap();
    let states: i64 = sqlx::query_scalar("SELECT COUNT(*) FROM social_login_states")
        .fetch_one(&pool)
        .await
        .unwrap();
    assert_eq!(identities, 0, "身份必须随用户级联删除");
    assert_eq!(states, 0, "link 态必须随用户级联删除");
}

#[tokio::test]
async fn state_is_consumed_exactly_once() {
    let (social, _users, _pool, _dir) = fixture().await;
    social
        .put_state(
            &SocialLoginState {
                state: "nonce-1".to_owned(),
                provider: SocialProvider::Google,
                code_verifier: "verifier-1".to_owned(),
                action: SocialLoginAction::Login,
                user_id: None,
                expires_at: now() + TimeDelta::minutes(10),
            },
            now(),
        )
        .await
        .expect("中转态必须可落库");

    let first = social.take_state("nonce-1").await.expect("取出必须成功").expect("必须存在");
    assert_eq!(first.code_verifier, "verifier-1");
    assert_eq!(first.action, SocialLoginAction::Login);
    assert!(first.user_id.is_none());

    // 第二次：已消费
    assert!(social.take_state("nonce-1").await.expect("取出必须成功").is_none());
}
