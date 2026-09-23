//!
use super::attestation::get_quote_ecdsa;
use crate::registration::attestation::{
    allow_list, verify_quote_sgx, AttestationCombined, PPID_WHITELIST,
};
#[cfg(feature = "verify-validator-whitelist")]
use block_verifier::validator_whitelist;
use core::convert::TryInto;
use ed25519_dalek::{PublicKey, Signature};
use enclave_crypto::consts::{
    make_sgx_secret_path, FILE_CERT_COMBINED, FILE_HALT_APPHASH, FILE_HALT_ER, FILE_HALT_HEIGHT,
    FILE_HALT_PROOF, FILE_MIGRATION_CERT_LOCAL,
    FILE_MIGRATION_CERT_REMOTE, FILE_MIGRATION_CONSENSUS, FILE_MIGRATION_DATA,
    FILE_MIGRATION_TARGET_INFO, FILE_PUBKEY, SELF_REPORT_BODY,
};
#[cfg(feature = "random")]
use enclave_crypto::{AESKey, Ed25519PublicKey, KeyPair, SIVEncryptable, PUBLIC_KEY_SIZE};
use enclave_ffi_types::SINGLE_ENCRYPTED_SEED_SIZE;
use enclave_utils::key_manager::KeychainMutableData;
use enclave_utils::pointers::validate_mut_slice;
use enclave_utils::storage::{get_key_from_seed, migrate_all_from_2_17, rotate_store};
use enclave_utils::{validate_const_ptr, validate_mut_ptr, Keychain, KEY_MANAGER};

/// These functions run off chain, and so are not limited by deterministic limitations. Feel free
/// to go crazy with random generation entropy, time requirements, or whatever else
///
use log::*;
use sgx_trts::trts::rsgx_read_rand;
use sgx_tse::{rsgx_create_report, rsgx_verify_report};
use sgx_types::{
    sgx_report_body_t, sgx_report_t, sgx_status_t, sgx_target_info_t, SgxResult,
};
use sha2::{Digest, Sha256};
use std::collections::HashMap;
use std::fs::File;
use std::io;
use std::io::prelude::*;
use std::panic;
use std::sgxfs::SgxFile;
use std::slice;
use tendermint::Hash::Sha256 as tm_Sha256;

use super::persistency::write_master_pub_keys;
use super::seed_exchange::{decrypt_seed, encrypt_seed};

#[cfg(feature = "light-client-validation")]
use block_verifier::VERIFIED_BLOCK_MESSAGES;

fn enforce_allow_list_init() {
    let _ = &*PPID_WHITELIST;
}
///
/// `ecall_init_bootstrap`
///
/// Function to handle the initialization of the bootstrap node. Generates the master private/public
/// key (seed + pk_io/sk_io). This happens once at the genesis of a chain. Returns the master
/// public key (pk_io), which is saved on-chain, and used to propagate the seed to registering nodes
///
/// # Safety
///  Something should go here
///
#[no_mangle]
pub unsafe extern "C" fn ecall_init_bootstrap(
    public_key: &mut [u8; PUBLIC_KEY_SIZE],
) -> sgx_status_t {
    validate_mut_ptr!(
        public_key.as_mut_ptr(),
        public_key.len(),
        sgx_status_t::SGX_ERROR_UNEXPECTED,
    );

    let mut key_manager = Keychain::new_empty();

    if let Err(_e) = key_manager.create_consensus_seed() {
        return sgx_status_t::SGX_ERROR_UNEXPECTED;
    }

    if let Err(_e) = key_manager.generate_consensus_master_keys() {
        return sgx_status_t::SGX_ERROR_UNEXPECTED;
    }

    if let Err(_e) = key_manager.create_registration_key() {
        return sgx_status_t::SGX_ERROR_UNEXPECTED;
    }
    key_manager.save();

    if let Err(status) = write_master_pub_keys(&key_manager) {
        return status;
    }

    public_key.copy_from_slice(&key_manager.seed_exchange_key().unwrap().get_pubkey());

    trace!(
        "ecall_init_bootstrap consensus_seed_exchange_keypair public key: {:?}",
        hex::encode(public_key)
    );

    sgx_status_t::SGX_SUCCESS
}

#[no_mangle]
pub unsafe extern "C" fn ecall_get_network_pubkey(
    i_seed: u32,
    p_node: *mut u8,
    p_io: *mut u8,
    p_seeds: *mut u32,
) -> sgx_status_t {
    validate_mut_ptr!(p_node, PUBLIC_KEY_SIZE, sgx_status_t::SGX_ERROR_UNEXPECTED);
    validate_mut_ptr!(p_io, PUBLIC_KEY_SIZE, sgx_status_t::SGX_ERROR_UNEXPECTED);

    if let Ok(seeds) = KEY_MANAGER.get_consensus_seed() {
        if (i_seed as usize) < seeds.arr.len() {
            let seed = &seeds.arr[i_seed as usize];
            let node_pk = Keychain::generate_consensus_seed_exchange_keypair(seed).get_pubkey();

            slice::from_raw_parts_mut(p_node, PUBLIC_KEY_SIZE).copy_from_slice(&node_pk);

            let io_pk = Keychain::generate_consensus_io_exchange_keypair(seed).get_pubkey();

            slice::from_raw_parts_mut(p_io, PUBLIC_KEY_SIZE).copy_from_slice(&io_pk);
        }
        *p_seeds = seeds.arr.len() as u32;
    } else {
        *p_seeds = 0;
    }

    enforce_allow_list_init();

    sgx_status_t::SGX_SUCCESS
}

///
///  `ecall_init_node`
///
/// This function is called during initialization of __non__ bootstrap nodes.
///
/// It receives the master public key (pk_io) and uses it, and its node key (generated in [ecall_key_gen])
/// to decrypt the seed.
///
/// The seed was encrypted using Diffie-Hellman in the function [ecall_get_encrypted_seed]
///
/// This function happens off-chain, so if we panic for some reason it _can_ be acceptable,
///  though probably not recommended
///
/// 15/10/22 - this is now called during node startup and will evaluate whether or not a node is valid
///
/// # Safety
///  Something should go here
///
#[no_mangle]
pub unsafe extern "C" fn ecall_init_node(
    master_key: *const u8,
    master_key_len: u32,
    encrypted_seed: *const u8,
    encrypted_seed_len: u32,
    // seed structure 1 byte - length (96 or 48) | genesis seed bytes | current seed bytes (optional)
) -> sgx_status_t {
    #[cfg(all(feature = "SGX_MODE_HW", feature = "production"))]
    {
        let temp_key_result = KeyPair::new();

        if temp_key_result.is_err() {
            error!("Failed to generate temporary key for attestation");
            return sgx_status_t::SGX_ERROR_UNEXPECTED;
        }

        // this validates the cert and handles the "what if it fails" inside as well
        let res = crate::registration::attestation::validate_enclave_version(
            temp_key_result.as_ref().unwrap(),
        );
        if res.is_err() {
            error!("Error starting node, might not be updated",);
            return sgx_status_t::SGX_ERROR_UNEXPECTED;
        }
    }

    let mut key_manager = Keychain::new();
    if key_manager.is_consensus_seed_set() {
        return sgx_status_t::SGX_SUCCESS; // skip the rest
    }

    validate_const_ptr!(
        master_key,
        master_key_len as usize,
        sgx_status_t::SGX_ERROR_UNEXPECTED,
    );

    validate_const_ptr!(
        encrypted_seed,
        encrypted_seed_len as usize,
        sgx_status_t::SGX_ERROR_UNEXPECTED,
    );

    if encrypted_seed_len == 0 {
        error!("Encrypted seed is empty");
        return sgx_status_t::SGX_ERROR_INVALID_PARAMETER;
    }

    info!("Importing encrypted seed...");

    let encrypted_seed_slice = slice::from_raw_parts(encrypted_seed, encrypted_seed_len as usize);

    // public keys in certificates don't have 0x04, so we'll copy it here
    let mut target_public_key: [u8; PUBLIC_KEY_SIZE] = [0u8; PUBLIC_KEY_SIZE];

    let key_slice = slice::from_raw_parts(master_key, master_key_len as usize);
    let pk = key_slice.to_vec();

    // just make sure the of the public key isn't messed up
    if pk.len() != PUBLIC_KEY_SIZE {
        error!("Got public key with the wrong size: {:?}", pk.len());
        return sgx_status_t::SGX_ERROR_UNEXPECTED;
    }
    target_public_key.copy_from_slice(&pk);

    trace!(
        "ecall_init_node target public key is: {}",
        hex::encode(target_public_key)
    );

    key_manager.delete_consensus_seed();
    key_manager.save();

    // older format for encrypted seeds: 1st byte is the size, then followed by 1 or 2 seeds
    // newer format: only the seeds, without the preceeding first byte

    let (seeds_count, seeds_packed) = match encrypted_seed_slice.len() % SINGLE_ENCRYPTED_SEED_SIZE
    {
        0 => {
            // newer format
            (encrypted_seed_slice.len(), encrypted_seed_slice)
        }
        1 => {
            // older format
            let used_len = encrypted_seed_slice[0] as u32 as usize;
            if used_len > encrypted_seed_slice.len() - 1 {
                error!("Unrecognized seeds used len: {}", used_len);
                return sgx_status_t::SGX_ERROR_INVALID_PARAMETER;
            }

            let seeds_count = used_len / SINGLE_ENCRYPTED_SEED_SIZE;
            let remaining = &encrypted_seed_slice[1..used_len + 1];
            (seeds_count, remaining)
        }
        _ => {
            error!("Unrecognized seeds len: {}", encrypted_seed_slice.len());
            return sgx_status_t::SGX_ERROR_INVALID_PARAMETER;
        }
    };

    for i_seed in 0..seeds_count {
        let offs = i_seed * SINGLE_ENCRYPTED_SEED_SIZE;
        let sub_slice: &[u8; SINGLE_ENCRYPTED_SEED_SIZE] = seeds_packed
            [offs..offs + SINGLE_ENCRYPTED_SEED_SIZE]
            .try_into()
            .unwrap();

        let seed = match decrypt_seed(&key_manager, target_public_key, *sub_slice) {
            Ok(result) => result,
            Err(status) => return status,
        };

        key_manager.push_consensus_seed(seed);
    }

    // this initializes the key manager with all the keys we need for computations
    if let Err(_e) = key_manager.generate_consensus_master_keys() {
        return sgx_status_t::SGX_ERROR_UNEXPECTED;
    }

    if let Err(status) = write_master_pub_keys(&key_manager) {
        return status;
    }

    key_manager.save();
    sgx_status_t::SGX_SUCCESS
}

