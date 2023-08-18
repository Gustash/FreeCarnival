use cookie::Cookie;
use reqwest::header::{self, HeaderMap};

use crate::config::{CookieConfig, GalaConfig};

pub(crate) mod auth;
pub(crate) mod product;

pub(crate) struct GalaRequest {
    pub(crate) client: reqwest::Client,
}

impl GalaRequest {
    pub(crate) fn new() -> GalaRequest {
        let default_headers = Self::default_headers();
        let cookies = match CookieConfig::load() {
            Ok(cookie_config) => Some(
                cookie_config
                    .cookies
                    .into_iter()
                    .map(|c| Cookie::parse(c).unwrap())
                    .collect(),
            ),
            Err(_) => None,
        };
        // TODO: Create custom CookieStore
        let client = Self::build_client(default_headers, cookies);

        GalaRequest { client }
    }

    pub(crate) fn update_cookies(&mut self, new_cookies: Vec<Cookie>) {
        self.client = Self::build_client(Self::default_headers(), Some(new_cookies));
    }

    fn default_headers() -> HeaderMap {
        let mut default_headers = HeaderMap::new();
        default_headers.insert(
            header::CONTENT_TYPE,
            "application/x-www-form-urlencoded".parse().unwrap(),
        );
        default_headers
    }

    fn build_client(default_headers: HeaderMap, cookies: Option<Vec<Cookie>>) -> reqwest::Client {
        let mut default_headers = HeaderMap::from(default_headers);
        if let Some(cookies) = cookies {
            let raw_cookies = cookies.iter().fold(String::new(), |a, b| {
                let cookie = format!("{}={}", b.name(), b.value());

                if a.is_empty() {
                    cookie
                } else {
                    a + "; " + &cookie
                }
            });

            default_headers.insert(header::COOKIE, raw_cookies.parse().unwrap());
        }

        reqwest::Client::builder()
            .default_headers(default_headers)
            .user_agent("galaClient")
            .cookie_store(true)
            .use_rustls_tls()
            .build()
            .unwrap()
    }
}
