use lazy_static::lazy_static;

lazy_static! {
    pub(crate) static ref BASE_URL: &'static str = "https://www.indiegala.com";
    pub(crate) static ref CONTENT_URL: &'static str = "https://content.indiegalacdn.com";
    pub(crate) static ref MAX_CHUNK_SIZE: u64 = 1048576; // 1 MiB
    pub(crate) static ref DEFAULT_MAX_DL_WORKERS: usize = 1024;
}
