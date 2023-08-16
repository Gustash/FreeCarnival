use cookie::Cookie;
use reqwest::header::{self, HeaderMap};

use crate::config::{CookieConfig, GalaConfig};

pub(crate) mod auth;
pub(crate) mod product;

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
        if let Ok(cookie_config) = CookieConfig::load() {
            let cookie_header = cookie_config
                .cookies
                .iter()
                .map(|c| Cookie::parse(c).unwrap())
                .fold(String::new(), |a, b| {
                    let cookie = format!("{}={}", b.name(), b.value());

                    if a.is_empty() {
                        cookie
                    } else {
                        a + "; " + &cookie
                    }
                });
            default_headers.insert(header::COOKIE, cookie_header.parse().unwrap());
        }
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
}
