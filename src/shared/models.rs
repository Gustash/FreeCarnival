use std::path::PathBuf;

use serde::{Deserialize, Deserializer, Serialize, Serializer};

#[derive(Debug, Serialize, Deserialize)]
pub(crate) struct InstallInfo {
    /// Directory where game was installed to
    pub(crate) install_path: PathBuf,
    /// Version of the game that is installed
    pub(crate) version: String,
    /// OS the build is for
    #[serde(default)]
    pub(crate) os: api::BuildOs,
}

impl InstallInfo {
    pub(crate) fn new(install_path: PathBuf, version: String, os: api::BuildOs) -> InstallInfo {
        InstallInfo {
            install_path,
            version,
            os,
        }
    }
}

#[derive(Debug, Clone, Deserialize, Serialize)]
pub(crate) struct BuildManifestRecord {
    #[serde(rename = "Size in Bytes")]
    pub(crate) size_in_bytes: usize,
    #[serde(rename = "Chunks")]
    pub(crate) chunks: usize,
    #[serde(rename = "SHA")]
    pub(crate) sha: String,
    #[serde(rename = "Flags")]
    pub(crate) flags: u8,
    #[serde(
        rename = "File Name",
        deserialize_with = "from_latin1_str",
        serialize_with = "to_latin1_bytes"
    )]
    pub(crate) file_name: String,
    #[serde(rename = "Change Tag")]
    pub(crate) tag: Option<ChangeTag>,
}

impl BuildManifestRecord {
    pub(crate) fn is_directory(&self) -> bool {
        self.flags == 40
    }

    pub(crate) fn is_empty(&self) -> bool {
        self.size_in_bytes == 0
    }
}

#[derive(PartialEq, Clone, Debug, Serialize, Deserialize)]
pub(crate) enum ChangeTag {
    Added,
    Modified,
    Removed,
}

#[derive(Debug, Clone, Deserialize, Serialize)]
pub(crate) struct BuildManifestChunksRecord {
    #[serde(rename = "ID")]
    pub(crate) id: u16,
    #[serde(
        rename = "Filepath",
        deserialize_with = "from_latin1_str",
        serialize_with = "to_latin1_bytes"
    )]
    pub(crate) file_path: String,
    #[serde(rename = "Chunk SHA")]
    pub(crate) sha: String,
}

fn from_latin1_str<'de, D>(deserializer: D) -> Result<String, D::Error>
where
    D: Deserializer<'de>,
{
    let s: &[u8] = Deserialize::deserialize(deserializer)?;
    Ok(s.iter().cloned().map(char::from).collect())
}

fn to_latin1_bytes<S>(string: &str, serializer: S) -> Result<S::Ok, S::Error>
where
    S: Serializer,
{
    serializer.serialize_bytes(&string.chars().map(|c| c as u8).collect::<Vec<u8>>()[..])
}

pub(crate) mod api {
    use chrono::NaiveDateTime;
    use serde::{Deserialize, Serialize};

    use crate::config::{LibraryConfig, UserConfig};

    #[derive(Debug, Serialize)]
    pub(crate) struct LatestBuildNumberPayload {
        dev_id: String,
        id_key_name: String,
        os_selected: String,
    }

    #[derive(Debug, Deserialize)]
    pub(crate) struct GameDetailsResponse {
        pub(crate) status: String,
        pub(crate) message: String,
        pub(crate) product_data: GameDetails,
    }

    #[derive(Debug, Deserialize, Serialize)]
    pub(crate) struct GameDetails {
        pub(crate) exe_path: Option<String>,
        pub(crate) args: Option<String>,
        pub(crate) cwd: Option<String>,
    }

    #[derive(Deserialize, Debug)]
    pub(crate) struct LoginResult {
        pub(crate) status: String,
        pub(crate) message: String,
    }

    pub(crate) struct SyncResult {
        pub(crate) user_config: UserConfig,
        pub(crate) library_config: LibraryConfig,
    }

