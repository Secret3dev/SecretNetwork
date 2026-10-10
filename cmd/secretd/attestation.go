//go:build !secretcli
// +build !secretcli

package main

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/server"
	"github.com/cosmos/cosmos-sdk/x/genutil"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"
	"github.com/scrtlabs/SecretNetwork/app"
	"github.com/scrtlabs/SecretNetwork/go-cosmwasm/api"
	reg "github.com/scrtlabs/SecretNetwork/x/registration"
	ra "github.com/scrtlabs/SecretNetwork/x/registration/remote_attestation"
	"github.com/spf13/cobra"
)

const (
	flagReset                     = "reset"
	flagPulsar                    = "pulsar"
	flagCustomRegistrationService = "registration-service"
	flag_no_epid                  = "no-epid"
	flag_no_dcap                  = "no-dcap"
	flag_is_migration_report      = "migration"
	flag_unbound_attestation      = "unbound-attestation"
)

const (
	flagLegacyRegistrationNode = "registration-node"
	flagLegacyBootstrapNode    = "node"
)

const mainnetRegistrationService = "https://auto.secret3.dev/api/registernode"

const signedEnclaveFile = "librust_cosmwasm_enclave.signed.so"

type PrivValidatorKey struct {
	PrivKey struct {
		Value string `json:"value"`
	} `json:"priv_key"`
}

func CreateAttestationReportEx(cmd *cobra.Command, is_migration_report bool) error {
	var ext_sk []byte

	unbound_attestation, _ := cmd.Flags().GetBool(flag_unbound_attestation)
	if !unbound_attestation {
		path := app.DefaultNodeHome + "/config/priv_validator_key.json"

		data, err := os.ReadFile(path)
		if err != nil {
			fmt.Errorf("couldn't read the validator key: %w", err)
			return err
		}

		var key PrivValidatorKey
		if err := json.Unmarshal(data, &key); err != nil {
			fmt.Errorf("couldn't decode the validator key: %w", err)
			return err
		}

		decoded, err := base64.StdEncoding.DecodeString(key.PrivKey.Value)
		if err != nil {
			fmt.Errorf("couldn't decode the validator key: %w", err)
			return err
		}

		ext_sk = decoded[:32]
	}

	_, err := api.CreateAttestationReport(ext_sk, is_migration_report)
	if err != nil {
		return fmt.Errorf("failed to create attestation report: %w", err)
	}
	return err
}

func InitAttestation() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init-enclave [output-file]",
		Short: "Perform remote attestation of the enclave",
		Long: `Create attestation report, signed by Intel which is used in the registation process of
the node to the chain. This process, if successful, will output a certificate which is used to authenticate with the 
blockchain. Writes the certificate in DER format to ~/attestation_cert
`,
		Args: cobra.ExactArgs(0),
		RunE: func(cmd *cobra.Command, _ []string) error {
			sgxSecretsDir := os.Getenv("SCRT_SGX_STORAGE")
			if sgxSecretsDir == "" {
				sgxSecretsDir = os.ExpandEnv("/opt/secret/.sgx_secrets")
			}

			// create sgx secrets dir if it doesn't exist
			if _, err := os.Stat(sgxSecretsDir); !os.IsNotExist(err) {
				err := os.MkdirAll(sgxSecretsDir, 0o777)
				if err != nil {
					return err
				}
			}

			sgxSecretsPath := sgxSecretsDir + string(os.PathSeparator) + reg.EnclaveSealedData

			resetFlag, err := cmd.Flags().GetBool(flagReset)
			if err != nil {
				return fmt.Errorf("error with reset flag: %s", err)
			}

			if !resetFlag {
				if _, err := os.Stat(sgxSecretsPath); os.IsNotExist(err) {
					fmt.Println("Creating new enclave registration key")
					_, err := api.KeyGen()
					if err != nil {
						return fmt.Errorf("failed to initialize enclave: %w", err)
					}
				} else {
					fmt.Println("Enclave key already exists. If you wish to overwrite and reset the node, use the --reset flag")
				}
			} else {
				fmt.Println("Reset enclave flag set, generating new enclave registration key. You must now re-register the node")
				_, err := api.KeyGen()
				if err != nil {
					return fmt.Errorf("failed to initialize enclave: %w", err)
				}
			}

			is_migration_report, _ := cmd.Flags().GetBool(flag_is_migration_report)
			err = CreateAttestationReportEx(cmd, is_migration_report)
			if err != nil {
				return fmt.Errorf("failed to create attestation report: %w", err)
			}
			return nil
		},
	}
	cmd.Flags().Bool(flagReset, false, "Optional flag to regenerate the enclave registration key")
	cmd.Flags().Bool(flag_no_epid, false, "Optional flag to disable EPID attestation")
	cmd.Flags().Bool(flag_no_dcap, false, "Optional flag to disable DCAP attestation")
	cmd.Flags().Bool(flag_is_migration_report, false, "Create migration report rather then attestation")
	cmd.Flags().Bool(flag_unbound_attestation, false, "Optional flag to disable attestation to user binding")

	return cmd
}

func InitBootstrapCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init-bootstrap [node-exchange-file] [io-exchange-file]",
		Short: "Perform bootstrap initialization",
		Long: `Create attestation report, signed by Intel which is used in the registration process of
the node to the chain. This process, if successful, will output a certificate which is used to authenticate with the 
blockchain. Writes the certificate in DER format to ~/attestation_cert
`,
		Args: cobra.MaximumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx := client.GetClientContextFromCmd(cmd)
			cdc := clientCtx.Codec

			serverCtx := server.GetServerContextFromCmd(cmd)
			config := serverCtx.Config

			genFile := config.GenesisFile()
			appState, genDoc, err := genutiltypes.GenesisStateFromGenFile(genFile)
			if err != nil {
				return fmt.Errorf("failed to unmarshal genesis state: %w", err)
			}

			regGenState := reg.GetGenesisStateFromAppState(cdc, appState)

			// the master key of the generated certificate is returned here
			masterKey, err := api.InitBootstrap()
			if err != nil {
				return fmt.Errorf("failed to initialize enclave: %w", err)
			}

			userHome, _ := os.UserHomeDir()

			// Load consensus_seed_exchange_pubkey
			var key []byte
			if len(args) >= 1 {
				key, err = os.ReadFile(args[0])
				if err != nil {
					return err
				}
			} else {
				key, err = os.ReadFile(filepath.Join(userHome, reg.NodeExchMasterKeyPath))
				if err != nil {
					return err
				}
			}

			pubkey, err := base64.StdEncoding.DecodeString(string(key))
			if err != nil {
				return err
			}

			fmt.Printf("%s\n", hex.EncodeToString(pubkey))
			fmt.Printf("%s\n", hex.EncodeToString(masterKey))

			// sanity check - make sure the certificate we're using matches the generated key
			if hex.EncodeToString(pubkey) != hex.EncodeToString(masterKey) {
				return fmt.Errorf("invalid certificate for master public key")
			}

			regGenState.NodeExchMasterKey.Bytes, err = base64.StdEncoding.DecodeString(string(key))
			if err != nil {
				return err
			}

			// Load consensus_io_exchange_pubkey
			if len(args) == 2 {
				key, err = os.ReadFile(args[1])
				if err != nil {
					return err
				}
			} else {
				key, err = os.ReadFile(filepath.Join(userHome, reg.IoExchMasterKeyPath))
				if err != nil {
					return err
				}
			}

			regGenState.IoMasterKey.Bytes, err = base64.StdEncoding.DecodeString(string(key))
			if err != nil {
				return err
			}

			// Create genesis state from certificates
			regGenStateBz, err := cdc.MarshalJSON(&regGenState)
			if err != nil {
				return fmt.Errorf("failed to marshal auth genesis state: %w", err)
			}

			appState[reg.ModuleName] = regGenStateBz

			appStateJSON, err := json.Marshal(appState)
			if err != nil {
				return fmt.Errorf("failed to marshal application genesis state: %w", err)
			}

			genDoc.AppState = appStateJSON
			return genutil.ExportGenesisFile(genDoc, genFile)
		},
	}

	return cmd
}

