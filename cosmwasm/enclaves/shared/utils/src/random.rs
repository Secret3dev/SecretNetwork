#![cfg(feature = "random")]

use enclave_crypto::{AESKey, Hmac};
use log::trace;

/// Whether a proof at this height includes the validator-set hash.
/// A zero gate means every height includes it.
pub fn proof_includes_valset(height: u64, hstar: u64) -> bool {
    hstar == 0 || height > hstar
}

/// HMAC binds height || encrypted_random || block_hash || valset_hash.
/// Generate always uses this format. Validate uses it only when proof_includes_valset.
pub fn create_random_proof(
    key: &AESKey,
    height: u64,
    random: &[u8],
    block_hash: &[u8],
    valset_hash: &[u8],
) -> [u8; 32] {
    trace!(
        "Height: {:?}\nRandom: {:?}\nApphash: {:?}\nValset: {:?}",
        height,
        hex::encode(random),
        hex::encode(block_hash),
        hex::encode(valset_hash)
    );

    let height_bytes = height.to_be_bytes();

    let data_len = height_bytes.len() + random.len() + block_hash.len() + valset_hash.len();
    let mut data = Vec::with_capacity(data_len);

    data.extend_from_slice(&height_bytes);
    data.extend_from_slice(random);
    data.extend_from_slice(block_hash);
    data.extend_from_slice(valset_hash);

    key.sign_sha_256(&data)
}

/// Proof over height, encrypted random, and block hash, without the validator-set hash.
/// Used only when proof_includes_valset is false.
pub fn create_random_proof_v126(
    key: &AESKey,
    height: u64,
    random: &[u8],
    block_hash: &[u8],
) -> [u8; 32] {
    trace!(
        "v126 Height: {:?}\nRandom: {:?}\nApphash: {:?}",
        height,
        hex::encode(random),
        hex::encode(block_hash)
    );

    let height_bytes = height.to_be_bytes();
    let data_len = height_bytes.len() + random.len() + block_hash.len();
    let mut data = Vec::with_capacity(data_len);

    data.extend_from_slice(&height_bytes);
    data.extend_from_slice(random);
    data.extend_from_slice(block_hash);

    key.sign_sha_256(&data)
}
