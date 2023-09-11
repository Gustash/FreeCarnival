use std::path::PathBuf;

use serde::{Deserialize, Serialize};

use crate::api::auth::BuildOs;

#[derive(Debug, Serialize, Deserialize)]
pub(crate) struct InstallInfo {
    /// Directory where game was installed to
    pub(crate) install_path: PathBuf,
    /// Version of the game that is installed
    pub(crate) version: String,
    /// OS the build is for
    #[serde(default)]
    pub(crate) os: BuildOs,
}

impl InstallInfo {
    pub(crate) fn new(install_path: PathBuf, version: String, os: BuildOs) -> InstallInfo {
        InstallInfo {
            install_path,
            version,
            os,
        }
    }
}
