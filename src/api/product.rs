use bytes::Bytes;
use serde::{Deserialize, Serialize, Serializer, Deserializer};

use crate::{
    constants::{CONTENT_URL, DEV_URL},
    utils::ChangeTag,
};

use super::auth::Product;

#[derive(Debug, Serialize)]
struct LatestBuildNumberPayload {
    dev_id: String,
    id_key_name: String,
    os_selected: String,
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
    #[serde(rename = "File Name", deserialize_with = "from_latin1_str", serialize_with = "to_latin1_bytes")]
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

#[derive(Debug, Clone, Deserialize, Serialize)]
pub(crate) struct BuildManifestChunksRecord {
    #[serde(rename = "ID")]
    pub(crate) id: u16,
    #[serde(rename = "Filepath", deserialize_with = "from_latin1_str", serialize_with = "to_latin1_bytes")]
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

fn to_latin1_bytes<S>(string: &String, serializer: S) -> Result<S::Ok, S::Error>
where
    S: Serializer,
{
    serializer.serialize_bytes(&string.chars().into_iter().map(|c| c as u8).collect::<Vec<u8>>()[..])
}

pub(crate) async fn get_build_manifest(
    client: &reqwest::Client,
    product: &Product,
    build_version: &String,
) -> Result<Bytes, reqwest::Error> {
    let res = client
        .get(format!(
            "{}/DevShowCaseSourceVolume/dev_fold_{}/{}/{}/{}_manifest.csv",
            *CONTENT_URL,
            product.namespace,
            product.id_key_name,
            "win", // TODO: Support other platform downloads
            build_version,
        ))
        .send()
        .await?;
    let body = res.bytes().await?;
    Ok(body)
}

pub(crate) async fn get_build_manifest_chunks(
    client: &reqwest::Client,
    product: &Product,
    build_version: &String,
) -> Result<Bytes, reqwest::Error> {
    let res = client
        .get(format!(
            "{}/DevShowCaseSourceVolume/dev_fold_{}/{}/{}/{}_manifest_chunks.csv",
            *CONTENT_URL,
            product.namespace,
            product.id_key_name,
            "win", // TODO: Support other platform downloads
            build_version,
        ))
        .send()
        .await?;
    let body = res.bytes().await?;
    Ok(body)
}

pub(crate) async fn download_chunk(
    client: &reqwest::Client,
    product: &Product,
    chunk_sha: &String,
) -> Result<Bytes, reqwest::Error> {
    let res = client.get(get_chunk_url(product, chunk_sha)).send().await?;
    let bytes = res.bytes().await?;
    Ok(bytes)
}

#[derive(Debug, Deserialize)]
struct GameDetailsResponse {
    status: String,
    message: String,
    product_data: GameDetails,
}

#[derive(Debug, Deserialize, Serialize)]
pub(crate) struct GameDetails {
    pub(crate) exe_path: Option<String>,
    pub(crate) args: Option<String>,
    pub(crate) cwd: Option<String>,
}

pub(crate) async fn get_game_details(
    client: &reqwest::Client,
    product: &Product,
) -> Result<Option<GameDetails>, reqwest::Error> {
    let query = &[
        ("dev_id", &product.namespace),
        ("prod_name", &product.slugged_name),
    ];
    let res = client
        .get(format!("{}/get_product_info", *DEV_URL))
        .query(query)
        .send()
        .await?;

    let body = res.text().await?;
    match serde_json::from_str::<GameDetailsResponse>(&body) {
        Ok(data) => {
            if data.status != "success" {
                println!("Server failed to deliver game details");
                return Ok(None);
            }

            Ok(Some(data.product_data))
        }
        Err(_) => {
            println!(
                "Failed to get game details for {}. Are you logged in?",
                product.name
            );
            Ok(None)
        }
    }
}

fn get_chunk_url(product: &Product, chunk_sha: &String) -> String {
    format!(
        "{}/DevShowCaseSourceVolume/dev_fold_{}/{}/{}/{}",
        *CONTENT_URL,
        product.namespace,
        product.id_key_name,
        "win", // TODO: Support other platform downloads
        chunk_sha,
    )
}
