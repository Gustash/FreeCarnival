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
    product: &Product,
) -> Result<Option<String>, reqwest::Error> {
    let client = GalaRequest::new().client;
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
    pub(crate) size_in_bytes: u64,
    #[serde(alias = "Chunks")]
    pub(crate) chunks: usize,
    #[serde(alias = "SHA")]
    pub(crate) sha: String,
    #[serde(alias = "Flags")]
    pub(crate) flags: u8,
    #[serde(alias = "File Name")]
    pub(crate) file_name: String,
}

pub(crate) async fn get_build_manifest(
    product: &Product,
    build_version: &String,
) -> Result<Vec<BuildManifestRecord>, reqwest::Error> {
    let client = GalaRequest::new().client;

    println!("Product: {:?}", product);
    println!("Build Version: {}", build_version);
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
    println!("Body: {}", body);

    let mut rdr = csv::Reader::from_reader(body.as_bytes());
    let mut manifest: Vec<BuildManifestRecord> = vec![];
    for record in rdr.deserialize::<BuildManifestRecord>() {
        manifest.push(record.unwrap());
    }
    Ok(manifest)
}