    #[derive(Deserialize, Serialize, Debug)]
    pub(crate) struct UserInfo {
        pub(crate) status: String,
        pub(crate) user_found: String,
        #[serde(alias = "_indiegala_user_email")]
        pub(crate) email: Option<String>,
        #[serde(alias = "_indiegala_username")]
        pub(crate) username: Option<String>,
        #[serde(alias = "_indiegala_user_id")]
        pub(crate) user_id: Option<u64>,
    }

    #[derive(Deserialize, Debug)]
    pub(crate) struct UserInfoShowcaseContent {
        pub(crate) showcase_content: Option<ShowcaseContent>,
    }

    #[derive(Deserialize, Debug)]
    pub(crate) struct ShowcaseContent {
        pub(crate) content: Content,
    }

    #[derive(Deserialize, Debug)]
    pub(crate) struct Content {
        pub(crate) user_collection: Vec<Product>,
    }

    #[derive(Deserialize, Serialize, Debug, Clone)]
    pub(crate) struct Product {
        #[serde(alias = "prod_dev_namespace")]
        pub(crate) namespace: String,
        #[serde(alias = "prod_slugged_name")]
        pub(crate) slugged_name: String,
        pub(crate) id: u64,
        #[serde(alias = "prod_name")]
        pub(crate) name: String,
        #[serde(alias = "prod_id_key_name")]
        pub(crate) id_key_name: String,
        pub(crate) version: Vec<ProductVersion>,
    }

    impl Product {
        pub(crate) fn get_latest_version(&self, os: Option<&BuildOs>) -> Option<&ProductVersion> {
            self.version.iter().fold(None, |acc, version| {
                let valid_os = match os {
                    Some(build_os) => version.os == *build_os,
                    #[cfg(target_os = "macos")]
                    None => version.os == BuildOs::Mac,
                    #[cfg(not(target_os = "macos"))]
                    None => version.os == BuildOs::Windows,
                };
                if !valid_os {
                    return acc;
                }

                match acc {
                    Some(v) => {
                        if version.date > v.date {
                            Some(version)
                        } else {
                            acc
                        }
                    }
                    None => Some(version),
                }
            })
        }
    }

    #[derive(Deserialize, Serialize, Debug, Clone)]
    pub(crate) struct ProductVersion {
        pub(crate) status: u16,
        pub(crate) enabled: u8,
        pub(crate) version: String,
        pub(crate) os: BuildOs,
        pub(crate) date: NaiveDateTime,
        pub(crate) text: String,
    }

    #[derive(Debug, Serialize, Deserialize, PartialEq, Clone)]
    pub(crate) enum BuildOs {
        #[serde(rename = "win")]
        Windows,
        #[serde(rename = "lin")]
        Linux,
        #[serde(rename = "mac")]
        Mac,
    }

    impl Default for BuildOs {
        fn default() -> Self {
            Self::Windows
        }
    }

    impl std::fmt::Display for BuildOs {
        fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
            write!(
                f,
                "{}",
                match self {
                    BuildOs::Windows => "win",
                    BuildOs::Linux => "lin",
                    BuildOs::Mac => "mac",
                }
            )
        }
    }

    impl std::fmt::Display for Product {
        fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
            write!(f, "[{}] {}", self.slugged_name, self.name)
        }
    }

    impl std::fmt::Display for ProductVersion {
        fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
            writeln!(f, "[{}]", self.version)?;
            writeln!(f, "Build Date: {}", self.date)?;
            writeln!(
                f,
                "Platform: {}",
                match self.os {
                    BuildOs::Windows => "Windows",
                    BuildOs::Linux => "Linux",
                    BuildOs::Mac => "macOS",
                }
            )?;
            if !self.text.is_empty() {
                writeln!(f, "About:\n\n{}", self.text)?;
            }

            Ok(())
        }
    }
}
