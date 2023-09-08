use std::collections::HashMap;

use confy::ConfyError;
use reqwest_cookie_store::CookieStore;
use serde::{de::DeserializeOwned, Deserialize, Serialize};

use crate::{
    api::auth::{Product, UserInfo},
    constants::PROJECT_NAME,
    shared::models::InstallInfo,
};

pub(crate) trait GalaConfig
where
    Self: Sized + Serialize + DeserializeOwned + Default,
{
    fn load() -> Result<Self, ConfyError> {
        confy::load::<Self>(*PROJECT_NAME, Self::config_name())
    }

    fn store(&self) -> Result<(), ConfyError> {
        confy::store(*PROJECT_NAME, Self::config_name(), self)
    }

    fn clear() -> Result<(), ConfyError> {
        confy::store(*PROJECT_NAME, Self::config_name(), Self::default())
    }

    fn config_name() -> &'static str;
}

#[derive(Default, Debug, Serialize, Deserialize)]
pub(crate) struct UserConfig {
    pub(crate) user_info: Option<UserInfo>,
}

impl GalaConfig for UserConfig {
    fn config_name() -> &'static str {
        "user"
    }
}

#[derive(Default, Debug, Serialize, Deserialize)]
pub(crate) struct CookieConfig(pub(crate) CookieStore);

impl GalaConfig for CookieConfig {
    fn config_name() -> &'static str {
        "cookies"
    }
}

#[derive(Default, Debug, Serialize, Deserialize)]
pub(crate) struct LibraryConfig {
    pub(crate) collection: Vec<Product>,
}

impl GalaConfig for LibraryConfig {
    fn config_name() -> &'static str {
        "library"
    }
}

pub(crate) type InstalledConfig = HashMap<String, InstallInfo>;

impl GalaConfig for InstalledConfig {
    fn config_name() -> &'static str {
        "installed"
    }
}