pub unsafe fn get_attestation_report_dcap(
    pub_k: &[u8],
) -> Result<AttestationCombined, sgx_status_t> {
    let attestation = match get_quote_ecdsa(pub_k) {
        Ok(r) => r,
        Err(e) => {
            warn!("Error creating attestation report");
            return Err(e);
        }
    };

    Ok(attestation)
}

fn mrsigner_allowed(body: &sgx_report_body_t) -> bool {
    #[cfg(not(feature = "SGX_MODE_HW"))]
    {
        let _ = body;
        true
    }
    #[cfg(feature = "SGX_MODE_HW")]
    {
        body.mr_signer.m == SELF_REPORT_BODY.mr_signer.m
    }
}

fn target_info_from_report_body(body: &sgx_report_body_t) -> sgx_target_info_t {
    let mut ti = sgx_target_info_t::default();
    ti.mr_enclave = body.mr_enclave;
    ti.attributes = body.attributes;
    ti.config_svn = body.config_svn;
    ti.misc_select = body.misc_select;
    ti.config_id = body.config_id;
    ti
}

fn create_source_report_for_dest(dest_body: &sgx_report_body_t) -> SgxResult<sgx_report_t> {
    let target_info = target_info_from_report_body(dest_body);
    let mut report_data = sgx_types::sgx_report_data_t::default();
    report_data.d[..32].copy_from_slice(&Keychain::get_migration_keys().get_pubkey());
    rsgx_create_report(&target_info, &report_data)
}

fn dest_op1_report_is_self() -> bool {
    // The local report must be for this enclave. A report for another measurement is rejected.
    let mut f_in = match File::open(make_sgx_secret_path(FILE_MIGRATION_CERT_LOCAL)) {
        Ok(f) => f,
        Err(_) => return false,
    };
    let mut buffer = vec![0u8; std::mem::size_of::<sgx_report_t>()];
    if f_in.read_exact(&mut buffer).is_err() {
        return false;
    }
    let report: sgx_report_t =
        unsafe { std::ptr::read(buffer.as_ptr() as *const sgx_report_t) };
    let body = report.body;
    if body.mr_enclave.m != SELF_REPORT_BODY.mr_enclave.m {
        error!("import local report MRENCLAVE is not self");
        return false;
    }
    if !mrsigner_allowed(&body) {
        error!("import local report MRSIGNER not allowed");
        return false;
    }
    let pk = Keychain::get_migration_keys().get_pubkey();
    let rd = body.report_data;
    if rd.d[..32] != pk {
        error!("import local report pubkey is not dest migration key");
        return false;
    }
    true
}

fn get_verified_migration_report_body(check_ppid_wl: bool) -> SgxResult<sgx_report_body_t> {
    if let Ok(mut f_in) = File::open(make_sgx_secret_path(FILE_MIGRATION_CERT_LOCAL)) {
        let mut buffer = vec![0u8; std::mem::size_of::<sgx_report_t>()];
        if f_in.read_exact(&mut buffer).is_ok() {
            println!("Found local migration report");
            let report: sgx_report_t =
                unsafe { std::ptr::read(buffer.as_ptr() as *const sgx_report_t) };

            match rsgx_verify_report(&report) {
                Ok(()) => {
                    if !mrsigner_allowed(&report.body) {
                        error!("local migration report MRSIGNER not allowed");
                        return Err(sgx_status_t::SGX_ERROR_NO_PRIVILEGE);
                    }
                    return Ok(report.body);
                }
                Err(e) => {
                    error!("Can't verify local report: {}", e);
                }
            }
        }
    }

    if File::open(make_sgx_secret_path(FILE_MIGRATION_CERT_REMOTE)).is_ok() {
        let _ = check_ppid_wl;
        // A remote migration quote is not accepted. Migration uses the local report.
        error!("remote migration quote refused: DCAP time_s=0 forbidden; use local report");
    }

    Err(sgx_status_t::SGX_ERROR_NO_PRIVILEGE)
}

#[no_mangle]
/**
 * `ecall_get_attestation_report`
 *
 * Creates the attestation report to be used to authenticate with the blockchain. The output of this
 * function is an X.509 certificate signed by the enclave, which contains the report signed by Intel.
 *
 * Verifying functions will verify the public key bytes sent in the extra data of the __report__ (which
 * may or may not match the public key of the __certificate__ -- depending on implementation choices)
 *
 * This x509 certificate can be used in the future for mutual-RA cross-enclave TLS channels, or for
 * other creative usages.
 * # Safety
 * Something should go here
*/
pub unsafe extern "C" fn ecall_get_attestation_report(
    p_sk: *const u8,
    n_sk: u32,
    flags: u32,
) -> sgx_status_t {
    let mut report_data = [0_u8; sgx_types::SGX_REPORT_DATA_SIZE];

    let (kp, is_migration_report) = match 0x10 & flags {
        0x10 => {
            // migration report
            (Keychain::get_migration_keys(), true)
        }
        _ => {
            // standard network registration report
            let kp = KEY_MANAGER.get_registration_key().unwrap();
            trace!(
                "ecall_get_attestation_report key pk: {:?}",
                hex::encode(kp.get_pubkey())
            );

            let mut f_out = match File::create(make_sgx_secret_path(FILE_PUBKEY).as_str()) {
                Ok(f) => f,
                Err(e) => {
                    error!("failed to create file {}", e);
                    return sgx_status_t::SGX_ERROR_UNEXPECTED;
                }
            };

            f_out.write_all(kp.get_pubkey().as_ref()).unwrap();

            (kp, false)
        }
    };

    let attestation = {
        let pk_len = 32_usize;
        report_data[0..pk_len].copy_from_slice(&kp.get_pubkey());

        if n_sk == 32 {
            let sk = ed25519_dalek::SecretKey::from_bytes(std::slice::from_raw_parts(
                p_sk,
                n_sk as usize,
            ))
            .unwrap();

            let pk = ed25519_dalek::PublicKey::from(&sk);
            report_data[pk_len..pk_len + pk_len].copy_from_slice(&pk.to_bytes());
        }

        match get_attestation_report_dcap(&report_data) {
            Ok(x) => x,
            Err(e) => {
                return e;
            }
        }
    };

    let out_path = make_sgx_secret_path(if is_migration_report {
        FILE_MIGRATION_CERT_REMOTE
    } else {
        FILE_CERT_COMBINED
    });

    let mut f_out = match File::create(out_path.as_str()) {
        Ok(f) => f,
        Err(e) => {
            error!("failed to create file {}", e);
            return sgx_status_t::SGX_ERROR_UNEXPECTED;
        }
    };

    attestation.save(&mut f_out);

    sgx_status_t::SGX_SUCCESS
}

///
/// This function generates the registration_key, which is used in the attestation and registration
/// process
///
#[no_mangle]
pub unsafe extern "C" fn ecall_key_gen(
    public_key: &mut [u8; PUBLIC_KEY_SIZE],
) -> sgx_types::sgx_status_t {
    if let Err(_e) = validate_mut_slice(public_key) {
        return sgx_status_t::SGX_ERROR_UNEXPECTED;
    }

    let mut key_manager = Keychain::new_empty();
    if let Err(_e) = key_manager.create_registration_key() {
        error!("Failed to create registration key");
        return sgx_status_t::SGX_ERROR_UNEXPECTED;
    };
    key_manager.save();

    let reg_key = key_manager.get_registration_key();

    if reg_key.is_err() {
        error!("Failed to unlock node key. Please make sure the file is accessible or reinitialize the node");
        return sgx_status_t::SGX_ERROR_UNEXPECTED;
    }

    let pubkey = reg_key.unwrap().get_pubkey();
    public_key.clone_from_slice(&pubkey);
    trace!("ecall_key_gen key pk: {:?}", hex::encode(&public_key));
    sgx_status_t::SGX_SUCCESS
}

