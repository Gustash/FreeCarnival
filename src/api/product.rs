use bytes::Bytes;
use serde::{Deserialize, Serialize};

use crate::constants::CONTENT_URL;

use super::auth::Product;

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
    #[serde(rename = "Size in Bytes")]
    pub(crate) size_in_bytes: usize,
    #[serde(rename = "Chunks")]
    pub(crate) chunks: usize,
    #[serde(rename = "SHA")]
    pub(crate) sha: String,
    #[serde(rename = "Flags")]
    pub(crate) flags: u8,
    #[serde(rename = "File Name")]
    pub(crate) file_name: String,
}

#[derive(Debug, Clone, Deserialize, Serialize)]
pub(crate) struct BuildManifestChunksRecord {
    #[serde(rename = "ID")]
    pub(crate) id: u16,
    #[serde(rename = "Filepath")]
    pub(crate) file_path: String,
    #[serde(rename = "Chunk SHA")]
    pub(crate) sha: String,
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