func ParseCert() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "parse [cert file]",
		Short: "Verify and parse a certificate file",
		Long: "Helper to verify generated credentials, and extract the public key of the secret node, which is used to" +
			"register the node, during node initialization",
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			// parse coins trying to be sent
			cert, err := os.ReadFile(args[0])
			if err != nil {
				return err
			}

			pubkey, err := ra.UNSAFE_VerifyRaCert(cert)
			if err != nil {
				return err
			}

			fmt.Printf("0x%s\n", hex.EncodeToString(pubkey))
			return nil
		},
	}

	return cmd
}

func DumpBin() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dump [binary file]",
		Short: "Dump a binary file",
		Long: "Helper to display the contents of a binary file, and extract the public key of the secret node, which is used to" +
			"register the node, during node initialization",
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			data, err := os.ReadFile(args[0])
			if err != nil {
				return err
			}

			fmt.Printf("%s\n", hex.EncodeToString(data))
			return nil
		},
	}

	return cmd
}

func parsePlanHeight(s string) (uint64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty halt_height")
	}
	n, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("halt_height must be a positive integer (plan.Height): %s", err)
	}
	if n == 0 {
		return 0, fmt.Errorf("halt_height must be non-zero plan.Height")
	}
	return n, nil
}

func parsePlanHeightFromUpgradeInfo() (uint64, bool) {
	path := filepath.Join(app.DefaultNodeHome, "data", "upgrade-info.json")
	bz, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	var info struct {
		Height json.RawMessage `json:"height"`
	}
	if json.Unmarshal(bz, &info) != nil {
		return 0, false
	}
	var n uint64
	if json.Unmarshal(info.Height, &n) == nil && n != 0 {
		return n, true
	}
	var s string
	if json.Unmarshal(info.Height, &s) == nil {
		n, err := strconv.ParseUint(strings.TrimSpace(s), 10, 64)
		if err == nil && n != 0 {
			return n, true
		}
	}
	return 0, false
}

// resolveHaltHeight returns plan.Height. CLI second arg, then EXTRA_HEIGHT, then
// data/upgrade-info.json. Never extra.height, never H+1. If upgrade-info is
// present, the chosen value MUST equal it.
func resolveHaltHeight(arg string) (string, error) {
	plan, hasPlan := parsePlanHeightFromUpgradeInfo()
	raw := strings.TrimSpace(arg)
	src := "migrate_op 3 arg"
	if raw == "" {
		raw = strings.TrimSpace(os.Getenv("EXTRA_HEIGHT"))
		src = "EXTRA_HEIGHT"
	}
	if raw == "" {
		if hasPlan {
			return strconv.FormatUint(plan, 10), nil
		}
		return "", nil
	}
	n, err := parsePlanHeight(raw)
	if err != nil {
		return "", err
	}
	if hasPlan && n != plan {
		return "", fmt.Errorf("%s=%d is not plan.Height %d (not extra.height, not H+1)", src, n, plan)
	}
	return strconv.FormatUint(n, 10), nil
}

func stampHaltHeightFile(haltHeight string) error {
	dir := os.Getenv("SCRT_SGX_STORAGE")
	if dir == "" {
		dir = "/opt/secret/.sgx_secrets"
	}
	path := filepath.Join(dir, "halt_height")
	haltHeight = strings.TrimSpace(haltHeight)
	if haltHeight == "" {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	n, err := parsePlanHeight(haltHeight)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(strconv.FormatUint(n, 10)+"\n"), 0o600)
}

func MigrationOp() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "migrate_op [opcode] [halt_height]",
		Short: "Migration operation",
		Long:  "0: migrate from SGX 2.17 format, 1: create migration report, 2: export sealing key for the new enclave, 3: import sealing data, 4: import legacy data, 5: self target info. Opcode 3: second arg / EXTRA_HEIGHT / data/upgrade-info.json MUST be plan.Height, never extra.height, never H+1. Empty stamp deletes a stale halt_height file.",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(_ *cobra.Command, args []string) error {
			op_num, err := strconv.ParseUint(args[0], 10, 32)
			if err != nil {
				return fmt.Errorf("opcode should be a number: %s", err)
			}

			if uint32(op_num) == 3 {
				arg := ""
				if len(args) >= 2 {
					arg = args[1]
				}
				h, err := resolveHaltHeight(arg)
				if err != nil {
					return err
				}
				if err := stampHaltHeightFile(h); err != nil {
					return err
				}
				if h == "" {
					return fmt.Errorf("migrate_op 3 requires halt_height = plan.Height (second arg, EXTRA_HEIGHT, or data/upgrade-info.json); empty stamp removed any stale halt_height file")
				}
			}

			_, err = api.MigrationOp(uint32(op_num))
			if err != nil {
				return fmt.Errorf("failed to migrate sealings. Enclave returned: %s", err)
			}

			fmt.Printf("Migration op succeeded\n")
			return nil
		},
	}

	return cmd
}

