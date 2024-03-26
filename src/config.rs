use std::collections::HashMap;

use confy::ConfyError;
use reqwest_cookie_store::CookieStore;
use serde::{de::DeserializeOwned, Deserialize, Serialize};
use std::path::{Path, PathBuf};

use crate::{
    constants::CONFIG_PATH,
    constants::PROJECT_NAME,
    shared::models::{
        api::{Product, UserInfo},
        InstallInfo,
    },
};

pub(crate) trait GalaConfig
where
    Self: Sized + Serialize + DeserializeOwned + Default,
{
    fn load() -> Result<Self, ConfyError> {
        confy::load_path::<Self>(Self::get_config_path())
    }

    fn store(&self) -> Result<(), ConfyError> {
        confy::store_path(Self::get_config_path(), self)
    }

    fn clear() -> Result<(), ConfyError> {
        confy::store_path(Self::get_config_path(), Self::default())
    }

    fn config_name() -> &'static str;

    fn get_config_path() -> PathBuf {
        if !CONFIG_PATH.is_empty() {
            Path::new(&(*CONFIG_PATH))
                .join(format!("{}.yml", Self::config_name()))
                .to_path_buf()
        } else {
            match confy::get_configuration_file_path(*PROJECT_NAME, Self::config_name()) {
                Ok(p) => PathBuf::from(p.to_str().unwrap_or_default()).to_owned(),
                Err(_e) => panic!("Can't get config path for {}", Self::config_name()),
            }
        }
    }
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
