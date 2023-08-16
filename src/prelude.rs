use cookie::Cookie;
use reqwest::header::{self, HeaderMap};

pub(crate) trait CookieHeaderMap {
    fn to_cookie(&self) -> Vec<Cookie>;
}

impl CookieHeaderMap for HeaderMap {
    fn to_cookie(&self) -> Vec<Cookie> {
        self.get_all(header::SET_COOKIE)
            .iter()
            .map(|c| Cookie::parse(c.to_str().unwrap()).unwrap())
            .collect()
    }
}