///
/// `ecall_get_genesis_seed
///
/// This call is used to help new nodes that want to full sync to have the previous "genesis" seed
/// A node that is regestering or being upgraded to version 1.9 will call this function.
///
/// The seed is encrypted with a key derived from the secret master key of the chain, and the public
/// key of the requesting chain
///
/// This function happens off-chain
///
#[no_mangle]
pub unsafe extern "C" fn ecall_get_genesis_seed(
    pk: *const u8,
    pk_len: u32,
    seed: &mut [u8; SINGLE_ENCRYPTED_SEED_SIZE],
) -> sgx_types::sgx_status_t {
    validate_mut_ptr!(
        seed.as_mut_ptr(),
        seed.len(),
        sgx_status_t::SGX_ERROR_UNEXPECTED
    );
    if pk_len != 0 {
        validate_const_ptr!(pk, pk_len as usize, sgx_status_t::SGX_ERROR_UNEXPECTED);
    }

    // Public ecall must not return genesis consensus seed[0] without
    // attestation+export policy. This ABI has no attestation; empty/error, not seeds.
    seed.fill(0);
    warn!("ecall_get_genesis_seed refused: attestation+export policy required");
    sgx_status_t::SGX_ERROR_ECALL_NOT_ALLOWED
}

#[no_mangle]
pub unsafe extern "C" fn ecall_rotate_store(p_buf: *mut u8, n_buf: u32) -> sgx_types::sgx_status_t {
    validate_const_ptr!(
        p_buf,
        n_buf as usize,
        sgx_status_t::SGX_ERROR_INVALID_PARAMETER
    );

    let consensus_ikm = match KEY_MANAGER.get_consensus_state_ikm() {
        Ok(keys) => keys,
        Err(e) => {
            error!("no current ikm keys {}", e);
            return sgx_status_t::SGX_ERROR_UNEXPECTED;
        }
    };

    let (rot_seed, _) = match read_rot_seed() {
        Ok(seed) => seed,
        Err(e) => {
            return e;
        }
    };

    let next_ikm = Keychain::generate_consensus_ikm_key(&rot_seed);

    let mut _num_total: u32 = 0;
    let mut _num_recoded: u32 = 0;

    match rotate_store(
        p_buf,
        n_buf as usize,
        consensus_ikm.last(),
        &next_ikm,
        &mut _num_total,
        &mut _num_recoded,
    ) {
        Ok(()) => {
            //trace!("------- Total={}, Recoded={}", num_total, num_recoded);
            sgx_status_t::SGX_SUCCESS
        }
        Err(e) => e,
    }
}

#[no_mangle]
pub unsafe extern "C" fn ecall_migration_op(opcode: u32) -> sgx_types::sgx_status_t {
    match opcode {
        0 => {
            println!("Convert legacy SGX files");
            migrate_all_from_2_17()
        }
        1 => {
            println!("Create self migration report");

            export_local_migration_report();
            ecall_get_attestation_report(core::ptr::null(), 0, 0x10) // migration
        }
        2 => {
            println!("Export encrypted data to the next aurhorized enclave");
            export_sealed_data()
        }
        3 => {
            println!("Import sealed data from the previous enclave");
            import_sealed_data()
        }
        4 => {
            println!("Import sealed data from the legacy enclave");
            import_sealing_legacy()
        }
        5 => {
            println!("Export self target info");
            export_self_target_info()
        }
        6 => {
            println!("Generate true random seed for rotation");
            generate_rot_seed()
        }
        7 => {
            println!("Export rotation seed");
            export_rot_seed()
        }
        8 => {
            println!("Import rotation seed");
            import_rot_seed()
        }
        9 => {
            println!("Apply rotation seed");
            apply_rot_seed()
        }
        10 => {
            println!("Key config");
            print_key_config()
        }
        _ => sgx_status_t::SGX_ERROR_UNEXPECTED,
    }
}

fn is_msg_mrenclave(msg_in_block: &[u8], mrenclave: &[u8]) -> bool {
    trace!("*** block msg: {:?}", hex::encode(msg_in_block));

    // The message is a fixed-length protobuf value containing the measurement.

    if msg_in_block.len() != 81 {
        trace!("len mismatch: {}", msg_in_block.len());
        return false;
    }

    if &msg_in_block[0..2] != [0x0a, 0x2d].as_slice() {
        trace!("wrong sub1");
        return false;
    }

    if &msg_in_block[47..49] != [0x12, 0x20].as_slice() {
        trace!("wrong sub2");
        return false;
    }

    if &msg_in_block[49..81] != mrenclave {
        trace!("wrong mrenclave");
        return false;
    }

    true
}

#[cfg(feature = "light-client-validation")]
fn check_mrenclave_in_block(msg_slice: &[u8]) -> bool {
    let mut verified_msgs = VERIFIED_BLOCK_MESSAGES.lock().unwrap();

    while verified_msgs.remaining() > 0 {
        if let Some(verified_msg) = verified_msgs.get_next() {
            if is_msg_mrenclave(&verified_msg, msg_slice) {
                return true;
            }
        }
    }
    false
}

#[cfg(not(feature = "light-client-validation"))]
fn check_mrenclave_in_block(_msg_slice: &[u8]) -> bool {
    false
}

#[no_mangle]
pub unsafe extern "C" fn ecall_onchain_approve_upgrade(
    msg: *const u8,
    msg_len: u32,
) -> sgx_types::sgx_status_t {
    validate_const_ptr!(msg, msg_len as usize, sgx_status_t::SGX_ERROR_UNEXPECTED);
    let msg_slice = slice::from_raw_parts(msg, msg_len as usize);

    trace!(
        "ecall_onchain_approve_upgrade mrenclave: {:?}",
        hex::encode(msg_slice)
    );

    if !check_mrenclave_in_block(msg_slice) {
        error!("migration target not approved");
        return sgx_types::sgx_status_t::SGX_ERROR_UNEXPECTED;
    }

    // The call is accepted after the block check. It does not store the measurement.
    info!(
        "ecall_onchain_approve_upgrade: no-write. mr_enclave={} ignored",
        hex::encode(msg_slice)
    );

    sgx_types::sgx_status_t::SGX_SUCCESS
}

struct ProtobufParser<'a> {
    pub cursor: &'a [u8],
}

impl ProtobufParser<'_> {
    fn cut_head(&mut self, size: usize) {
        self.cursor = &self.cursor[size..];
    }

    pub fn read_uint(&mut self) -> Option<usize> {
        let mut ret: usize = 0;
        let len = self.cursor.len();
        for i in 0..len {
            let byte = self.cursor[i];

            if byte & 0x80 == 0 {
                self.cut_head(i + 1);
                ret |= (byte as usize) << (i * 7);
                return Some(ret);
            }

            ret |= ((byte & 0x7f) as usize) << (i * 7);
        }

        None
    }

    pub fn read_fix_arr(&mut self, fixed: &[u8]) -> bool {
        let fixed_len = fixed.len();
        if self.cursor.len() < fixed_len {
            return false;
        }

        if &self.cursor[0..fixed_len] != fixed {
            return false;
        }

        self.cut_head(fixed_len);
        true
    }

    fn read_const_size(&mut self, len: usize) -> bool {
        if self.cursor.len() < len {
            return false;
        }

        self.cut_head(len);
        true
    }
}

fn is_msg_machine_id(msg_in_block: &[u8], machine_id: &[u8]) -> bool {
    trace!("*** block msg: {}", hex::encode(msg_in_block));
    //trace!("*** target: {}", hex::encode(machine_id));

    // The message is a protobuf value containing the proposal id and machine ids.

    let mut r = ProtobufParser {
        cursor: msg_in_block,
    };

    if !r.read_fix_arr([0x0a, 0x2d].as_slice()) {
        trace!("wrong sub1");
        return false;
    }

    if !r.read_const_size(45) {
        trace!("wrong sub2");
        return false;
    }

    if !r.read_fix_arr([0x10].as_slice()) {
        trace!("wrong sub3");
        return false;
    }

    if r.read_uint().is_none() {
        trace!("wrong sub4");
        return false;
    }

    if !r.read_fix_arr([0x1a].as_slice()) {
        trace!("wrong sub5");
        return false;
    }

    if let Some(x) = r.read_uint() {
        if x > r.cursor.len() {
            trace!("malformed field");
            return false;
        }
        r.cursor = &r.cursor[0..x];
    } else {
        trace!("wrong sub6");
        return false;
    };

    loop {
        let (elem_size, is_last) = if let Some(pos) = r.cursor.iter().position(|&b| b == b',') {
            (pos, false)
        } else {
            (r.cursor.len(), true)
        };

        //trace!("elem: {}", hex::encode(&r.cursor[0..elem_size]));

        if (elem_size == machine_id.len()) && (&r.cursor[0..elem_size] == machine_id) {
            return true;
        }

        if is_last {
            break;
        }

        r.cut_head(elem_size + 1);
    }

    false
}

