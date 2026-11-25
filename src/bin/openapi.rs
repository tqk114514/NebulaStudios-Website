//! 把 `OpenAPI` 文档打印到 stdout，供前端 `npm run codegen:api` 生成类型与 client。
//!
//! 不启动服务、不连数据库；只读编译期登记的 `ApiDoc`。

use utoipa::OpenApi;

fn main() -> anyhow::Result<()> {
    let document = nebula_server::api::openapi::ApiDoc::openapi();
    println!("{}", serde_json::to_string_pretty(&document)?);
    Ok(())
}
