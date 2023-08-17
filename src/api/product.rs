use bytes::Bytes;
use serde::{Deserialize, Serialize};

use crate::constants::CONTENT_URL;

use super::{auth::Product, GalaRequest};

#[derive(Debug, Serialize)]
struct LatestBuildNumberPayload {
    dev_id: String,
    id_key_name: String,
    os_selected: String,
}

#[derive(Debug, Deserialize)]
struct BuildVersionResponse {
    result: String,
    build_version: Option<String>,
}

pub(crate) async fn get_latest_build_number(
    client: &reqwest::Client,
    product: &Product,
) -> Result<Option<String>, reqwest::Error> {
    let payload = LatestBuildNumberPayload {
        dev_id: product.namespace.to_owned(),
        id_key_name: product.id_key_name.to_owned(),
        os_selected: "win".to_string(), // TODO: Support other platform downloads
    };

    let res = client
        .post(format!("{}/get_latest_build_number", *CONTENT_URL))
        .json(&payload)
        .send()
        .await?;
    println!("Response: {res:#?}");

    let body = res.text().await?;
    match serde_json::from_str::<BuildVersionResponse>(&body) {
        Ok(data) => {
            if data.result != "ok" {
                println!("Server failed to deliver the latest build version");
                return Ok(None);
            }

            Ok(Some(data.build_version.unwrap()))
        }
        Err(_) => {
            println!(
                "Failed to get latest build for {}. Are you logged in?",
                product.name
            );
            Ok(None)
        }
    }
}

#[derive(Debug, Deserialize, Serialize)]
pub(crate) struct BuildManifestRecord {
    #[serde(alias = "Size in Bytes")]
    pub(crate) size_in_bytes: usize,
    #[serde(alias = "Chunks")]
    pub(crate) chunks: usize,
    #[serde(alias = "SHA")]
    pub(crate) sha: String,
    #[serde(alias = "Flags")]
    pub(crate) flags: u8,
    #[serde(alias = "File Name")]
    pub(crate) file_name: String,
}

#[derive(Debug, Deserialize, Serialize)]
pub(crate) struct BuildManifestChunksRecord {
    #[serde(alias = "ID")]
    pub(crate) id: u16,
    #[serde(alias = "Filepath")]
    pub(crate) file_path: String,
    #[serde(alias = "Chunk SHA")]
    pub(crate) sha: String,
}

pub(crate) async fn get_build_manifest(
    client: &reqwest::Client,
    product: &Product,
    build_version: &String,
) -> Result<String, reqwest::Error> {
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
    let body = res.text().await?;
    Ok(body)
}

pub(crate) async fn get_build_manifest_chunks(
    client: &reqwest::Client,
    product: &Product,
    build_version: &String,
) -> Result<String, reqwest::Error> {
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
    let body = res.text().await?;
    Ok(body)
}

pub(crate) async fn download_chunk(
    client: &reqwest::Client,
    product: &Product,
    chunk_sha: &String,
) -> Result<Bytes, reqwest::Error> {
    let res = client
        .get(format!(
            "{}/DevShowCaseSourceVolume/dev_fold_{}/{}/{}/{}",
            *CONTENT_URL,
            product.namespace,
            product.id_key_name,
            "win", // TODO: Support other platform downloads
            chunk_sha,
        ))
        .send()
        .await?;
    let bytes = res.bytes().await?;
    Ok(bytes)
}
