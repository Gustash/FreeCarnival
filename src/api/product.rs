use bytes::Bytes;

use crate::{
    constants::{CONTENT_URL, DEV_URL},
    shared::models::api::{BuildOs, GameDetails, GameDetailsResponse, Product, ProductVersion},
};

pub(crate) async fn get_build_manifest(
    client: &reqwest::Client,
    product: &Product,
    build_version: &ProductVersion,
) -> Result<Bytes, reqwest::Error> {
    let res = client
        .get(format!(
            "{}/DevShowCaseSourceVolume/dev_fold_{}/{}/{}/{}_manifest.csv",
            *CONTENT_URL,
            product.namespace,
            product.id_key_name,
            build_version.os,
            build_version.version,
        ))
        .send()
        .await?;
    let body = res.bytes().await?;
    Ok(body)
}

pub(crate) async fn get_build_manifest_chunks(
    client: &reqwest::Client,
    product: &Product,
    build_version: &ProductVersion,
) -> Result<Bytes, reqwest::Error> {
    let res = client
        .get(format!(
            "{}/DevShowCaseSourceVolume/dev_fold_{}/{}/{}/{}_manifest_chunks.csv",
            *CONTENT_URL,
            product.namespace,
            product.id_key_name,
            build_version.os,
            build_version.version,
        ))
        .send()
        .await?;
    let body = res.bytes().await?;
    Ok(body)
}

pub(crate) async fn download_chunk(
    client: &reqwest::Client,
    product: &Product,
    os: &BuildOs,
    chunk_sha: &String,
) -> Result<Bytes, reqwest::Error> {
    let res = client
        .get(get_chunk_url(product, os, chunk_sha))
        .send()
        .await?;
    let bytes = res.bytes().await?;
    Ok(bytes)
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

fn get_chunk_url(product: &Product, os: &BuildOs, chunk_sha: &String) -> String {
    format!(
        "{}/DevShowCaseSourceVolume/dev_fold_{}/{}/{}/{}",
        *CONTENT_URL, product.namespace, product.id_key_name, os, chunk_sha,
    )
}
