use crate::{
    config::{LibraryConfig, UserConfig},
    constants::BASE_URL,
    shared::models::api::{LoginResult, SyncResult, UserInfo, UserInfoShowcaseContent},
};

pub(crate) async fn login(
    client: &reqwest::Client,
    username: &String,
    password: &String,
) -> Result<Option<LoginResult>, reqwest::Error> {
    let params = [("usre", username), ("usrp", password)];
    let res = client
        .post(format!("{}/login_new/gcl", *BASE_URL))
        .form(&params)
        .send()
        .await?;
    let body = res.text().await?;

    match serde_json::from_str::<LoginResult>(&body) {
        Ok(login) => Ok(Some(login)),
        Err(_) => Ok(None),
    }
}

pub(crate) async fn sync(client: &reqwest::Client) -> Result<Option<SyncResult>, reqwest::Error> {
    let res = client
        .get(format!("{}/login_new/user_info", *BASE_URL))
        .send()
        .await?;

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
                library_config: LibraryConfig {
                    collection: user_collection,
                },
                user_config: UserConfig {
                    user_info: Some(user_info),
                },
            }))
        }
        Err(_) => {
            println!("Failed to sync data. Are you logged in?");
            Ok(None)
        }
    }
}
