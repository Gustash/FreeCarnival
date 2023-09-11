use chrono::NaiveDateTime;
use serde::{Deserialize, Serialize};

use crate::{
    config::{LibraryConfig, UserConfig},
    constants::BASE_URL,
};

#[derive(Deserialize, Debug)]
pub(crate) struct LoginResult {
    pub(crate) status: String,
    pub(crate) message: String,
}

pub(crate) struct SyncResult {
    pub(crate) user_config: UserConfig,
    pub(crate) library_config: LibraryConfig,
}

#[derive(Deserialize, Serialize, Debug)]
pub(crate) struct UserInfo {
    status: String,
    user_found: String,
    #[serde(alias = "_indiegala_user_email")]
    email: Option<String>,
    #[serde(alias = "_indiegala_username")]
    username: Option<String>,
    #[serde(alias = "_indiegala_user_id")]
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
    pub(crate) version: Vec<ProductVersion>,
}

impl Product {
    pub(crate) fn get_latest_version(&self, os: Option<&BuildOs>) -> Option<&ProductVersion> {
        self.version.iter().fold(None, |acc, version| {
            let valid_os = match os {
                Some(build_os) => version.os == *build_os,
                #[cfg(target_os = "macos")]
                None => v.os == BuildOs::Mac,
                #[cfg(not(target_os = "macos"))]
                None => version.os == BuildOs::Windows,
            };
            if !valid_os {
                return acc;
            }

            match acc {
                Some(v) => {
                    if version.date > v.date {
                        Some(version)
                    } else {
                        acc
                    }
                }
                None => Some(version),
            }
        })
    }
}

#[derive(Deserialize, Serialize, Debug, Clone)]
pub(crate) struct ProductVersion {
    pub(crate) status: u16,
    pub(crate) enabled: u8,
    pub(crate) version: String,
    pub(crate) os: BuildOs,
    pub(crate) date: NaiveDateTime,
    pub(crate) text: String,
}

#[derive(Debug, Serialize, Deserialize, PartialEq, Clone)]
pub(crate) enum BuildOs {
    #[serde(rename = "win")]
    Windows,
    #[serde(rename = "lin")]
    Linux,
    #[serde(rename = "mac")]
    Mac,
}

impl Default for BuildOs {
    fn default() -> Self {
        Self::Windows
    }
}

impl std::fmt::Display for BuildOs {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(
            f,
            "{}",
            match self {
                BuildOs::Windows => "win",
                BuildOs::Linux => "lin",
                BuildOs::Mac => "mac",
            }
        )
    }
}

impl std::fmt::Display for Product {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "[{}] {}", self.slugged_name, self.name)
    }
}

impl std::fmt::Display for ProductVersion {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "[{}]\n", self.version)?;
        write!(f, "Build Date: {}\n", self.date)?;
        write!(
            f,
            "Platform: {}",
            match self.os {
                BuildOs::Windows => "Windows",
                BuildOs::Linux => "Linux",
                BuildOs::Mac => "macOS",
            }
        )?;
        if !self.text.is_empty() {
            write!(f, "\nAbout:\n\n{}", self.text)?;
        }

        Ok(())
    }
}

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
