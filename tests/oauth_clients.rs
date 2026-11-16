//! OAuth Provider 客户端登记测试：按 `client_id` upsert，保留创建时间。
#![allow(clippy::expect_used, clippy::unwrap_used)]

mod common;

use nebula_server::model::oauth2::OAuthClient;

#[tokio::test]
async fn upsert_client_creates_then_updates_by_id() {
    let (state, _clock, _mailbox, _dir) = common::app_state().await;
    let now = state.clock.now();
    let client = OAuthClient {
        id: "client-1".to_owned(),
        name: "One".to_owned(),
        redirect_uri: "https://one.example/cb".to_owned(),
        enabled: true,
    };

    state.oauth_repo.upsert_client(&client, "hash-one", now).await.expect("客户端必须可插入");
    let found = state.oauth_repo.find_client("client-1").await.expect("查找必须成功");
    assert_eq!(found.expect("客户端必须存在").name, "One");
    assert_eq!(
        state.oauth_repo.find_client_secret("client-1").await.expect("查找必须成功"),
        Some("hash-one".to_owned())
    );

    let updated = OAuthClient {
        id: "client-1".to_owned(),
        name: "Two".to_owned(),
        redirect_uri: "https://two.example/cb".to_owned(),
        enabled: false,
    };
    state.oauth_repo.upsert_client(&updated, "hash-two", now).await.expect("客户端必须可更新");
    let found = state
        .oauth_repo
        .find_client("client-1")
        .await
        .expect("查找必须成功")
        .expect("客户端必须存在");
    assert_eq!(found.name, "Two");
    assert_eq!(found.redirect_uri, "https://two.example/cb");
    assert!(!found.enabled);
    assert_eq!(
        state.oauth_repo.find_client_secret("client-1").await.expect("查找必须成功"),
        Some("hash-two".to_owned())
    );
}
