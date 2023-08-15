use reqwest::header;
use serde::Serialize;
use serde_query::Deserialize;

use crate::{api::GalaRequest, config::UserConfig, constants::BASE_URL, prelude::*};

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
    #[query(".showcase_content.content.user_collection")]
    user_collection: Option<Vec<Product>>,
}

#[derive(Deserialize, Serialize, Debug)]
struct Product {
    #[query(".prod_dev_namespace")]
    prod_dev_namespace: String,
    #[query(".prod_slugged_name")]
    prod_slugged_name: String,
    #[query(".id")]
    id: u64,
    #[query(".prod_name")]
    prod_name: String,
}

pub(crate) async fn login(
    username: &String,
    password: &String,
) -> Result<Option<UserConfig>, reqwest::Error> {
    let params = [("usre", username), ("usrp", password)];
    let gala_req = GalaRequest::new();
    let client = &gala_req.client;
    let res = client
        .post(format!("{}/login_new/gcl", *BASE_URL))
        .form(&params)
        .send()
        .await?;
    let raw_cookies = res
        .headers()
        .get_all(header::SET_COOKIE)
        .iter()
        .map(|c| c.to_str().unwrap().to_string())
        .collect::<Vec<String>>();
    let cookie = res.headers().to_cookie_str();
    gala_req.save_cookies();
    let res = client
        .get(format!("{}/login_new/user_info", *BASE_URL))
        .header(header::COOKIE, cookie)
        .header(header::USER_AGENT, "galaClient")
        .send()
        .await?;

    let body = res.text().await?;
    match serde_json::from_str::<UserInfo>(&body) {
        Ok(user_info) => {
            if user_info.status != "success" || user_info.user_found != "true" {
                return Ok(None);
            }

            Ok(Some(UserConfig {
                auth: Some(crate::config::UserConfigAuth {
                    cookies: raw_cookies,
                }),
                user_info: Some(user_info),
            }))
        }
        Err(err) => {
            println!("Failed to parse response: {:#?}", err);
            Ok(None)
        }
    }
}
