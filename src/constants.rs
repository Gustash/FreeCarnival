use std::path::PathBuf;

use directories::UserDirs;
use lazy_static::lazy_static;
use reqwest::header::{self, HeaderMap};

lazy_static! {
    pub(crate) static ref BASE_URL: &'static str = "https://www.indiegala.com";
    pub(crate) static ref CONTENT_URL: &'static str = "https://content.indiegalacdn.com";
    pub(crate) static ref DEV_URL: &'static str = "https://developers.indiegala.com";
    pub(crate) static ref MAX_CHUNK_SIZE: usize = 1048576; // 1 MiB
    pub(crate) static ref DEFAULT_MAX_DL_WORKERS: usize = std::cmp::min(num_cpus::get() * 2, 16);
    pub(crate) static ref DEFAULT_MAX_MEMORY_USAGE: usize = *MAX_CHUNK_SIZE * 1024; // 1 GiB
    pub(crate) static ref DEFAULT_BASE_INSTALL_PATH: PathBuf = UserDirs::new().expect("Failed to retrieve home directory.").home_dir().join("Games").join(*PROJECT_NAME);
    pub(crate) static ref PROJECT_NAME: &'static str = env!("CARGO_PKG_NAME");
    pub(crate) static ref PROJECT_VERSION: &'static str = env!("CARGO_PKG_VERSION");
    pub(crate) static ref VERSION_CODENAME: &'static str = include_str!("../CODENAME");
    pub(crate) static ref CONFIG_PATH: String = {
        match std::env::var("CARNIVAL_CONFIG_PATH") {
            Ok(p) => String::from(p),
            Err(_e) => "".to_string()
        }
    };
    pub(crate) static ref HELP_VERSION: &'static str = {
        Box::leak(format!("{} - {}", *PROJECT_VERSION, *VERSION_CODENAME).into_boxed_str())
    };
    pub(crate) static ref DEFAULT_HEADERS: HeaderMap = {
        let mut default_headers = HeaderMap::new();
        default_headers.insert(
            header::CONTENT_TYPE,
            "application/x-www-form-urlencoded".parse().unwrap(),
        );
        default_headers
    };
}
