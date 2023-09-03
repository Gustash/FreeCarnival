use std::path::PathBuf;

use serde::{Deserialize, Serialize};

#[derive(Debug, Serialize, Deserialize)]
pub(crate) struct InstallInfo {
    /// Directory where game was installed to
    pub(crate) install_path: PathBuf,
    /// Version of the game that is installed
    pub(crate) version: String,
}

impl InstallInfo {
    pub(crate) fn new(install_path: PathBuf, version: String) -> InstallInfo {
        InstallInfo {
            install_path,
            version,
        }
    }
}
