use std::{fs, io::BufWriter};

use reqwest::header::{self, HeaderMap};

pub(crate) mod auth;

pub(crate) struct GalaRequest {
    client: reqwest::Client,
}

impl GalaRequest {
    fn new() -> GalaRequest {
        let mut default_headers = HeaderMap::new();
        default_headers.insert(
            header::CONTENT_TYPE,
            "application/x-www-form-urlencoded".parse().unwrap(),
        );
        // TODO: Create custom CookieStore
        let client = reqwest::Client::builder()
            .default_headers(default_headers)
            .user_agent("galaClient")
            .cookie_store(true)
            .use_rustls_tls()
            .build()
            .unwrap();

        GalaRequest { client }
    }

    fn save_cookies(&self) {
        fs::create_dir_all("/home/gustash/.config/openGala")
            .expect("Failed to create config directory");
        let mut writer = fs::File::create("/home/gustash/.config/openGala/cookies.json")
            .map(BufWriter::new)
            .unwrap();
        // TODO: Save cookies in JSON file
    }
}
