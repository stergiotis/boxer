package cli

import (
	"github.com/stergiotis/boxer/public/config/env"
	cli "github.com/urfave/cli/v3"
)

// categoryStreaming groups the variables that address streaming clusters.
const categoryStreaming env.CategoryE = "streaming"

// The connection, SASL and TLS settings every kafka subcommand shares, each
// declared once (ADR-0009) and derived into the common flags.
var (
	Brokers = env.NewString(env.Spec{
		Name:        "BOXER_KAFKA_BROKERS",
		Description: "comma-separated list of broker addresses (host1:9092,host2:9092)",
		Category:    categoryStreaming,
		CliFlagName: "brokers",
	})
	ClientID = env.NewString(env.Spec{
		Name:        "BOXER_KAFKA_CLIENT_ID",
		Default:     "boxer",
		Description: "kafka client.id",
		Category:    categoryStreaming,
		CliFlagName: "client-id",
	})
	// SASLMechanism stays a plain string: parseSASLMechanism is the
	// validator, case-insensitively, and refuses what it does not know
	// rather than falling back to none.
	SASLMechanism = env.NewString(env.Spec{
		Name:        "BOXER_KAFKA_SASL_MECHANISM",
		Description: "SASL mechanism: none, PLAIN, SCRAM-SHA-256, SCRAM-SHA-512 or OAUTHBEARER (case-insensitive); unset means none",
		Category:    categoryStreaming,
		CliFlagName: "sasl-mechanism",
	})
	SASLUsername = env.NewString(env.Spec{
		Name:        "BOXER_KAFKA_SASL_USERNAME",
		Description: "SASL username (PLAIN, SCRAM-SHA-256, SCRAM-SHA-512)",
		Category:    categoryStreaming,
		CliFlagName: "sasl-username",
	})
	SASLPassword = env.NewString(env.Spec{
		Name:        "BOXER_KAFKA_SASL_PASSWORD",
		Description: "SASL password (PLAIN, SCRAM-SHA-256, SCRAM-SHA-512); prefer the variable to the flag, which lands in shell history",
		Category:    categoryStreaming,
		Sensitive:   true,
		CliFlagName: "sasl-password",
	})
	SASLToken = env.NewString(env.Spec{
		Name:        "BOXER_KAFKA_SASL_TOKEN",
		Description: "static OAUTHBEARER token (only for --sasl-mechanism=OAUTHBEARER)",
		Category:    categoryStreaming,
		Sensitive:   true,
		CliFlagName: "sasl-token",
	})
	TLS = env.NewBool(env.Spec{
		Name:        "BOXER_KAFKA_TLS",
		Default:     "false",
		Description: "enable TLS (implicit if any --tls-* file flag is set)",
		Category:    categoryStreaming,
		CliFlagName: "tls",
	})
	TLSCAFile = env.NewPath(env.Spec{
		Name:        "BOXER_KAFKA_TLS_CA_FILE",
		Description: "path to a PEM-encoded CA bundle for verifying the broker certificate",
		Category:    categoryStreaming,
		CliFlagName: "tls-ca-file",
	})
	TLSCertFile = env.NewPath(env.Spec{
		Name:        "BOXER_KAFKA_TLS_CERT_FILE",
		Description: "path to a PEM-encoded client certificate (for mTLS); requires --tls-key-file",
		Category:    categoryStreaming,
		CliFlagName: "tls-cert-file",
	})
	TLSKeyFile = env.NewPath(env.Spec{
		Name:        "BOXER_KAFKA_TLS_KEY_FILE",
		Description: "path to a PEM-encoded client key (for mTLS); requires --tls-cert-file",
		Category:    categoryStreaming,
		CliFlagName: "tls-key-file",
	})
	TLSSkipVerify = env.NewBool(env.Spec{
		Name:        "BOXER_KAFKA_TLS_SKIP_VERIFY",
		Default:     "false",
		Description: "skip broker certificate verification (insecure; useful for self-signed dev clusters)",
		Category:    categoryStreaming,
		CliFlagName: "tls-skip-verify",
	})
)

// brokersFlag is Brokers as the required --brokers / -b flag.
func brokersFlag() cli.Flag {
	f := Brokers.AsCliFlag().(*cli.StringFlag)
	f.Aliases = []string{"b"}
	f.Required = true
	return f
}
