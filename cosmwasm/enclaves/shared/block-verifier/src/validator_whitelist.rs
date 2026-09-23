use std::collections::HashSet;
use tendermint_light_client_verifier::types::UntrustedBlockState;

// trinity wins over production when both are set (Trinity HW Makefile).
#[cfg(feature = "trinity")]
const WHITELIST_FROM_FILE: &str = include_str!("../fixtures/validator_whitelist_trinity.txt");
#[cfg(all(feature = "production", not(feature = "trinity")))]
const WHITELIST_FROM_FILE: &str = include_str!("../fixtures/validator_whitelist_prod.txt");
#[cfg(not(any(feature = "production", feature = "trinity")))]
const WHITELIST_FROM_FILE: &str = include_str!("../fixtures/validator_whitelist.txt");

#[cfg(feature = "trinity")]
const WHITELIST_EMERGENCY_FROM_FILE: &str =
    include_str!("../fixtures/validator_whitelist_emergency_trinity.txt");
#[cfg(all(feature = "production", not(feature = "trinity")))]
const WHITELIST_EMERGENCY_FROM_FILE: &str =
    include_str!("../fixtures/validator_whitelist_emergency_prod.txt");
#[cfg(not(any(feature = "production", feature = "trinity")))]
const WHITELIST_EMERGENCY_FROM_FILE: &str = include_str!("../fixtures/validator_whitelist.txt");

#[cfg(feature = "trinity")]
pub const VALIDATOR_THRESHOLD: usize = 5;
#[cfg(feature = "trinity")]
pub const VALIDATOR_THRESHOLD_EMERGENCY: usize = 3;

#[cfg(all(feature = "production", not(feature = "trinity")))]
pub const VALIDATOR_THRESHOLD: usize = 5;
#[cfg(all(feature = "production", not(feature = "trinity")))]
pub const VALIDATOR_THRESHOLD_EMERGENCY: usize = 5;

#[cfg(not(any(feature = "production", feature = "trinity")))]
pub const VALIDATOR_THRESHOLD: usize = 1;
#[cfg(not(any(feature = "production", feature = "trinity")))]
pub const VALIDATOR_THRESHOLD_EMERGENCY: usize = 2;

lazy_static::lazy_static! {

    pub static ref VALIDATOR_WHITELIST: ValidatorList = ValidatorList::from_str(WHITELIST_FROM_FILE);
    pub static ref VALIDATOR_WHITELIST_EMERGENCY: ValidatorList = ValidatorList::from_str(WHITELIST_EMERGENCY_FROM_FILE);
}

#[derive(Debug, Clone)]
pub struct ValidatorList(pub HashSet<String>);

impl ValidatorList {
    fn from_str(list: &str) -> Self {
        let addresses: HashSet<String> = list
            .split(',')
            .map(|s| s.trim().to_string())
            .filter(|s| !s.is_empty())
            .collect();
        Self(addresses)
    }

    // use for tests
    #[allow(dead_code)]
    pub fn len(&self) -> usize {
        self.0.len()
    }

    pub fn contains(&self, input: &String) -> bool {
        self.0.contains(input)
    }
}

pub fn whitelisted_validators_in_block(untrusted_block: &UntrustedBlockState) -> bool {
    untrusted_block
        .validators
        .validators()
        .iter()
        .filter(|&a| VALIDATOR_WHITELIST.contains(&a.address.to_string()))
        .count()
        >= VALIDATOR_THRESHOLD
}

#[cfg(feature = "test")]
pub mod tests {

    use super::ValidatorList;

    const VALIDATOR_LIST_TEST: &str = "61D6833562A2EAFB0F7D9FDD8AD9F2BA0A1A7F86,A3F845F5D93356584BF276FCBB8F119BEB5DAE2A,40998CBE01E892CC9BFDB2BEF5B662AD954F3787,455DDE08C93C002F0356792BCA72AD3AAB75C096";

    pub fn test_parse_validators() {
        let validator_list = ValidatorList::from_str(VALIDATOR_LIST_TEST);

        assert_eq!(validator_list.len(), 4);
    }

    pub fn test_parse_validators_trims_whitespace() {
        let list = "61D6833562A2EAFB0F7D9FDD8AD9F2BA0A1A7F86,\n A3F845F5D93356584BF276FCBB8F119BEB5DAE2A\n,";
        let validator_list = ValidatorList::from_str(list);
        assert_eq!(validator_list.len(), 2);
        assert!(validator_list.contains(&"61D6833562A2EAFB0F7D9FDD8AD9F2BA0A1A7F86".to_string()));
        assert!(validator_list.contains(&"A3F845F5D93356584BF276FCBB8F119BEB5DAE2A".to_string()));
    }
}
