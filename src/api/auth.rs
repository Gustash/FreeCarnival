use cookie::Cookie;
use reqwest::header;
use serde::Deserialize;

use crate::{api::GalaRequest, constants::BASE_URL};

#[derive(Deserialize, Debug)]
struct UserInfoResponse {
    status: String,
    user_found: String,
    #[serde(alias = "_indiegala_user_email")]
    email: Option<String>,
    #[serde(alias = "_indiegala_username")]
    username: Option<String>,
    #[serde(alias = "_indiegala_user_id")]
    user_id: Option<u64>,
    showcase_content: Option<ShowcaseContentRoot>,
}

#[derive(Deserialize, Debug)]
struct ShowcaseContentRoot {
    content: ShowcaseContent,
}

#[derive(Deserialize, Debug)]
struct ShowcaseContent {
    user_collection: Vec<Product>,
}

#[derive(Deserialize, Debug)]
struct Product {
    prod_dev_namespace: String,
    prod_slugged_name: String,
    id: u64,
    prod_name: String,
}

#[derive(Debug)]
pub(crate) struct UserInfo {
    email: String,
    username: String,
    user_id: u64,
}

pub(crate) async fn login(
    username: &String,
    password: &String,
) -> Result<Option<UserInfo>, reqwest::Error> {
    let params = [("usre", username), ("usrp", password)];
    let gala_req = GalaRequest::new();
    let client = &gala_req.client;
    let res = client
        .post(format!("{}/login_new/gcl", *BASE_URL))
        .form(&params)
        .send()
        .await?;
    let cookie = res
        .headers()
        .get_all(header::SET_COOKIE)
        .iter()
        .fold(String::new(), |a, b| {
            let cookie = Cookie::parse(b.to_str().unwrap()).unwrap();
            let cookie = format!("{}={}", cookie.name(), cookie.value());

            if a.is_empty() {
                cookie
            } else {
                a + "; " + &cookie
            }
        });
    gala_req.save_cookies();
    let res = client
        .get(format!("{}/login_new/user_info", *BASE_URL))
        .header(header::COOKIE, cookie)
        .header(header::USER_AGENT, "galaClient")
        .send()
        .await?;

    let body = res.text().await?;
    match serde_json::from_str::<UserInfoResponse>(&body) {
        Ok(user_info) => {
            if user_info.status != "success" || user_info.user_found != "true" {
                return Ok(None);
            }

            Ok(Some(UserInfo {
                email: user_info.email.unwrap(),
                user_id: user_info.user_id.unwrap(),
                username: user_info.username.unwrap(),
            }))
        }
        Err(err) => {
            println!("Failed to parse response: {:#?}", err);
            Ok(None)
        }
    }
}