#[cfg(feature = "test")]
pub fn test_is_msg_machine_id_malformed_len_returns_false() {
    let mut msg = vec![0x0a, 0x2d];
    msg.extend_from_slice(&[0u8; 45]);
    msg.extend_from_slice(&[0x10, 0x01, 0x1a, 0x7f]);
    assert!(!is_msg_machine_id(&msg, b"machine"));
}

#[cfg(feature = "light-client-validation")]
fn check_machine_id_in_block(msg_slice: &[u8]) -> bool {
    let mut verified_msgs = VERIFIED_BLOCK_MESSAGES.lock().unwrap();

    while verified_msgs.remaining() > 0 {
        if let Some(verified_msg) = verified_msgs.show_next() {
            if is_msg_machine_id(verified_msg, msg_slice) {
                return true;
            }

            verified_msgs.get_next(); // skip
        }
    }
    false
}

#[cfg(not(feature = "light-client-validation"))]
fn check_machine_id_in_block(_msg_slice: &[u8]) -> bool {
    false
}

#[no_mangle]
pub unsafe extern "C" fn ecall_onchain_approve_machine_id(
    p_id: *const u8,
    n_id: u32,
) -> sgx_types::sgx_status_t {
    validate_const_ptr!(p_id, n_id as usize, sgx_status_t::SGX_ERROR_UNEXPECTED);

    if n_id as usize != allow_list::MACHINE_ID_LEN {
        println!("machine_id wrong len");
        return sgx_types::sgx_status_t::SGX_ERROR_UNEXPECTED;
    }

    let machine_id = slice::from_raw_parts(p_id, n_id as usize);
    let machine_id_str = hex::encode(machine_id);

    if !check_machine_id_in_block(machine_id_str.as_bytes()) {
        error!("machine ID not approved");
        return sgx_types::sgx_status_t::SGX_ERROR_UNEXPECTED;
    }

    {
        let mut allow_list = PPID_WHITELIST.lock().unwrap();

        let machine_id: &allow_list::MachineID = machine_id.try_into().unwrap();

        allow_list.add_new(*machine_id, false);
    }

    sgx_types::sgx_status_t::SGX_SUCCESS
}

struct MerkleProcessor<'a> {
    cursor: io::Cursor<&'a [u8]>,
}

impl<'a> MerkleProcessor<'a> {
    fn new(data: &'a [u8]) -> Self {
        Self {
            cursor: io::Cursor::new(data),
        }
    }

    fn read_slice_n(&mut self, n: usize) -> io::Result<&'a [u8]> {
        let pos = self.cursor.position() as usize;
        let buf = self.cursor.get_ref();

        if pos + n > buf.len() {
            return Err(io::Error::new(
                io::ErrorKind::UnexpectedEof,
                "not enough bytes",
            ));
        }

        let out = &buf[pos..pos + n];
        self.cursor.set_position((pos + n) as u64);
        Ok(out)
    }

    fn read_slice(&mut self) -> io::Result<&'a [u8]> {
        let n = Keychain::read_u32(&mut self.cursor)? as usize;
        self.read_slice_n(n)
    }

    fn hash_var_uint(hasher: &mut Sha256, mut x: usize) {
        loop {
            if x < 0x80 {
                let b = x as u8;
                hasher.update([b]);
                break;
            }

            let b = 0x80 | ((x & 0x7f) as u8);
            hasher.update([b]);
            x >>= 7;
        }
    }

    fn hash_leaf_iavl(&mut self, key: &[u8], val: &[u8]) -> io::Result<[u8; 32]> {
        let prefix = self.read_slice()?;

        let valhash: [u8; 32] = {
            let mut hasher = Sha256::new();
            hasher.update(val);
            hasher.finalize().into()
        };

        let mut hasher = Sha256::new();
        hasher.update(prefix); // prefix len isn't required
        Self::hash_var_uint(&mut hasher, key.len());
        hasher.update(key);
        Self::hash_var_uint(&mut hasher, valhash.len());
        hasher.update(valhash);

        Ok(hasher.finalize().into())
    }

    fn interpret_sub_path(&mut self, hash_value: &mut [u8; 32]) -> io::Result<()> {
        //println!("*** leaf hash: {}", hex::encode(&hash_value));

        let n = Keychain::read_u32(&mut self.cursor)?;
        for _i in 0..n {
            let mut hasher = Sha256::new();
            hasher.update(self.read_slice()?); // prefix
            hasher.update(&hash_value);
            hasher.update(self.read_slice()?); // suffix
            *hash_value = hasher.finalize().into();
            //println!("*** next hash: {}", hex::encode(&hash_value));
        }

        Ok(())
    }
}

#[no_mangle]
pub unsafe extern "C" fn ecall_submit_machine_swap(
    index: u32,
    p_machine_info: *const u8,
    n_machine_info: u32,
    p_proof: *const u8,
    n_proof: u32,
) -> sgx_types::sgx_status_t {
    validate_const_ptr!(
        p_machine_info,
        n_machine_info as usize,
        sgx_status_t::SGX_ERROR_UNEXPECTED
    );
    validate_const_ptr!(
        p_proof,
        n_proof as usize,
        sgx_status_t::SGX_ERROR_UNEXPECTED
    );

    let merkle_proof_result = (|| -> io::Result<()> {
        // verify merkle proof

        let apphash = {
            let extra = KEY_MANAGER.extra_data.lock().unwrap();
            extra.apphash
        };

        let mut merkle = MerkleProcessor::new(slice::from_raw_parts(p_proof, n_proof as usize));

        let proof_parts = Keychain::read_u32(&mut merkle.cursor)?;
        if proof_parts != 2 {
            return Err(io::Error::new(
                io::ErrorKind::InvalidData,
                "proof wrong len",
            ));
        }

        // leaf
        let mut hash_val = {
            let mut k = [0u8; 5];
            k[0] = 0x03; // RegistrationMachinePrefix
            k[1..5].copy_from_slice(&index.to_be_bytes());

            let v = slice::from_raw_parts(p_machine_info, n_machine_info as usize);

            merkle.hash_leaf_iavl(&k, v)?
        };

        merkle.interpret_sub_path(&mut hash_val)?;

        hash_val = {
            let k = b"register";
            merkle.hash_leaf_iavl(k, &hash_val)?
        };

        merkle.interpret_sub_path(&mut hash_val)?;

        if apphash != hash_val {
            error!(
                "Merkle root expected: {}, actual: {}",
                hex::encode(apphash),
                hex::encode(hash_val)
            );
            return Err(io::Error::new(
                io::ErrorKind::InvalidData,
                "apphash mismatch",
            ));
        }

        Ok(())
    })();

    if let Err(e) = merkle_proof_result {
        println!("Merkle proof failed: {}", e);
        return sgx_types::sgx_status_t::SGX_ERROR_UNEXPECTED;
    }

    const MACHINE_SWAP_INFO_LEN: usize =
        allow_list::OWNER_LEN + allow_list::MACHINE_ID_LEN + allow_list::MACHINE_ID_LEN;

    let (swap_info_opt, p_machine_id) = match n_machine_info as usize {
        MACHINE_SWAP_INFO_LEN => {
            let owner: &allow_list::Owner =
                slice::from_raw_parts(p_machine_info, allow_list::OWNER_LEN)
                    .try_into()
                    .unwrap();

            let machine_id_pop: &allow_list::MachineID = slice::from_raw_parts(
                p_machine_info.add(allow_list::OWNER_LEN + allow_list::MACHINE_ID_LEN),
                allow_list::MACHINE_ID_LEN,
            )
            .try_into()
            .unwrap();

            (
                Some((owner, machine_id_pop)),
                p_machine_info.add(allow_list::OWNER_LEN),
            )
        }
        allow_list::MACHINE_ID_LEN => (None, p_machine_info),
        _ => {
            println!("machine_info wrong len");
            return sgx_types::sgx_status_t::SGX_ERROR_UNEXPECTED;
        }
    };

    let machine_id: &allow_list::MachineID =
        slice::from_raw_parts(p_machine_id, allow_list::MACHINE_ID_LEN)
            .try_into()
            .unwrap();

    {
        let mut allow_list = PPID_WHITELIST.lock().unwrap();

        if let Some(swap_info) = swap_info_opt {
            allow_list.update(machine_id, swap_info.0, swap_info.1);
        } else {
            allow_list.add_new(*machine_id, false);
        }
    }

    sgx_types::sgx_status_t::SGX_SUCCESS
}

