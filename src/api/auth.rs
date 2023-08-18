use reqwest::header::HeaderMap;
use serde::{Deserialize, Serialize};

use crate::{
    api::GalaRequest,
    config::{CookieConfig, GalaConfig, LibraryConfig, UserConfig},
    constants::BASE_URL,
    prelude::*,
};

pub(crate) struct SyncResult {
    pub(crate) user_config: UserConfig,
    pub(crate) cookie_config: CookieConfig,
    pub(crate) library_config: LibraryConfig,
}

#[derive(Deserialize, Serialize, Debug)]
pub(crate) struct UserInfo {
    status: String,
    user_found: String,
    #[serde(alias = "._indiegala_user_email")]
    email: Option<String>,
    #[serde(alias = "._indiegala_username")]
    username: Option<String>,
    #[serde(alias = "._indiegala_user_id")]
    user_id: Option<u64>,
}

#[derive(Deserialize, Debug)]
struct UserInfoShowcaseContent {
    showcase_content: Option<ShowcaseContent>,
}

#[derive(Deserialize, Debug)]
struct ShowcaseContent {
    content: Content,
}

#[derive(Deserialize, Debug)]
struct Content {
    user_collection: Vec<Product>,
}

#[derive(Deserialize, Serialize, Debug, Clone)]
pub(crate) struct Product {
    #[serde(alias = "prod_dev_namespace")]
    pub(crate) namespace: String,
    #[serde(alias = "prod_slugged_name")]
    pub(crate) slugged_name: String,
    pub(crate) id: u64,
    #[serde(alias = "prod_name")]
    pub(crate) name: String,
    #[serde(alias = "prod_id_key_name")]
    pub(crate) id_key_name: String,
}

impl std::fmt::Display for Product {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "[{}]\t{} ({})", self.slugged_name, self.name, self.id)
    }
}

pub(crate) async fn login(
    username: &String,
    password: &String,
) -> Result<Option<SyncResult>, reqwest::Error> {
    let params = [("usre", username), ("usrp", password)];
    let gala_req = GalaRequest::new();
    let client = &gala_req.client;
    let res = client
        .post(format!("{}/login_new/gcl", *BASE_URL))
        .form(&params)
        .send()
        .await?;
    let raw_cookies = get_raw_cookies(res.headers());
    CookieConfig {
        cookies: raw_cookies,
    }
    .store()
    .expect("Failed to save cookie config");

    sync().await
}

pub(crate) async fn sync() -> Result<Option<SyncResult>, reqwest::Error> {
    let client = GalaRequest::new().client;
    let res = client
        .get(format!("{}/login_new/user_info", *BASE_URL))
        .send()
        .await?;

    let raw_cookies = get_raw_cookies(res.headers());
    let body = res.text().await?;

    match serde_json::from_str::<UserInfo>(&body) {
        Ok(user_info) => {
            if user_info.status != "success" || user_info.user_found != "true" {
                return Ok(None);
            }
            let user_collection = match serde_json::from_str::<UserInfoShowcaseContent>(&body) {
                Ok(user_info) => match user_info.showcase_content {
                    Some(showcase) => showcase.content.user_collection,
                    None => vec![],
                },
                Err(err) => {
                    println!("Failed to parse user library: {err:?}");
                    vec![]
                }
            };

            Ok(Some(SyncResult {
                library_config: LibraryConfig(user_collection),
                user_config: UserConfig {
                    user_info: Some(user_info),
                },
                cookie_config: CookieConfig {
                    cookies: raw_cookies,
                },
            }))
        }
        Err(_) => {
            println!("Failed to sync data. Are you logged in?");
            Ok(None)
        }
    }
}

fn get_raw_cookies(headers: &HeaderMap) -> Vec<String> {
    headers
        .to_cookie()
        .iter()
        .filter(|c| c.expires() > Some(time::now()))
        .map(|c| c.to_string())
        .collect::<Vec<String>>()
}
