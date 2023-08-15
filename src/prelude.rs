use confy::ConfyError;
use cookie::Cookie;
use reqwest::header::{self, HeaderMap};

use crate::config::UserConfig;

pub(crate) trait CookieString {
    fn to_cookie_str(&self) -> String;
}

impl CookieString for HeaderMap {
    fn to_cookie_str(&self) -> String {
        self.get_all(header::SET_COOKIE)
            .iter()
            .fold(String::new(), |a, b| {
                let cookie = Cookie::parse(b.to_str().unwrap()).unwrap();
                let cookie = format!("{}={}", cookie.name(), cookie.value());

                if a.is_empty() {
                    cookie
                } else {
                    a + "; " + &cookie
                }
            })
    }
}

pub(crate) trait GalaConfig
where
    Self: Sized,
{
    fn load() -> Result<Self, ConfyError>;

    fn store(&self) -> Result<(), ConfyError>;
}

impl GalaConfig for UserConfig {
    fn load() -> Result<UserConfig, ConfyError> {
        confy::load::<UserConfig>("openGala", "user")
    }

    fn store(&self) -> Result<(), ConfyError> {
        confy::store("openGala", "user", self)
    }
}