pub fn calculate_truncated_hash(input: &[u8]) -> [u8; 20] {
    let mut res = [0u8; 20];

    let mut hasher = Sha256::new();
    hasher.update(input);

    let res_full = hasher.finalize();
    res.copy_from_slice(&res_full[..20]);

    res
}

fn load_offchain_signers(
    mut f_in: File,
    report: &sgx_report_body_t,
) -> std::collections::HashSet<[u8; 20]> {
    let mut json_data = String::new();
    f_in.read_to_string(&mut json_data).unwrap();

    // Deserialize the JSON string into a HashMap<String, String>
    let signatures: HashMap<String, (String, String)> =
        serde_json::from_str(&json_data).expect("Failed to deserialize JSON");

    let mut signers = std::collections::HashSet::new();

    for (addr_str, (pubkey_str, sig_str)) in &signatures {
        let pubkey_bytes = base64::decode(pubkey_str).unwrap();

        // calculate the address
        let addr = calculate_truncated_hash(&pubkey_bytes);

        // make sure pubkey matches the address
        let res = hex::decode(addr_str).unwrap();
        if res != addr {
            panic!(
                "address doesn't match pubkey. Expected={}, actual={}",
                hex::encode(addr),
                addr_str
            );
        }

        // verify signature

        let pubkey_obj = PublicKey::from_bytes(&pubkey_bytes).unwrap();
        let sig_bytes = base64::decode(sig_str).unwrap();
        let sig_obj = Signature::from_bytes(&sig_bytes).unwrap();

        if pubkey_obj
            .verify_strict(&report.mr_enclave.m, &sig_obj)
            .is_err()
        {
            panic!("Incorrect signature for address: {}", addr_str);
        }

        if signers.insert(addr) {
            println!("  Approved by {}", addr_str);
        }
    }

    signers
}

#[cfg(feature = "verify-validator-whitelist")]
fn count_included_addresses(
    signers: &std::collections::HashSet<[u8; 20]>,
    list: &validator_whitelist::ValidatorList,
) -> usize {
    let mut res: usize = 0;

    for addr_str in &list.0 {
        let addr_vec = hex::decode(addr_str).unwrap();
        let addr: [u8; 20] = addr_vec.try_into().unwrap();

        if signers.contains(&addr) {
            res += 1;
        }
    }

    res
}

fn is_standard_consensus_reached(signers: &std::collections::HashSet<[u8; 20]>) -> bool {
    let mut total_voting_power: u64 = 0;
    let mut approved_power: u64 = 0;

    let validator_set = {
        let extra = KEY_MANAGER.extra_data.lock().unwrap();
        extra.decode_validator_set().unwrap()
    };

    for validator in validator_set.validators() {
        let power: u64 = validator.power.value();
        total_voting_power += power;

        let addr: [u8; 20] = validator.address.as_bytes().try_into().unwrap();
        if signers.contains(&addr) {
            approved_power += power;
        }
    }

    println!(
        "Total Power = {}, Approved Power = {}",
        total_voting_power, approved_power
    );

    if approved_power * 3 < total_voting_power * 2 {
        println!(" not enogh voting power");
        return false;
    }

    #[cfg(feature = "verify-validator-whitelist")]
    {
        let approved_whitelisted =
            count_included_addresses(signers, &validator_whitelist::VALIDATOR_WHITELIST);
        if approved_whitelisted < validator_whitelist::VALIDATOR_THRESHOLD {
            println!(
                " not enogh whitelisted validators: {}",
                approved_whitelisted
            );
            return false;
        }
    }
    true
}

fn is_export_approved_offchain(f_in: File, report: &sgx_report_body_t) -> bool {
    let signers = load_offchain_signers(f_in, report);

    let b1 = is_standard_consensus_reached(&signers);
    println!("Standard consensus reached: {}", b1);

    #[cfg(not(feature = "verify-validator-whitelist"))]
    let b2 = false;

    #[cfg(feature = "verify-validator-whitelist")]
    let b2 = {
        let approved_whitelisted = count_included_addresses(
            &signers,
            &validator_whitelist::VALIDATOR_WHITELIST_EMERGENCY,
        );
        println!(
            " Emergency whitelisted validators: {}",
            approved_whitelisted
        );

        approved_whitelisted >= validator_whitelist::VALIDATOR_THRESHOLD_EMERGENCY
    };

    println!("Emergency threshold reached: {}", b2);

    b1 || b2
}

fn is_export_approved(report: &sgx_report_body_t) -> bool {
    // Export is approved only from the emergency consensus file.
    let _ = report;
    if let Ok(f_in) = File::open(make_sgx_secret_path(FILE_MIGRATION_CONSENSUS).as_str()) {
        if is_export_approved_offchain(f_in, report) {
            println!("Migration is authorized by off-chain (emergency) consensus");
            return true;
        }
    }

    false
}

fn export_self_target_info() -> sgx_status_t {
    let mut target_info = sgx_target_info_t::default();
    unsafe { sgx_types::sgx_self_target(&mut target_info) };

    let mut f_out = match File::create(make_sgx_secret_path(FILE_MIGRATION_TARGET_INFO)) {
        Ok(f) => f,
        Err(e) => {
            error!("failed to create file {}", e);
            return sgx_status_t::SGX_ERROR_UNEXPECTED;
        }
    };

    let info_ptr = &target_info as *const sgx_target_info_t as *const u8;
    let info_size = std::mem::size_of::<sgx_target_info_t>();

    // Convert the report to a byte slice
    let report_bytes: &[u8] = unsafe { slice::from_raw_parts(info_ptr, info_size) };

    // Write the byte slice to the file
    f_out.write_all(report_bytes).unwrap();

    println!("Local migration self target saved");
    sgx_status_t::SGX_SUCCESS
}

fn get_rot_seed_file_params() -> (String, [u8; 16]) {
    let kdk = get_key_from_seed("seal.rot_seed".as_bytes());
    (make_sgx_secret_path("rot_seed.sealed"), kdk)
}

fn get_rot_seed_encrypted_path() -> String {
    make_sgx_secret_path("rot_seed_encr.bin")
}

fn save_rot_seed(rot_seed: &enclave_crypto::Seed, flags: u8) {
    let (path, kdk) = get_rot_seed_file_params();
    let mut file = SgxFile::create_ex(path, &kdk).unwrap();
    file.write_all(rot_seed.as_slice()).unwrap();
    file.write_all(&[flags]).unwrap();

    print_key_config_rot(rot_seed);
}

fn generate_rot_seed() -> sgx_status_t {
    let rot_seed = match enclave_crypto::Seed::new() {
        Ok(seed) => seed,
        Err(e) => {
            error!("Error generating random: {}", e);
            return sgx_status_t::SGX_ERROR_UNEXPECTED;
        }
    };

    save_rot_seed(&rot_seed, 1);
    println!("New seed generated");

    sgx_status_t::SGX_SUCCESS
}

fn get_dh_aes_key_from_report(other_report: &sgx_report_body_t, kp: &KeyPair) -> AESKey {
    let other_pub_k = &other_report.report_data.d[0..32].try_into().unwrap();
    AESKey::new_from_slice(&kp.diffie_hellman(other_pub_k))
}

fn get_seed_rot_report() -> SgxResult<sgx_report_body_t> {
    let next_report = match get_verified_migration_report_body(false) {
        Ok(report) => report,
        Err(e) => {
            error!("No migration report: {}", e);
            return Err(e);
        }
    };

    if next_report.mr_enclave.m != SELF_REPORT_BODY.mr_enclave.m {
        println!("Not eligible");
        return Err(sgx_status_t::SGX_ERROR_NO_PRIVILEGE);
    }

    Ok(next_report)
}

fn get_dh_aes_key_from_rot_report() -> SgxResult<AESKey> {
    match get_seed_rot_report() {
        Ok(r) => {
            let kp = Keychain::get_migration_keys();
            Ok(get_dh_aes_key_from_report(&r, &kp))
        }
        Err(e) => Err(e),
    }
}

fn read_rot_seed() -> SgxResult<(enclave_crypto::Seed, u8)> {
    let (path, kdk) = get_rot_seed_file_params();
    let mut file = match SgxFile::open_ex(path, &kdk) {
        Ok(f) => f,
        Err(e) => {
            error!("can't open rot seed file: {}", e);
            return Err(sgx_status_t::SGX_ERROR_UNEXPECTED);
        }
    };

    let mut seed = enclave_crypto::Seed::default();
    if let Err(e) = file.read_exact(seed.as_mut()) {
        error!("can't read rot seed file: {}", e);
        return Err(sgx_status_t::SGX_ERROR_UNEXPECTED);
    }

    let mut flags_buf = [0u8; 1]; // a real 1-byte buffer
    if let Err(e) = file.read_exact(&mut flags_buf) {
        error!("can't read rot seed file: {}", e);
        return Err(sgx_status_t::SGX_ERROR_UNEXPECTED);
    }

    print_key_config_rot(&seed);

    Ok((seed, flags_buf[0]))
}

