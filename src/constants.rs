use std::path::PathBuf;

use directories::UserDirs;
use lazy_static::lazy_static;

lazy_static! {
    pub(crate) static ref BASE_URL: &'static str = "https://www.indiegala.com";
    pub(crate) static ref CONTENT_URL: &'static str = "https://content.indiegalacdn.com";
    pub(crate) static ref MAX_CHUNK_SIZE: usize = 1048576; // 1 MiB
    pub(crate) static ref DEFAULT_MAX_DL_WORKERS: usize = std::cmp::min(num_cpus::get() * 2, 16);
    pub(crate) static ref DEFAULT_MAX_MEMORY_USAGE: usize = *MAX_CHUNK_SIZE * 1024; // 1 GiB
    pub(crate) static ref DEFAULT_BASE_INSTALL_PATH: PathBuf = UserDirs::new().expect("Failed to retrieve home directory.").home_dir().join("Games").join("opengala");
}