func EmergencyApproveUpgrade() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "emergency_approve_upgrade [mr_enclave]",
		Short: "Emergency enclave upgade approval",
		Long:  "Approve enclave upgrade in an offline mode. Need to reach consensus among network validators",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			_, err := api.EmergencyApproveUpgrade(app.DefaultNodeHome, args[0])
			if err != nil {
				return fmt.Errorf("failed to approve emergency upgrade: %s", err)
			}
			return nil
		},
	}

	return cmd
}

func ConfigureSecret() *cobra.Command {
	cmd := &cobra.Command{
		Use: "configure-secret [master-key] [seed]",
		Short: "After registration is successful, configure the secret node with the master key file and the encrypted " +
			"seed that was written on-chain",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			masterKey, err := os.ReadFile(args[0])
			if err != nil {
				return err
			}

			seed := args[1]
			println(seed)
			if (len(seed)%reg.EncryptedKeyGranularity != 0) || !reg.IsHexString(seed) {
				return fmt.Errorf("invalid encrypted seed format (requires hex string of multiples of 96 bytes without 0x prefix)")
			}

			cfg := reg.SeedConfig{
				EncryptedKey: seed,
				MasterKey:    string(masterKey),
				Version:      reg.SeedConfigVersion,
			}

			cfgBytes, err := json.Marshal(&cfg)
			if err != nil {
				return err
			}

			homeDir, err := cmd.Flags().GetString(flags.FlagHome)
			if err != nil {
				return err
			}

			// Create .secretd/.node directory if it doesn't exist
			nodeDir := filepath.Join(homeDir, reg.SecretNodeCfgFolder)
			err = os.MkdirAll(nodeDir, os.ModePerm)
			if err != nil {
				return err
			}

			seedFilePath := filepath.Join(nodeDir, reg.SecretNodeSeedNewConfig)

			err = os.WriteFile(seedFilePath, cfgBytes, 0o600)
			if err != nil {
				return err
			}

			return nil
		},
	}

	return cmd
}

func HealthCheck() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "check-enclave",
		Short: "Test enclave status",
		Long:  "Help diagnose issues by performing a basic sanity test that SGX is working properly",
		Args:  cobra.ExactArgs(0),
		RunE: func(_ *cobra.Command, _ []string) error {
			res, err := api.HealthCheck()
			if err != nil {
				return fmt.Errorf("failed to start enclave. Enclave returned: %s", err)
			}

			fmt.Printf("SGX enclave health status: %s\n", res)
			return nil
		},
	}

	return cmd
}

func ResetEnclave() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "reset-enclave",
		Short: "Reset registration & enclave parameters",
		Long: "This will delete all registration and enclave parameters. Use when something goes wrong and you want to start fresh." +
			"You will have to go through registration again to be able to start the node",
		Args: cobra.ExactArgs(0),
		RunE: func(cmd *cobra.Command, _ []string) error {
			homeDir, err := cmd.Flags().GetString(flags.FlagHome)
			if err != nil {
				return err
			}

			// Remove .secretd/.node/seed.json
			path := filepath.Join(homeDir, reg.SecretNodeCfgFolder, reg.SecretNodeSeedNewConfig)
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				fmt.Printf("Removing %s\n", path)
				err = os.Remove(path)
				if err != nil {
					return err
				}
			} else if err != nil {
				println(err.Error())
			}

			// remove sgx_secrets
			sgxSecretsDir := os.Getenv("SCRT_SGX_STORAGE")
			if sgxSecretsDir == "" {
				sgxSecretsDir = os.ExpandEnv("/opt/secret/.sgx_secrets")
			}
			if _, err := os.Stat(sgxSecretsDir); !os.IsNotExist(err) {
				fmt.Printf("Removing %s\n", sgxSecretsDir)
				err = os.RemoveAll(sgxSecretsDir)
				if err != nil {
					return err
				}
				err := os.MkdirAll(sgxSecretsDir, 0o777)
				if err != nil {
					return err
				}
			} else if err != nil {
				println(err.Error())
			}
			return nil
		},
	}

	return cmd
}