fn export_rot_seed() -> sgx_status_t {
    let rot_seed = match read_rot_seed() {
        Ok((seed, flags)) => {
            if flags == 0 {
                println!("Not a source of seed. Export disallowed");
                return sgx_status_t::SGX_ERROR_ECALL_NOT_ALLOWED;
            }
            seed
        }
        Err(e) => return e,
    };

    let next_report = match get_seed_rot_report() {
        Ok(r) => r,
        Err(e) => return e,
    };
    if !is_export_approved(&next_report) {
        error!("Export rotation seed not authorized");
        return sgx_status_t::SGX_ERROR_NO_PRIVILEGE;
    }

    let aes_key = match get_dh_aes_key_from_rot_report() {
        Ok(k) => k,
        Err(e) => return e,
    };

    let data_encrypted = aes_key.encrypt_siv(rot_seed.as_slice(), None).unwrap();

    //println!("ecnrypted seed candidate: {}", hex::encode(data_encrypted));

    let mut f_out = File::create(make_sgx_secret_path(&get_rot_seed_encrypted_path())).unwrap();
    f_out.write_all(&data_encrypted).unwrap();

    sgx_status_t::SGX_SUCCESS
}

fn import_rot_seed() -> sgx_status_t {
    let mut f_in = match File::open(get_rot_seed_encrypted_path()) {
        Err(e) => {
            error!("can't find encrypted seed: {}", e);
            return sgx_status_t::SGX_ERROR_UNEXPECTED;
        }
        Ok(f) => f,
    };

    let mut data_encrypted = Vec::new();
    f_in.read_to_end(&mut data_encrypted).unwrap();

    let aes_key = match get_dh_aes_key_from_rot_report() {
        Ok(k) => k,
        Err(e) => return e,
    };

    let data_plain = match aes_key.decrypt_siv(&data_encrypted, None) {
        Ok(res) => res,
        Err(err) => {
            error!("Can't decrypt: {}", err);
            return sgx_status_t::SGX_ERROR_UNEXPECTED;
        }
    };

    if enclave_crypto::SEED_KEY_SIZE != data_plain.len() {
        error!("seed len mismatch");
    }

    let mut rot_seed = enclave_crypto::Seed::default();
    rot_seed.as_mut().copy_from_slice(&data_plain);

    save_rot_seed(&rot_seed, 0);

    println!("Seed imported");
    sgx_status_t::SGX_SUCCESS
}

fn apply_rot_seed() -> sgx_status_t {
    let (rot_seed, _) = match read_rot_seed() {
        Ok(seed) => seed,
        Err(e) => {
            return e;
        }
    };

    let mut key_manager = Keychain::new();

    {
        let seeds = key_manager.get_consensus_seed().unwrap();

        if (seeds.arr.len() < 2) || (seeds.arr.len() > 4) {
            error!("seeds count mismatch");
            return sgx_status_t::SGX_ERROR_UNEXPECTED;
        }
    }

    key_manager.push_consensus_seed(rot_seed);
    key_manager.save();

    if let Err(_e) = key_manager.generate_consensus_master_keys() {
        return sgx_status_t::SGX_ERROR_UNEXPECTED;
    }

    match write_master_pub_keys(&key_manager) {
        Err(e) => e,
        Ok(()) => sgx_status_t::SGX_SUCCESS,
    }
}

fn print_key_config_ex(seed: &enclave_crypto::Seed) {
    let node_pk = Keychain::generate_consensus_seed_exchange_keypair(seed).get_pubkey();
    let io_pk = Keychain::generate_consensus_io_exchange_keypair(seed).get_pubkey();

    println!("{},{}", base64::encode(node_pk), base64::encode(io_pk));
}

fn print_key_config() -> sgx_status_t {
    if let Ok(seeds) = KEY_MANAGER.get_consensus_seed() {
        for i_seed in 0..seeds.arr.len() {
            print_key_config_ex(&seeds.arr[i_seed]);
        }
    }

    sgx_status_t::SGX_SUCCESS
}

fn print_key_config_rot(seed: &enclave_crypto::Seed) {
    println!("rot_seed pubkey");
    print_key_config_ex(seed);
}

fn export_local_migration_report() -> sgx_status_t {
    if let Ok(mut f_in) = File::open(make_sgx_secret_path(FILE_MIGRATION_TARGET_INFO)) {
        let mut buffer = vec![0u8; std::mem::size_of::<sgx_target_info_t>()];
        if f_in.read_exact(&mut buffer).is_ok() {
            println!("Found local migration target info");
            let target_info: sgx_target_info_t =
                unsafe { std::ptr::read(buffer.as_ptr() as *const sgx_target_info_t) };

            let mut report_data = sgx_types::sgx_report_data_t::default();
            report_data.d[..32].copy_from_slice(&Keychain::get_migration_keys().get_pubkey());

            let my_report = match rsgx_create_report(&target_info, &report_data) {
                Ok(report) => report,
                Err(e) => {
                    return e;
                }
            };

            let mut f_out = match File::create(make_sgx_secret_path(FILE_MIGRATION_CERT_LOCAL)) {
                Ok(f) => f,
                Err(e) => {
                    error!("failed to create file {}", e);
                    return sgx_status_t::SGX_ERROR_UNEXPECTED;
                }
            };

            let report_ptr = &my_report as *const sgx_report_t as *const u8;
            let report_size = std::mem::size_of::<sgx_report_t>();

            // Convert the report to a byte slice
            let report_bytes: &[u8] = unsafe { slice::from_raw_parts(report_ptr, report_size) };

            // Write the byte slice to the file
            f_out.write_all(report_bytes).unwrap();

            println!("Local migration report successfully saved");
        }
    }
    sgx_status_t::SGX_SUCCESS
}

fn export_sealed_data() -> sgx_status_t {
    let next_report = match get_verified_migration_report_body(true) {
        Ok(report) => report,
        Err(e) => {
            error!("No next migration report: {}", e);
            return e;
        }
    };

    if !is_export_approved(&next_report) {
        error!("Export sealing not authorized");
        return sgx_status_t::SGX_ERROR_NO_PRIVILEGE;
    }

    // Build a source report addressed to the next enclave.
    let source_report = match create_source_report_for_dest(&next_report) {
        Ok(r) => r,
        Err(e) => {
            error!("source migration report for dest failed: {}", e);
            return e;
        }
    };

    let kp = Keychain::get_migration_keys();
    let aes_key = get_dh_aes_key_from_report(&next_report, &kp);

    let mut data_plain = Vec::new();
    KEY_MANAGER.serialize(&mut data_plain).unwrap();

    let data_encrypted = aes_key.encrypt_siv(&data_plain, None).unwrap();

    let mut f_out = match File::create(make_sgx_secret_path(FILE_MIGRATION_DATA)) {
        Ok(f) => f,
        Err(e) => {
            error!("failed to create file {}", e);
            return sgx_status_t::SGX_ERROR_UNEXPECTED;
        }
    };

    let report_ptr = &source_report as *const sgx_report_t as *const u8;
    let report_size = std::mem::size_of::<sgx_report_t>();
    let report_bytes: &[u8] = unsafe { slice::from_raw_parts(report_ptr, report_size) };
    if f_out.write_all(report_bytes).is_err() || f_out.write_all(&data_encrypted).is_err() {
        error!("failed to write migration blob");
        return sgx_status_t::SGX_ERROR_UNEXPECTED;
    }

    println!("Sealed data successfully exported");
    sgx_status_t::SGX_SUCCESS
}

fn parse_u64_nonzero(s: &str) -> Option<u64> {
    let n = s.trim().parse::<u64>().ok()?;
    if n == 0 {
        None
    } else {
        Some(n)
    }
}

fn parse_upgrade_info_height(bytes: &[u8]) -> Option<u64> {
    let v: serde_json::Value = serde_json::from_slice(bytes).ok()?;
    let h = v.get("height")?;
    if let Some(n) = h.as_u64() {
        if n == 0 {
            return None;
        }
        return Some(n);
    }
    if let Some(s) = h.as_str() {
        return parse_u64_nonzero(s);
    }
    None
}

/// Reads the halt height from the halt-height file, EXTRA_HEIGHT, or upgrade-info.json.
fn read_halt_height() -> Option<u64> {
    if let Ok(mut f) = File::open(make_sgx_secret_path(FILE_HALT_HEIGHT)) {
        let mut s = String::new();
        if f.read_to_string(&mut s).is_ok() {
            if let Some(h) = parse_u64_nonzero(&s) {
                return Some(h);
            }
        }
    }
    if let Ok(s) = std::env::var("EXTRA_HEIGHT") {
        if let Some(h) = parse_u64_nonzero(&s) {
            return Some(h);
        }
    }
    let mut paths: Vec<String> = Vec::new();
    if let Ok(home) = std::env::var("HOME") {
        paths.push(format!("{}/.secretd/data/upgrade-info.json", home));
    }
    paths.push("/root/.secretd/data/upgrade-info.json".to_string());
    paths.push("/opt/secret/.secretd/data/upgrade-info.json".to_string());
    for path in paths {
        if let Ok(mut f) = File::open(&path) {
            let mut buf = Vec::new();
            if f.read_to_end(&mut buf).is_ok() {
                if let Some(h) = parse_upgrade_info_height(&buf) {
                    return Some(h);
                }
            }
        }
    }
    None
}

