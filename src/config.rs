use confy::ConfyError;
use serde::{Deserialize, Serialize};

use crate::api::auth::{Product, UserInfo};

pub(crate) trait GalaConfig
where
    Self: Sized,
{
    fn load() -> Result<Self, ConfyError>;

    fn store(&self) -> Result<(), ConfyError>;

    fn clear() -> Result<(), ConfyError>;
}

#[derive(Default, Debug, Serialize, Deserialize)]
pub(crate) struct UserConfig {
    pub(crate) user_info: Option<UserInfo>,
}

impl GalaConfig for UserConfig {
    fn load() -> Result<UserConfig, ConfyError> {
        confy::load::<UserConfig>("openGala", "user")
    }

    fn store(&self) -> Result<(), ConfyError> {
        confy::store("openGala", "user", self)
    }

    fn clear() -> Result<(), ConfyError> {
        confy::store("openGala", "user", UserConfig::default())
    }
}

#[derive(Default, Debug, Serialize, Deserialize)]
pub(crate) struct CookieConfig {
    pub(crate) cookies: Vec<String>,
}

impl GalaConfig for CookieConfig {
    fn load() -> Result<CookieConfig, ConfyError> {
        confy::load::<CookieConfig>("openGala", "cookies")
    }

    fn store(&self) -> Result<(), ConfyError> {
        confy::store("openGala", "cookies", self)
    }

    fn clear() -> Result<(), ConfyError> {
        confy::store("openGala", "cookies", CookieConfig::default())
    }
}

#[derive(Default, Debug, Serialize, Deserialize)]
pub(crate) struct LibraryConfig(pub(crate) Vec<Product>);

impl GalaConfig for LibraryConfig {
    fn load() -> Result<LibraryConfig, ConfyError> {
        confy::load::<LibraryConfig>("openGala", "library")
    }

    fn store(&self) -> Result<(), ConfyError> {
        confy::store("openGala", "library", self)
    }

    fn clear() -> Result<(), ConfyError> {
        confy::store("openGala", "library", LibraryConfig::default())
    }
}