type OkayResponse struct {
	Status          string `json:"status"`
	Details         KeyVal `json:"details"`
	RegistrationKey string `json:"registration_key"`
}

type KeyVal struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type ErrorResponse struct {
	Status  string `json:"status"`
	Details string `json:"details"`
}

func signedEnclavePath() (string, error) {
	dir := os.Getenv("SCRT_ENCLAVE_DIR")
	if dir == "" {
		dir = "."
	}
	candidates := []string{
		filepath.Join(dir, signedEnclaveFile),
		filepath.Join("/lib", signedEnclaveFile),
		filepath.Join("/usr/lib", signedEnclaveFile),
		filepath.Join("/usr/local/lib", signedEnclaveFile),
	}
	seen := map[string]struct{}{}
	for _, path := range candidates {
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		info, err := os.Stat(path)
		if err == nil && !info.IsDir() {
			return path, nil
		}
	}
	return "", fmt.Errorf("cannot find %s", signedEnclaveFile)
}

func enclaveMeasurement(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	hdr := []byte{
		0x06, 0x00, 0x00, 0x00,
		0xe1, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x01, 0x00,
		0x00, 0x00, 0x00, 0x00,
	}
	i := bytes.Index(data, hdr)
	if i < 0 || i+992 > len(data) {
		return "", fmt.Errorf("enclave measurement not found in %s", path)
	}
	return hex.EncodeToString(data[i+960 : i+992]), nil
}

func currentSealPath() (string, error) {
	enclavePath, err := signedEnclavePath()
	if err != nil {
		return "", err
	}
	measurement, err := enclaveMeasurement(enclavePath)
	if err != nil {
		return "", err
	}
	dir := os.Getenv("SCRT_SGX_STORAGE")
	if dir == "" {
		dir = "/opt/secret/.sgx_secrets"
	}
	return filepath.Join(dir, "data-"+measurement+".bin"), nil
}

func rejectPulsar(cmd *cobra.Command) error {
	pulsar, err := cmd.Flags().GetBool(flagPulsar)
	if err != nil {
		return fmt.Errorf("error with testnet flag: %s", err)
	}
	if pulsar {
		return fmt.Errorf("auto-register --pulsar is not available")
	}
	return nil
}

func registrationServiceURL(cmd *cobra.Command) (string, error) {
	if err := rejectPulsar(cmd); err != nil {
		return "", err
	}
	custom, err := cmd.Flags().GetString(flagCustomRegistrationService)
	if err != nil {
		return "", err
	}
	if custom != "" {
		log.Println("Registering node with custom registration service")
		return custom, nil
	}
	log.Println("Registering node on mainnet")
	return mainnetRegistrationService, nil
}