/// When the optional halt proof files are present, check them against each imported seed.
/// Prints `{label}=N` or `{label}=none`. Does not print key bytes.
fn probe_halt_hmac_seeds(key_manager: &Keychain, label: &str) {
    let er = match read_exact_secret_file(FILE_HALT_ER, 48) {
        Some(v) => v,
        None => return,
    };
    let proof = match read_exact_secret_file(FILE_HALT_PROOF, 32) {
        Some(v) => v,
        None => return,
    };
    let apphash = match read_exact_secret_file(FILE_HALT_APPHASH, 32) {
        Some(v) => v,
        None => return,
    };
    let height = match read_halt_height() {
        Some(h) => h,
        None => return,
    };
    let seeds = match key_manager.get_consensus_seed() {
        Ok(s) => s,
        Err(_) => {
            println!("import_sealed_data: {}=none", label);
            return;
        }
    };
    let mut proof_arr = [0u8; 32];
    proof_arr.copy_from_slice(&proof);
    for i in 0..seeds.arr.len() {
        let irs = Keychain::generate_randomness_seed(&seeds.arr[i]);
        let got = enclave_utils::random::create_random_proof_v126(&irs, height, &er, &apphash);
        if got == proof_arr {
            println!("import_sealed_data: {}={}", label, i);
            return;
        }
    }
    println!("import_sealed_data: {}=none", label);
}

fn read_exact_secret_file(name: &str, want: usize) -> Option<Vec<u8>> {
    let path = make_sgx_secret_path(name);
    let mut f = File::open(&path).ok()?;
    let mut buf = Vec::new();
    f.read_to_end(&mut buf).ok()?;
    if buf.len() == want {
        Some(buf)
    } else {
        None
    }
}

fn apply_hstar_on_import(key_manager: &Keychain) -> sgx_status_t {
    let mut extra = key_manager.extra_data.lock().unwrap();
    if extra.height == 0 {
        error!("import_sealed_data: extra.height==0 (failed import)");
        return sgx_status_t::SGX_ERROR_UNEXPECTED;
    }
    // If the height gate is already set, keep it.
    if extra.random_proof_hstar != 0 {
        println!(
            "import_sealed_data: stamped random_proof_hstar={} (already set; extra.height={})",
            extra.random_proof_hstar, extra.height
        );
        return sgx_status_t::SGX_SUCCESS;
    }
    let halt_height = match read_halt_height() {
        Some(h) => h,
        None => {
            error!(
                "import_sealed_data: dest-3 needs halt_height = plan.Height (migrate_op 3 arg / EXTRA_HEIGHT / halt_height file / upgrade-info.json); never extra.height, never H+1"
            );
            return sgx_status_t::SGX_ERROR_UNEXPECTED;
        }
    };
    if extra.height != halt_height && extra.height != halt_height + 1 {
        error!(
            "import_sealed_data: extra.height={} not in {{halt={}, halt+1}}",
            extra.height, halt_height
        );
        return sgx_status_t::SGX_ERROR_UNEXPECTED;
    }
    extra.random_proof_hstar = halt_height;
    println!(
        "import_sealed_data: stamped random_proof_hstar={} (extra.height={})",
        halt_height, extra.height
    );
    sgx_status_t::SGX_SUCCESS
}

fn import_keychain_from_peer(source_pk: &Ed25519PublicKey, data_encrypted: &[u8]) -> sgx_status_t {
    let kp = Keychain::get_migration_keys();
    let aes_key = AESKey::new_from_slice(&kp.diffie_hellman(source_pk));

    let data_plain = match aes_key.decrypt_siv(data_encrypted, None) {
        Ok(res) => res,
        Err(err) => {
            error!("Can't decrypt sealing key: {}", err);
            return sgx_status_t::SGX_ERROR_UNEXPECTED;
        }
    };

    let mut key_manager = Keychain::new_empty();

    match key_manager.deserialize(&mut std::io::Cursor::new(data_plain)) {
        Ok(_) => {
            let n = key_manager
                .get_consensus_seed()
                .map(|s| s.arr.len())
                .unwrap_or(0);
            {
                let extra = key_manager.extra_data.lock().unwrap();
                println!(
                    "import_sealed_data: imported consensus_seeds={} last_block_seed={} extra.height={}",
                    n, extra.last_block_seed, extra.height
                );
            }
            probe_halt_hmac_seeds(&key_manager, "halt_hmac_seed");
            let st = apply_hstar_on_import(&key_manager);
            if st != sgx_status_t::SGX_SUCCESS {
                return st;
            }
            key_manager.save();
            let loaded = Keychain::new();
            probe_halt_hmac_seeds(&loaded, "halt_hmac_seed_after_load");
            info!("Sealing data successfully imported");
            sgx_status_t::SGX_SUCCESS
        }
        Err(err) => {
            info!("Failed to read sealed data: {}", err);
            sgx_status_t::SGX_ERROR_UNEXPECTED
        }
    }
}

fn import_sealed_data() -> sgx_status_t {
    // Import uses the verified source migration report. It does not store an approved measurement.
    let mut f_in = match File::open(make_sgx_secret_path(FILE_MIGRATION_DATA)) {
        Ok(f) => f,
        Err(e) => {
            error!("failed to open file {}", e);
            return sgx_status_t::SGX_ERROR_UNEXPECTED;
        }
    };

    let mut blob = Vec::new();
    if f_in.read_to_end(&mut blob).is_err() {
        error!("failed to read sealed data");
        return sgx_status_t::SGX_ERROR_UNEXPECTED;
    }

    let report_size = std::mem::size_of::<sgx_report_t>();
    if blob.len() > report_size {
        let source_report: sgx_report_t =
            unsafe { std::ptr::read(blob.as_ptr() as *const sgx_report_t) };
        if rsgx_verify_report(&source_report).is_ok() && mrsigner_allowed(&source_report.body) {
            let mut source_pk: Ed25519PublicKey = Ed25519PublicKey::default();
            source_pk.copy_from_slice(&source_report.body.report_data.d[..32]);
            if !source_pk.iter().all(|&b| b == 0) {
                return import_keychain_from_peer(&source_pk, &blob[report_size..]);
            }
        }
    }

    if !dest_op1_report_is_self() {
        error!("import_sealed_data: source migration report missing or invalid");
        return sgx_status_t::SGX_ERROR_NO_PRIVILEGE;
    }
    if blob.len() < 32 {
        error!("import_sealed_data: sealed blob too short");
        return sgx_status_t::SGX_ERROR_NO_PRIVILEGE;
    }
    let mut source_pk: Ed25519PublicKey = Ed25519PublicKey::default();
    source_pk.copy_from_slice(&blob[..32]);
    if source_pk.iter().all(|&b| b == 0) {
        error!("import_sealed_data: source pk empty");
        return sgx_status_t::SGX_ERROR_NO_PRIVILEGE;
    }
    import_keychain_from_peer(&source_pk, &blob[32..])
}

fn import_sealing_legacy() -> sgx_status_t {
    // TODO: disable in production build in the next version
    match Keychain::new_from_legacy() {
        Some(key_manager) => {
            key_manager.save();
            info!("Legacy data successfully imported");
            sgx_status_t::SGX_SUCCESS
        }
        None => {
            info!("Legacy data not found");
            sgx_status_t::SGX_ERROR_UNEXPECTED
        }
    }
}

const MAX_VARIABLE_LENGTH: u32 = 100_000;
const ENCRYPTED_RANDOM_LENGTH: u32 = 48;
const PROOF_LENGTH: u32 = 32;
const BLOCK_HASH_LENGTH: u32 = 32;
const VALSET_HASH_LENGTH: u32 = 32;

macro_rules! validate_input_length {
    ($input:expr, $var_name:expr, $constant:expr) => {
        if $input > $constant {
            error!(
                "Error: {} ({}) is larger than the constant value ({})",
                $var_name, $input, $constant
            );
            return sgx_status_t::SGX_ERROR_INVALID_PARAMETER;
        }
    };
}

pub fn calculate_validator_set_hash(
    validator_set_serialized: &[u8],
) -> SgxResult<tendermint::Hash> {
    match KeychainMutableData::decode_validator_set_ex(validator_set_serialized) {
        Some(res) => Ok(res.hash()),
        None => Err(sgx_status_t::SGX_ERROR_UNEXPECTED),
    }
}

