use serde::{Deserialize, Serialize};

use crate::api::auth::UserInfo;

#[derive(Default, Debug, Serialize, Deserialize)]
pub(crate) struct UserConfig {
    pub(crate) user_info: Option<UserInfo>,
    pub(crate) auth: Option<UserConfigAuth>,
}

#[derive(Default, Debug, Serialize, Deserialize)]
pub(crate) struct UserConfigAuth {
    pub(crate) cookies: Vec<String>,
}
