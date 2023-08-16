use reqwest::header::HeaderMap;
use serde::Serialize;
use serde_query::Deserialize;

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
    #[query(".status")]
    status: String,
    #[query(".user_found")]
    user_found: String,
    #[query("._indiegala_user_email")]
    email: Option<String>,
    #[query("._indiegala_username")]
    username: Option<String>,
    #[query("._indiegala_user_id")]
    user_id: Option<u64>,
}

#[derive(Deserialize, Serialize, Debug)]
pub(crate) struct UserShowcaseContent {
    #[query(".showcase_content.content.user_collection")]
    user_collection: Option<Vec<Product>>,
}

#[derive(Deserialize, Serialize, Debug)]
pub(crate) struct Product {
    #[query(".prod_dev_namespace")]
    prod_dev_namespace: String,
    #[query(".prod_slugged_name")]
    prod_slugged_name: String,
    #[query(".id")]
    id: u64,
    #[query(".prod_name")]
    prod_name: String,
}

impl std::fmt::Display for Product {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(
            f,
            "[{}]\t{} ({})",
            self.prod_slugged_name, self.prod_name, self.id
        )
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
            let user_collection = match serde_json::from_str::<UserShowcaseContent>(&body) {
                Ok(showcase) => showcase.user_collection,
                Err(_) => Some(vec![]),
            };

            Ok(Some(SyncResult {
                library_config: LibraryConfig {
                    collection: match user_collection {
                        Some(collection) => collection,
                        None => vec![],
                    },
                },
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