#[no_mangle]
pub unsafe extern "C" fn ecall_submit_validator_set_evidence(
    val_set_evidence: *const u8,
) -> sgx_status_t {
    let evidence_len: usize = 32;

    validate_const_ptr!(
        val_set_evidence,
        evidence_len,
        sgx_status_t::SGX_ERROR_INVALID_PARAMETER
    );

    #[cfg(feature = "light-client-validation")]
    {
        let mut verified_msgs = VERIFIED_BLOCK_MESSAGES.lock().unwrap();

        verified_msgs
            .next_validators_evidence
            .copy_from_slice(slice::from_raw_parts(val_set_evidence, evidence_len));
    }
    sgx_status_t::SGX_SUCCESS
}

/// # Safety
/// make sure to check that block_hash is a valid pointer and that it's exactly 32 bytes long
#[no_mangle]
pub unsafe extern "C" fn ecall_generate_random(
    block_hash: *const u8,
    block_hash_len: u32,
    _height: u64,
    random: &mut [u8; ENCRYPTED_RANDOM_LENGTH as usize],
    _proof: &mut [u8; PROOF_LENGTH as usize],
) -> sgx_status_t {
    validate_const_ptr!(
        block_hash,
        block_hash_len as usize,
        sgx_status_t::SGX_ERROR_INVALID_PARAMETER
    );

    if block_hash_len != BLOCK_HASH_LENGTH {
        error!("block hash bad length");
        return sgx_status_t::SGX_ERROR_UNEXPECTED;
    }

    let mut rand_buf: [u8; 32] = [0; 32];

    if let Err(_e) = rsgx_read_rand(&mut rand_buf) {
        error!("Error generating random value");
        return sgx_status_t::SGX_ERROR_UNEXPECTED;
    };

    let validator_set_hash = {
        let extra = KEY_MANAGER.extra_data.lock().unwrap();

        if extra.height != _height {
            error!(
                "generate_random fail-closed: extra.height={} requested={}",
                extra.height, _height
            );
            return sgx_status_t::SGX_ERROR_UNEXPECTED;
        }

        match calculate_validator_set_hash(extra.validator_set_serialized.as_slice()) {
            Ok(tm_Sha256(hash)) => hash,
            _ => {
                error!("Got invalid validator set");
                return sgx_status_t::SGX_ERROR_UNEXPECTED;
            }
        }
    };

    // todo: add entropy detection

    let encrypted: Vec<u8> = if let Ok(res) =
        KEY_MANAGER.random_encryption_key.unwrap().encrypt_siv(
            &rand_buf,
            Some(vec![validator_set_hash.as_slice()].as_slice()),
        ) {
        res
    } else {
        return sgx_status_t::SGX_ERROR_UNEXPECTED;
    };

    random.copy_from_slice(encrypted.as_slice());

    // New proofs bind the validator-set hash.
    #[cfg(feature = "random")]
    {
        let block_hash_slice = slice::from_raw_parts(block_hash, block_hash_len as usize);

        let proof_computed = enclave_utils::random::create_random_proof(
            &KEY_MANAGER.initial_randomness_seed.unwrap(),
            _height,
            encrypted.as_slice(),
            block_hash_slice,
            validator_set_hash.as_slice(),
        );
        _proof.copy_from_slice(proof_computed.as_slice());
    }

    // debug!("Calculated proof: {:?}", proof_computed);

    sgx_status_t::SGX_SUCCESS
}

/// # Safety
/// Validator set can be of variable length, but it shouldn't be too long (and obv a valid pointer)
#[no_mangle]
pub unsafe extern "C" fn ecall_submit_validator_set(
    val_set: *const u8,
    val_set_len: u32,
    height: u64,
) -> sgx_status_t {
    validate_input_length!(val_set_len, "validator set length", MAX_VARIABLE_LENGTH);
    validate_const_ptr!(
        val_set,
        val_set_len as usize,
        sgx_status_t::SGX_ERROR_INVALID_PARAMETER
    );

    {
        let mut extra = KEY_MANAGER.extra_data.lock().unwrap();

        if height != extra.height + 1 {
            if extra.height == height {
                // redundant call, skip
                return sgx_status_t::SGX_SUCCESS;
            }

            if extra.height > height {
                error!(
                    "Height range not consequent: current={}, submitted={}",
                    extra.height, height
                );
                return sgx_status_t::SGX_ERROR_UNEXPECTED;
            }
        }

        let validator_set_slice = slice::from_raw_parts(val_set, val_set_len as usize);
        let validator_set_hash = match calculate_validator_set_hash(validator_set_slice) {
            Ok(tm_Sha256(hash)) => hash,
            _ => {
                error!("invalid validator set");
                return sgx_status_t::SGX_ERROR_UNEXPECTED;
            }
        };

        #[cfg(feature = "light-client-validation")]
        {
            let verified_msgs = VERIFIED_BLOCK_MESSAGES.lock().unwrap();
            let mut is_match = false;

            let seeds = KEY_MANAGER.get_consensus_seed().unwrap();
            for i_seed in extra.last_block_seed..seeds.arr.len() as u16 {
                let expected_evidence = Keychain::encrypt_hash_ex(
                    &seeds.arr[i_seed as usize],
                    validator_set_hash,
                    height,
                );

                is_match = verified_msgs.next_validators_evidence == expected_evidence;
                if is_match {
                    extra.last_block_seed = i_seed;
                    break;
                }
            }

            if !is_match {
                if extra.height != 0 {
                    error!("validator set evidence mismatch");
                    return sgx_status_t::SGX_ERROR_UNEXPECTED;
                }

                // We're given an initial validator set, without evidence. This covers the following cases:
                // 1. Start after bootstraping a new network
                // 2. Start after registration and sync normally (without using statesync)
                // in both cases the height should be 1 (i.e. we're at the very 1st block). And both cases are not applicable to production build.

                // Note that there MUST be a valid evidence for the following scenarios:
                // 1. Normal operation. The evidence is computed in submit_block_signatures
                // 2. Just after upgrate from legacy. The initial validator set is imported from legacy files, then computed as usual
                // 3. After statesync. The evidence for the initial validator set is downloaded with the whole state

                if height != 1 {
                    error!("Initial validator set height mismatch");
                    return sgx_status_t::SGX_ERROR_UNEXPECTED;
                }

                #[cfg(feature = "production")]
                {
                    error!("Initial validator set can't be set in production");
                    return sgx_status_t::SGX_ERROR_UNEXPECTED;
                }

                #[cfg(not(feature = "production"))]
                {
                    info!("Setting initial validator set");
                }
            }
        }

        {
            extra.height = height;
            extra.validator_set_serialized = validator_set_slice.to_vec();
        }
    }
    KEY_MANAGER.save();

    // calculate hash, and compare with the stored next_validators_hash

    sgx_status_t::SGX_SUCCESS
}

/// # Safety
/// Random will be 48 bytes
/// Proof will be 32 bytes
#[no_mangle]
pub unsafe extern "C" fn ecall_validate_random(
    random: *const u8,
    random_len: u32,
    proof: *const u8,
    proof_len: u32,
    block_hash: *const u8,
    block_hash_len: u32,
    valset_hash: *const u8,
    valset_hash_len: u32,
    _height: u64,
) -> sgx_status_t {
    validate_input_length!(random_len, "encrypted_random", ENCRYPTED_RANDOM_LENGTH);
    validate_input_length!(proof_len, "proof", PROOF_LENGTH);
    if block_hash_len != BLOCK_HASH_LENGTH {
        error!("block hash bad length");
        return sgx_status_t::SGX_ERROR_UNEXPECTED;
    }
    if valset_hash_len != VALSET_HASH_LENGTH {
        error!("valset hash bad length");
        return sgx_status_t::SGX_ERROR_INVALID_PARAMETER;
    }

    validate_const_ptr!(
        random,
        random_len as usize,
        sgx_status_t::SGX_ERROR_INVALID_PARAMETER
    );
    validate_const_ptr!(
        proof,
        proof_len as usize,
        sgx_status_t::SGX_ERROR_INVALID_PARAMETER
    );
    validate_const_ptr!(
        block_hash,
        block_hash_len as usize,
        sgx_status_t::SGX_ERROR_INVALID_PARAMETER
    );
    validate_const_ptr!(
        valset_hash,
        valset_hash_len as usize,
        sgx_status_t::SGX_ERROR_INVALID_PARAMETER
    );

    #[cfg(feature = "random")]
    {
        let random_slice = slice::from_raw_parts(random, random_len as usize);
        let proof_slice = slice::from_raw_parts(proof, proof_len as usize);
        let block_hash_slice = slice::from_raw_parts(block_hash, block_hash_len as usize);
        let valset_hash_slice = slice::from_raw_parts(valset_hash, valset_hash_len as usize);

        // Handshake: 1.26 proof only (HMAC). Do not decrypt here.
        // Execute (submit_block_signatures) still decrypts with valset extra data.
        match block_verifier::verify::random::validate_random_proof(
            random_slice,
            proof_slice,
            block_hash_slice,
            valset_hash_slice,
            _height,
        ) {
            Ok(_) => {}
            Err(e) => return e,
        }
    }

    sgx_status_t::SGX_SUCCESS
}
