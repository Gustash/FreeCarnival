use std::sync::Arc;

use reqwest_cookie_store::CookieStoreMutex;

use crate::constants::DEFAULT_HEADERS;

pub(crate) mod auth;
pub(crate) mod product;

pub(crate) trait GalaClient {
    fn with_gala(cookie_store: &Arc<CookieStoreMutex>) -> Self;
}

impl GalaClient for reqwest::Client {
    fn with_gala(cookie_store: &Arc<CookieStoreMutex>) -> Self {
        reqwest::Client::builder()
            .default_headers(DEFAULT_HEADERS.to_owned())
            .cookie_provider(cookie_store.clone())
            .user_agent("galaClient")
            .use_rustls_tls()
            .build()
            .unwrap()
    }
}