func AutoRegisterNode() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auto-register",
		Short: "Register this node with the registration service",
		Long:  "Create an attestation and send it to the registration service. The service submits the register transaction.",
		Args:  cobra.ExactArgs(0),
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := rejectPulsar(cmd); err != nil {
				return err
			}

			sealPath, err := currentSealPath()
			if err != nil {
				return err
			}
			sgxSecretsFolder := filepath.Dir(sealPath)
			sgxAttestationCombined := filepath.Join(sgxSecretsFolder, reg.AttestationCombinedPath)

			resetFlag, err := cmd.Flags().GetBool(flagReset)
			if err != nil {
				return fmt.Errorf("error with reset flag: %s", err)
			}

			if !resetFlag {
				if _, err := os.Stat(sealPath); os.IsNotExist(err) {
					fmt.Println("Creating new enclave registration key")
					if _, err := api.KeyGen(); err != nil {
						return fmt.Errorf("failed to initialize enclave: %w", err)
					}
				} else if err != nil {
					return err
				} else {
					fmt.Println("Enclave key already exists. If you wish to overwrite and reset the node, use the --reset flag")
					return nil
				}
			} else {
				fmt.Println("Reset enclave flag set, generating new enclave registration key. You must now re-register the node")
				_ = os.Remove(sgxAttestationCombined)
				if _, err := api.KeyGen(); err != nil {
					return fmt.Errorf("failed to initialize enclave: %w", err)
				}
			}

			if err = CreateAttestationReportEx(cmd, false); err != nil {
				return fmt.Errorf("failed to create attestation report: %w", err)
			}

			certCombined, err := os.ReadFile(sgxAttestationCombined)
			if err != nil {
				_ = os.Remove(sgxAttestationCombined)
				return err
			}

			if _, err = ra.VerifyCombinedCert(certCombined); err != nil {
				return err
			}

			regURL, err := registrationServiceURL(cmd)
			if err != nil {
				return err
			}

			payload := []byte(fmt.Sprintf(`{
"certificate": "%s"
}`, base64.StdEncoding.EncodeToString(certCombined)))

			resp, err := http.Post(regURL, "application/json", bytes.NewBuffer(payload))
			if err != nil {
				log.Fatalln(err)
			}
			defer resp.Body.Close()

			body, err := io.ReadAll(resp.Body)
			if err != nil {
				log.Fatalln(err)
			}

			if resp.StatusCode != http.StatusOK {
				errDetails := ErrorResponse{}
				if err := json.Unmarshal(body, &errDetails); err != nil {
					return fmt.Errorf("registration TX was not successful - %s", err)
				}
				return fmt.Errorf("registration TX was not successful - %s", errDetails.Details)
			}

			details := OkayResponse{}
			if err = json.Unmarshal(body, &details); err != nil {
				return fmt.Errorf("error getting seed from registration service - %s", err)
			}

			seed := details.Details.Value
			log.Printf("seed: %s\n", seed)
			if len(seed) > 2 {
				seed = seed[2:]
			}
			if (len(seed)%reg.EncryptedKeyGranularity != 0) || !reg.IsHexString(seed) {
				return fmt.Errorf("invalid encrypted seed format (requires hex string of length 148 without 0x prefix)")
			}

			cfg := reg.SeedConfig{
				EncryptedKey: seed,
				MasterKey:    details.RegistrationKey,
				Version:      reg.SeedConfigVersion,
			}
			cfgBytes, err := json.Marshal(&cfg)
			if err != nil {
				return err
			}

			homeDir, err := cmd.Flags().GetString(flags.FlagHome)
			if err != nil {
				return err
			}
			seedCfgDir := filepath.Join(homeDir, reg.SecretNodeCfgFolder)
			seedCfgFile := filepath.Join(seedCfgDir, reg.SecretNodeSeedNewConfig)
			if _, err = os.Stat(seedCfgDir); os.IsNotExist(err) {
				if err = os.MkdirAll(seedCfgDir, 0o777); err != nil {
					return fmt.Errorf("failed to create directory '%s': %w", seedCfgDir, err)
				}
			}

			if _, err = os.Stat(seedCfgFile); !os.IsNotExist(err) {
				if err = os.Remove(seedCfgFile); err != nil {
					return fmt.Errorf("failed to modify file '%s': %w", seedCfgFile, err)
				}
			}
			if err = os.WriteFile(seedCfgFile, cfgBytes, 0o600); err != nil {
				return fmt.Errorf("failed to create file '%s': %w", seedCfgFile, err)
			}

			fmt.Println("Done registering! Ready to start...")
			return nil
		},
	}
	cmd.Flags().Bool(flagReset, false, "Optional flag to regenerate the enclave registration key")
	cmd.Flags().Bool(flagPulsar, false, "Not available")
	cmd.Flags().String(flagCustomRegistrationService, "", "Use this flag if you wish to specify a custom registration service")
	cmd.Flags().String(flagLegacyBootstrapNode, "", "DEPRECATED: This flag is no longer required or in use")
	cmd.Flags().String(flagLegacyRegistrationNode, "", "DEPRECATED: This flag is no longer required or in use")
	cmd.Flags().Bool(flag_no_epid, false, "Optional flag to disable EPID attestation")
	cmd.Flags().Bool(flag_no_dcap, false, "Optional flag to disable DCAP attestation")
	cmd.Flags().Bool(flag_unbound_attestation, false, "Optional flag to disable attestation to user binding")
	return cmd
}
