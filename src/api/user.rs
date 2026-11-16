//! 用户域端点（§2 vertical slice）：账户自身资料与两步验证管理。

pub mod avatar_serve;
pub mod avatar_upload;
pub mod consents;
pub mod email_change;
pub mod export_download;
pub mod export_request;
pub mod oauth_grants;
pub mod profile;
pub mod social;
pub mod totp_disable;
pub mod totp_enable;
pub mod totp_setup;
pub mod username;
